package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/testkit"
)

// pluginRoot is a copy of the plugin parts build.sh reads: hooks/build.sh, go.mod, go.sum and one Go file, with a
// fake go first on PATH that counts its builds and writes a bin/heavy that names the arguments it got.
type pluginRoot struct {
	root  string
	count string
	path  string
	fakes string
	// shellOptions go to /bin/sh in front of build.sh, such as -x for a trace of a run.
	shellOptions []string
}

func newPluginRoot(t *testing.T) *pluginRoot {
	t.Helper()
	root := t.TempDir()
	script, err := os.ReadFile("../../hooks/build.sh")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"hooks/build.sh":     string(script),
		"go.mod":             "module example.com/heavy\n",
		"go.sum":             "",
		"cmd/heavy/main.go":  "package main\n",
		"internal/x/x.go":    "package x\n",
		"internal/x/note.md": "not part of the stamp\n",
	}
	for name, content := range files {
		write(t, filepath.Join(root, name), content)
	}
	fakes := t.TempDir()
	count := filepath.Join(fakes, "builds")
	testkit.FakeProgram(t, fakes, "go", `out=""
while [ $# -gt 0 ]; do
  if [ "$1" = -o ]; then out=$2; fi
  shift
done
echo "$(date +%s.%N) go starts, parent $PPID, lock held by $(cat `+root+`/bin/.build.lock/pid 2>&1)" >> `+fakes+`/timeline
sleep 0.3
echo "$(date +%s.%N) go ends, parent $PPID" >> `+fakes+`/timeline
if [ -f `+fakes+`/during ]; then . `+fakes+`/during; fi
if [ -f `+fakes+`/fail ]; then echo "internal/x/x.go:1:1: syntax error" >&2; exit 1; fi
echo build >> `+count+`
printf '#!/bin/sh\necho "ran $* claude=$HEAVY_CLAUDE_PID" >> `+fakes+`/order\necho "heavy built for %s got $*"\n' "${GOOS:-host}" > "$out"
chmod +x "$out"`)
	// The system of the tests is a Mac with Apple silicon unless a test says otherwise, whatever machine runs them.
	testkit.FakeProgram(t, fakes, "uname", `case "$1" in -m) echo arm64 ;; *) echo Darwin ;; esac`)
	// No release holds a build of the sources unless a test puts one there: curl fails like GitHub's 404.
	testkit.FakeProgram(t, fakes, "curl", "exit 22")
	return &pluginRoot{root: root, count: count, path: fakes + ":/usr/bin:/bin", fakes: fakes}
}

// newLinuxRoot is a plugin root on a machine whose uname says Linux.
func newLinuxRoot(t *testing.T) *pluginRoot {
	t.Helper()
	plugin := newPluginRoot(t)
	testkit.FakeProgram(t, plugin.fakes, "uname", `case "$1" in -m) echo x86_64 ;; *) echo Linux ;; esac`)
	return plugin
}

// building is the note build.sh writes to stderr before a build.
func (p *pluginRoot) building() string {
	return "heavy: building bin/heavy and bin/heavy-linux with " + filepath.Join(p.fakes, "go") + "\n"
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (p *pluginRoot) builds(t *testing.T) int {
	t.Helper()
	content, err := os.ReadFile(p.count)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(content), "build\n")
}

// run starts build.sh with the mode and returns its exit status, stdout and stderr.
func (p *pluginRoot) run(t *testing.T, path, mode string) outcome {
	t.Helper()
	return p.runWith(t, path, mode)
}

// runWith is run with more variables in the environment.
func (p *pluginRoot) runWith(t *testing.T, path, mode string, environment ...string) outcome {
	t.Helper()
	command := exec.Command("/bin/sh", append(p.shellOptions, filepath.Join(p.root, "hooks", "build.sh"), mode)...)
	command.Env = append([]string{"PATH=" + path, "HOME=" + t.TempDir()}, environment...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	status := 0
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		status = exitError.ExitCode()
	} else if err != nil {
		t.Fatalf("running build.sh: %v", err)
	}
	return outcome{exitStatus: status, stdout: stdout.String(), stderr: stderr.String()}
}

