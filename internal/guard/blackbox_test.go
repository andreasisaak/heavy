package guard_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/testkit"
)

var startedLine = regexp.MustCompile(`^heavy: started at \d\d:\d\d:\d\d with \d+ cores \(HEAVY_CPUS\), running: .+$`)

// test_heavy_guard.py:279: the rewritten call runs through the real Go heavy and its daemon, in a state directory
// and a home of its own, with the shims of an installed heavy off PATH.
func TestRewrittenCommandRunsThroughTheRealHeavy(t *testing.T) {
	binary := testkit.BuildBinary(t)
	state := testkit.StateDir(t)
	t.Cleanup(func() { stopDaemon(t, state) })
	_, worktree := listed(t)
	home := testkit.Home(t)
	if err := os.WriteFile(filepath.Join(worktree, "Makefile"), []byte("ci:\n\t@echo ci ran\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	environment := runEnvironment(binary, state, home)
	for _, current := range []struct {
		command string
		lines   []string // "started" stands for heavy's start line
		ordered bool
	}{
		{"make ci", []string{"started", "ci ran"}, true},
		{"make ci &", []string{"started", "ci ran"}, false},
		{"make ci 2>&1 | tail -5", []string{"started", "ci ran"}, true},
		{"make ci && echo ok || echo failed", []string{"started", "ci ran", "ok"}, true},
		{"{ make ci; }", []string{"started", "ci ran"}, true},
		{"if make ci; then echo green; fi", []string{"started", "ci ran", "green"}, true},
		{"for attempt in 1 2; do make ci && break; done", []string{"started", "ci ran"}, true},
		{"while ! make ci; do sleep 1; done", []string{"started", "ci ran"}, true},
		{"! make ci || echo failed", []string{"started", "ci ran", "failed"}, true},
		{"nice -n 5 make ci", []string{"started", "ci ran"}, true},
		{"bash -c 'make ci'", []string{"started", "ci ran"}, true},
		{"(make ci)", []string{"started", "ci ran"}, true},
		{"echo $(make ci)", []string{"started", "ci ran"}, true},
		{"true\nmake ci", []string{"started", "ci ran"}, true},
		{"true # first\n(make ci) # then\ncat <<'EOF'\nlast\nEOF", []string{"started", "ci ran", "last"}, true},
		// heavy's start line names the command of the whole call with its line break.
		{"true\necho $(make ci)", []string{"started", "echo $(make ci)", "ci ran"}, true},
	} {
		for _, background := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s background=%v", current.command, background), func(t *testing.T) {
				present := newWorld(t)
				present.heavy = binary
				input := updated(present.run(t, bash(current.command, worktree, background)))
				if input == nil {
					t.Fatalf("%q was not rewritten", current.command)
				}

				stdout, stderr, exitStatus := runBash(t, input.Command, worktree, environment)

				want := current.lines
				if !background {
					want = append(slices.Clone(want), "heavy 0")
				}
				got := outputLines(stdout)
				if !current.ordered {
					slices.Sort(got)
					slices.Sort(want)
				}
				check(t, "exit status, stderr and stdout of "+input.Command, []any{exitStatus, stderr, got}, []any{0, "", want})
			})
		}
	}
}

// runEnvironment is this process's environment without heavy's and herdr's variables and without the directories
// of heavy shims on PATH, with the built heavy first on PATH and its own state directory and home.
func runEnvironment(binary, state, home string) []string {
	var environment []string
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		switch {
		case strings.HasPrefix(name, "HEAVY_"), strings.HasPrefix(name, "HERDR_"), name == "HOME":
		case name == "PATH":
			directories := []string{filepath.Dir(binary)}
			for _, directory := range filepath.SplitList(value) {
				if _, err := os.Stat(filepath.Join(directory, "heavy-shim")); err != nil {
					directories = append(directories, directory)
				}
			}
			environment = append(environment, "PATH="+strings.Join(directories, string(filepath.ListSeparator)))
		default:
			environment = append(environment, entry)
		}
	}
	return append(environment, "HEAVY_STATE_DIRECTORY="+state, "HOME="+home, "HEAVY_SESSION=blackbox-example")
}

func runBash(t *testing.T, command, directory string, environment []string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	run := exec.Command("/bin/bash", "-c", command)
	run.Dir, run.Env, run.Stdout, run.Stderr = directory, environment, &stdout, &stderr
	run.WaitDelay = 30 * time.Second
	err := run.Run()
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return stdout.String(), stderr.String(), exitError.ExitCode()
	}
	if err != nil {
		t.Fatalf("running bash -c %q: %v", command, err)
	}
	return stdout.String(), stderr.String(), 0
}

// outputLines are the lines of stdout with heavy's start line as "started".
func outputLines(stdout string) []string {
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	for index, line := range lines {
		if startedLine.MatchString(line) {
			lines[index] = "started"
		}
	}
	return lines
}

// stopDaemon ends the daemon a run started in state, by the pid in its lock.
func stopDaemon(t *testing.T, state string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(state, "daemon.lock"))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Errorf("reading the daemon lock: %v", err)
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		t.Errorf("the daemon lock holds %q, expected a pid", content)
		return
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Errorf("daemon %d did not end within 10 s of SIGTERM", pid)
}
