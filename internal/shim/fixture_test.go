package shim_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/shim"
	"github.com/andreasisaak/heavy/internal/testkit"
)

// fakeScript prints its name and each argument on a line of its own, holds when FAKE_HOLD names a file, copies
// standard input with FAKE_STDIN and ends with FAKE_STATUS, as the fake of hooks/test_heavy_shim.py:22.
const fakeScript = `printf 'fake %s\n' "${0##*/}"
for argument in "$@"; do printf '<%s>\n' "$argument"; done
[ -z "${FAKE_HOLD:-}" ] || { touch "$FAKE_HOLD"; sleep 1; }
[ -z "${FAKE_STDIN:-}" ] || cat
exit "${FAKE_STATUS:-0}"`

const patience = 30 * time.Second

var (
	pluginOnce      sync.Once
	pluginDirectory string
	pluginError     error
)

// TestMain removes the plugin layout the tests share.
func TestMain(m *testing.M) {
	code := m.Run()
	if pluginDirectory != "" {
		if err := os.RemoveAll(pluginDirectory); err != nil {
			code = 1
		}
	}
	os.Exit(code)
}

// plugin lays out a plugin as task 4.4 installs it: the built heavy as bin/heavy and every shim as a relative link
// shims/<program> -> ../bin/heavy. It returns the plugin root.
func plugin(t *testing.T) string {
	t.Helper()
	binary := testkit.BuildBinary(t)
	pluginOnce.Do(func() { pluginDirectory, pluginError = layOut(binary) })
	if pluginError != nil {
		t.Fatalf("laying out the plugin: %v", pluginError)
	}
	return pluginDirectory
}

func layOut(binary string) (string, error) {
	root, err := os.MkdirTemp("", "heavy-shim-plugin-")
	if err != nil {
		return "", err
	}
	if root, err = filepath.EvalSymlinks(root); err != nil {
		return "", err
	}
	for _, directory := range []string{"bin", "shims"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0o755); err != nil {
			return root, err
		}
	}
	if err := copyFile(binary, filepath.Join(root, "bin", "heavy")); err != nil {
		return root, err
	}
	for _, program := range shim.Programs {
		if err := os.Symlink(filepath.Join("..", "bin", "heavy"), filepath.Join(root, "shims", program)); err != nil {
			return root, err
		}
	}
	return root, nil
}

func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// fixture is one isolated session: the shared plugin, fake programs, a state directory, a home and a project.
type fixture struct {
	root    string
	shims   string
	heavy   string
	fakes   string
	state   string
	home    string
	scratch string
	project string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := plugin(t)
	scratch, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{
		root: root, shims: filepath.Join(root, "shims"), heavy: filepath.Join(root, "bin", "heavy"),
		fakes: filepath.Join(scratch, "fakes"), state: testkit.StateDir(t), home: filepath.Join(scratch, "home"),
		scratch: scratch, project: filepath.Join(scratch, "project"),
	}
	for _, directory := range []string{f.fakes, f.home} {
		if err := os.Mkdir(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, program := range shim.Programs {
		testkit.FakeProgram(t, f.fakes, program, fakeScript)
	}
	if output, err := exec.Command("/usr/bin/git", "init", "-q", f.project).CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v\n%s", f.project, err, output)
	}
	f.writeProjectList(t, `{"commands": ["make ci"]}`)
	t.Cleanup(func() { stopDaemon(t, f.state) })
	return f
}

func (f *fixture) writeProjectList(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.project, ".heavy.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// environment is the whole environment of a call: the shims in front of the fakes, scratch home and state.
func (f *fixture) environment(extra ...string) []string {
	return append([]string{
		"PATH=" + f.shims + ":" + f.fakes + ":/usr/bin:/bin", "HOME=" + f.home, "HEAVY_STATE_DIRECTORY=" + f.state,
	}, extra...)
}

type outcome struct {
	exitStatus int
	stdout     string
	stderr     string
}

// call runs the shim of program, as the shell starts it through PATH, and waits for its end.
func (f *fixture) call(t *testing.T, directory string, environment []string, stdin, program string, arguments ...string) outcome {
	t.Helper()
	return run(t, filepath.Join(f.shims, program), directory, environment, stdin, arguments...)
}

func run(t *testing.T, path, directory string, environment []string, stdin string, arguments ...string) outcome {
	t.Helper()
	command := exec.Command(path, arguments...)
	command.Dir, command.Env, command.Stdin = directory, environment, strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	done := make(chan error, 1)
	if err := command.Start(); err != nil {
		t.Fatalf("starting %s %q: %v", path, arguments, err)
	}
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		var exitError *exec.ExitError
		if err != nil && !errors.As(err, &exitError) {
			t.Fatalf("running %s %q: %v", path, arguments, err)
		}
	case <-time.After(patience):
		_ = command.Process.Kill()
		t.Fatalf("%s %q did not end within %s; stdout %q stderr %q", path, arguments, patience, stdout.String(), stderr.String())
	}
	return outcome{exitStatus: command.ProcessState.ExitCode(), stdout: stdout.String(), stderr: stderr.String()}
}

// printed is what a fake prints for its arguments.
func printed(program string, arguments ...string) string {
	lines := []string{"fake " + program}
	for _, argument := range arguments {
		lines = append(lines, "<"+argument+">")
	}
	return strings.Join(lines, "\n") + "\n"
}