// plan 4.4 and FR-E1: without go on PATH build.sh ends with exit 1 and says how to get Go
func TestBuildScriptWithoutGoSaysHowToGetIt(t *testing.T) {
	plugin := newPluginRoot(t)

	got := plugin.run(t, toolsWithout(t, "go"), "session-start")

	want := outcome{exitStatus: 1, stderr: "heavy: found no build of these sources to download and no go on PATH; wait a few minutes for the build on GitHub, " +
		"or install Go 1.27 (https://go.dev/dl/, on a Mac: brew install go), and start the session again\n"}
	if got != want {
		t.Fatalf("build.sh = %+v, want %+v", got, want)
	}
}

// V15: a build that fails falls back to the binary of the last build, with the go output and a note on stderr
func TestBuildScriptThatFailsRunsTheLastBuild(t *testing.T) {
	plugin := newPluginRoot(t)
	plugin.run(t, plugin.path, "session-start")
	write(t, filepath.Join(plugin.root, "internal", "x", "x.go"), "package x\n\nconst broken = \n")
	write(t, filepath.Join(plugin.fakes, "fail"), "")

	got := plugin.run(t, plugin.path, "session-start")

	want := outcome{stdout: "heavy built for host got --hook session-start\n", stderr: plugin.building() +
		"internal/x/x.go:1:1: syntax error\nheavy: building heavy failed; the go output above says why\n" +
		"heavy: running the bin/heavy of the last build meanwhile\n"}
	if got != want {
		t.Fatalf("build.sh = %+v, want %+v", got, want)
	}
	if left, _ := filepath.Glob(filepath.Join(plugin.root, "bin", "*.new*")); len(left) != 0 {
		t.Fatalf("the failed build left %v", left)
	}
}

// V15 and G15: without any binary to run, a failed build blocks the tool call of a PreToolUse hook (exit 2) and
// ends a session start or the monitor with exit 1
func TestBuildScriptThatFailsWithoutABinaryBlocksTheToolCall(t *testing.T) {
	failed := "internal/x/x.go:1:1: syntax error\nheavy: building heavy failed; the go output above says why\n"
	for mode, status := range map[string]int{"guard": 2, "stop": 2, "session-start": 1, "monitor": 1} {
		t.Run(mode, func(t *testing.T) {
			plugin := newPluginRoot(t)
			write(t, filepath.Join(plugin.fakes, "fail"), "")

			got := plugin.run(t, plugin.path, mode)

			if want := (outcome{exitStatus: status, stderr: plugin.building() + failed}); got != want {
				t.Fatalf("build.sh = %+v, want %+v", got, want)
			}
		})
	}
	t.Run("guard without go", func(t *testing.T) {
		got := newPluginRoot(t).run(t, toolsWithout(t, "go"), "guard")

		want := outcome{exitStatus: 2, stderr: "heavy: found no build of these sources to download and no go on PATH; wait a few minutes for the build on GitHub, " +
			"or install Go 1.27 (https://go.dev/dl/, on a Mac: brew install go), and start the session again\n"}
		if got != want {
			t.Fatalf("build.sh = %+v, want %+v", got, want)
		}
	})
}

// a source changed while go builds is not stamped as built, so the next session start builds again
func TestBuildScriptStampsTheSourcesItBuilt(t *testing.T) {
	plugin := newPluginRoot(t)
	write(t, filepath.Join(plugin.fakes, "during"), "echo '// edited during the build' >> "+filepath.Join(plugin.root, "internal", "x", "x.go")+
		"\n/bin/rm -f "+filepath.Join(plugin.fakes, "during")+"\n")
	plugin.run(t, plugin.path, "session-start")

	plugin.run(t, plugin.path, "session-start")

	if plugin.builds(t) != 4 {
		t.Fatalf("go ran %d builds, want 4: the edit during the first build calls for a second one", plugin.builds(t))
	}
}

// plan 4.4: a missing binary is built, both builds land in bin/, the stamp is written, and the hook runs
func TestBuildScriptBuildsAMissingBinaryAndRunsTheHook(t *testing.T) {
	plugin := newPluginRoot(t)

	got := plugin.run(t, plugin.path, "guard")

	if want := (outcome{stdout: "heavy built for host got --hook guard\n", stderr: plugin.building()}); got != want {
		t.Fatalf("build.sh = %+v, want %+v", got, want)
	}
	linux, err := exec.Command(filepath.Join(plugin.root, "bin", "heavy-linux"), "x").Output()
	stamp, stampError := os.ReadFile(filepath.Join(plugin.root, "bin", ".stamp"))
	if err != nil || string(linux) != "heavy built for linux got x\n" || stampError != nil || len(strings.TrimSpace(string(stamp))) != 64 {
		t.Fatalf("heavy-linux %q, %v; stamp %q, %v", linux, err, stamp, stampError)
	}
	if plugin.builds(t) != 2 {
		t.Fatalf("go ran %d builds, want 2", plugin.builds(t))
	}
}

// plan 4.4: two session starts at once build once; both end with exit 0
func TestBuildScriptBuildsOnceForTwoSessionsAtOnce(t *testing.T) {
	plugin := newPluginRoot(t)
	// Each session traces its commands (sh -x) into its stderr, shown when the test fails: on the Ubuntu runner of
	// the CI both sessions built at once (2026-11-11). The traces of the pipelines mix into the lines of stderr,
	// so the test counts the note about the build instead of comparing stderr as a whole.
	plugin.shellOptions = []string{"-x"}
	var group sync.WaitGroup
	results := make([]outcome, 2)
	for index := range results {
		group.Add(1)
		go func() {
			defer group.Done()
			results[index] = plugin.run(t, plugin.path, "session-start")
		}()
	}
	group.Wait()

	notes, hooks := 0, 0
	for _, result := range results {
		// The trace repeats the note behind "+ echo"; the note itself starts its line.
		notes += strings.Count("\n"+result.stderr, "\n"+plugin.building())
		if result.exitStatus == 0 && result.stdout == "heavy built for host got --hook session-start\n" {
			hooks++
		}
	}
	one := notes == 1 && hooks == 2
	if !one || plugin.builds(t) != 2 {
		timeline, _ := os.ReadFile(filepath.Join(plugin.fakes, "timeline"))
		t.Fatalf("%d notes about a build, %d hooks that ran, %d builds; want one build of each binary and the second session waiting for it; the builds:\n%s\nfirst session:\n%s\nsecond session:\n%s",
			notes, hooks, plugin.builds(t), timeline, results[0].stderr, results[1].stderr)
	}
}

// plan 4.4: a changed Go file rebuilds at the session start, not on a guard call
func TestBuildScriptRebuildsAChangedSourceOnlyAtTheSessionStart(t *testing.T) {
	plugin := newPluginRoot(t)
	plugin.run(t, plugin.path, "session-start")
	write(t, filepath.Join(plugin.root, "internal", "x", "x.go"), "package x\n\nconst changed = true\n")

	plugin.run(t, plugin.path, "guard")
	afterGuard := plugin.builds(t)
	plugin.run(t, plugin.path, "session-start")
	afterStart := plugin.builds(t)
	write(t, filepath.Join(plugin.root, "internal", "x", "note.md"), "changed, but no Go\n")
	plugin.run(t, plugin.path, "session-start")

	if afterGuard != 2 || afterStart != 4 || plugin.builds(t) != 4 {
		t.Fatalf("builds after guard %d, after the session start %d, after a non-Go change %d; want 2, 4, 4", afterGuard, afterStart, plugin.builds(t))
	}
}

// G2: a lock left by a build that died does not hold up the next one
func TestBuildScriptTakesOverALockOfADeadBuild(t *testing.T) {
	plugin := newPluginRoot(t)
	write(t, filepath.Join(plugin.root, "bin", ".build.lock", "pid"), "999999\n")

	got := plugin.run(t, plugin.path, "stop")

	takeover := "heavy: the build lock names process 999999, which does not run; taking the lock over\n"
	if want := (outcome{stdout: "heavy built for host got --hook stop\n", stderr: takeover + plugin.building()}); got != want {
		t.Fatalf("build.sh = %+v, want %+v", got, want)
	}
	if _, err := os.Stat(filepath.Join(plugin.root, "bin", ".build.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the lock is still there: %v", err)
	}
}

func TestBuildScriptRefusesAnUnknownMode(t *testing.T) {
	plugin := newPluginRoot(t)

	got := plugin.run(t, plugin.path, "lunch")

	want := outcome{exitStatus: 64, stderr: "heavy: build.sh takes the mode session-start, guard, stop, monitor or stamp, got lunch\n"}
	if got != want {
		t.Fatalf("build.sh = %+v, want %+v", got, want)
	}
}

// lockedRoot is a built plugin whose bin/heavy says whether the build lock exists while it runs.
func lockedRoot(t *testing.T) (*pluginRoot, string) {
	t.Helper()
	plugin := newPluginRoot(t)
	plugin.run(t, plugin.path, "session-start")
	lock := filepath.Join(plugin.root, "bin", ".build.lock")
	binary := filepath.Join(plugin.root, "bin", "heavy")
	write(t, binary, "#!/bin/sh\nif [ -d '"+lock+"' ]; then echo \"lock held, got $*\"; else echo \"lock free, got $*\"; fi\n")
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(lock, 0o755); err != nil {
		t.Fatal(err)
	}
	return plugin, lock
}

// G9: the monitor waits for a build that holds the lock, so it never starts the binary that build replaces
func TestBuildScriptMonitorWaitsForARunningBuild(t *testing.T) {
	plugin, lock := lockedRoot(t)
	holder := exec.Command("/bin/sh", "-c", "sleep 1; /bin/rm '"+lock+"/pid'; rmdir '"+lock+"'")
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(lock, "pid"), strconv.Itoa(holder.Process.Pid)+"\n")

	got := plugin.run(t, plugin.path, "monitor")

	if err := holder.Wait(); err != nil {
		t.Fatalf("the lock holder failed: %v", err)
	}
	if want := (outcome{stdout: "lock free, got --monitor\n"}); got != want {
		t.Fatalf("build.sh monitor = %+v, want %+v", got, want)
	}
}

// a lock whose holder died, or that names no holder for three rounds, does not hold the monitor up
func TestBuildScriptMonitorDoesNotWaitForAStaleLock(t *testing.T) {
	for name, pid := range map[string]string{"dead holder": "999999\n", "no holder": ""} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			plugin, lock := lockedRoot(t)
			write(t, filepath.Join(lock, "pid"), pid)
			done := make(chan outcome, 1)

			go func() { done <- plugin.run(t, plugin.path, "monitor") }()

			select {
			case got := <-done:
				if want := (outcome{stdout: "lock held, got --monitor\n"}); got != want {
					t.Fatalf("build.sh monitor = %+v, want %+v", got, want)
				}
			case <-time.After(15 * time.Second):
				t.Fatal("build.sh monitor still waits for the stale lock after 15s")
			}
		})
	}
}

// V24: the monitor may start before the SessionStart hook takes the build lock, so it checks the stamp too and
// never runs the old binary for the whole session
func TestBuildScriptRebuildsAChangedSourceForTheMonitorToo(t *testing.T) {
	plugin := newPluginRoot(t)
	plugin.run(t, plugin.path, "session-start")
	write(t, filepath.Join(plugin.root, "internal", "x", "x.go"), "package x\n\nconst changed = true\n")

	got := plugin.run(t, plugin.path, "monitor")

	if want := (outcome{stdout: "heavy built for host got --monitor\n", stderr: plugin.building()}); got != want || plugin.builds(t) != 4 {
		t.Fatalf("build.sh monitor = %+v after %d builds, want %+v after 4", got, plugin.builds(t), want)
	}
}
