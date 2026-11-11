package sessionstart_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/sessions"
	"github.com/andreasisaak/heavy/internal/sessionstart"
	"github.com/andreasisaak/heavy/internal/testkit"
)

const sessionStart = `{"session_id": "5968af83-2137-45a0-b3d2-6d3a974c6814", "hook_event_name": "SessionStart", "source": "startup", "cwd": "%s"}`

var started = time.Date(2026, 11, 8, 12, 30, 0, 0, time.UTC)

// hook runs the session start with the input and returns its exit status, stdout and stderr; CLAUDE_PLUGIN_DATA is
// a temporary directory.
func hook(t *testing.T, input string, envFile string) (int, string, string, sessionstart.Dependencies) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "shims"), 0o755); err != nil {
		t.Fatal(err)
	}
	deps := sessionstart.Dependencies{
		Alive: everyProcessLives, EnvFile: envFile, Root: root, StateDirectory: testkit.StateDir(t), ClaudePID: 4242,
		Now: func() time.Time { return started }, HerdrPlugin: pluginOf(t, t.TempDir(), "1.0.1"),
	}
	var stdout, stderr bytes.Buffer
	status := sessionstart.Run(strings.NewReader(input), &stdout, &stderr, deps)
	return status, stdout.String(), stderr.String(), deps
}

// hooks/test_session_env.py:24: sourced twice, in bash and in zsh, the file puts the shims in front of PATH once
// and keeps what was there. The shims lie behind $STATE/current, a path without the version of the plugin, and the
// link there names the plugin folder of this heavy.
func TestEnvFilePutsTheShimsBehindTheLinkFirstOnce(t *testing.T) {
	for _, shell := range []string{"/bin/bash", "/bin/zsh"} {
		t.Run(shell, func(t *testing.T) {
			if _, err := os.Stat(shell); err != nil {
				t.Skipf("this machine has no %s", shell)
			}
			envFile := filepath.Join(t.TempDir(), "sessionstart-hook.sh")
			if err := os.WriteFile(envFile, []byte("export EXAMPLE=kept\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			status, stdout, stderr, deps := hook(t, strings.Replace(sessionStart, "%s", t.TempDir(), 1), envFile)
			command := exec.Command(shell, "-c", `. "`+envFile+`"; . "`+envFile+`"; printf "%s %s %s" "$EXAMPLE" "$HEAVY_SESSION" "$PATH"`)
			command.Env = []string{"PATH=/usr/bin:/bin"}
			path, err := command.Output()

			if status != 0 || stdout != "" || stderr != "" || err != nil {
				t.Fatalf("hook = %d, %q, %q; sourcing: %v", status, stdout, stderr, err)
			}
			if want := "kept 5968af83-2137-45a0-b3d2-6d3a974c6814 " + deps.StateDirectory + "/current/shims:/usr/bin:/bin"; string(path) != want {
				t.Fatalf("PATH after sourcing = %q, want %q", path, want)
			}
			if linked, err := os.Readlink(filepath.Join(deps.StateDirectory, "current")); err != nil || linked != deps.Root {
				t.Fatalf("$STATE/current = %q, %v; want a link to %s", linked, err, deps.Root)
			}
		})
	}
}

// hooks/test_session_env.py:39
func TestInputWithoutASessionFailsWithWhatItGot(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "sessionstart-hook.sh")

	status, stdout, stderr, _ := hook(t, `{"hook_event_name": "SessionStart"}`, envFile)

	_, statError := os.Stat(envFile)
	if status != 1 || stdout != "" || !os.IsNotExist(statError) {
		t.Fatalf("hook = %d, %q, env file %v; want 1, nothing, no env file", status, stdout, statError)
	}
	if want := "heavy: expected a session_id in the SessionStart input, got {\"hook_event_name\": \"SessionStart\"}\n"; stderr != want {
		t.Fatalf("stderr = %q, want %q", stderr, want)
	}
}

// hooks/test_session_env.py:48
func TestWithoutEnvFileTheHookNotesWhyOnStderr(t *testing.T) {
	status, stdout, stderr, _ := hook(t, strings.Replace(sessionStart, "%s", t.TempDir(), 1), "")

	want := "heavy: CLAUDE_ENV_FILE is not set, so the shims stay off PATH and only the PreToolUse hook queues heavy calls\n"
	if status != 0 || stdout != "" || stderr != want {
		t.Fatalf("hook = %d, %q, %q; want 0, nothing, %q", status, stdout, stderr, want)
	}
}

// G9: the hook registers the session under the PID of Claude Code with its working directory and main checkout,
// for the monitor that walks up its parent PIDs
func TestTheSessionIsRegisteredUnderTheClaudePID(t *testing.T) {
	repository, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("/usr/bin/git", "init", "-q", repository).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	directory := filepath.Join(repository, "sub")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}

	status, _, stderr, deps := hook(t, strings.Replace(sessionStart, "%s", directory, 1), filepath.Join(t.TempDir(), "env.sh"))

	data, err := os.ReadFile(filepath.Join(deps.StateDirectory, "sessions", "4242.json"))
	var got map[string]any
	if status != 0 || stderr != "" || err != nil || json.Unmarshal(data, &got) != nil {
		t.Fatalf("hook = %d, %q; registration %q, %v", status, stderr, data, err)
	}
	want := map[string]any{"session_id": "5968af83-2137-45a0-b3d2-6d3a974c6814", "cwd": directory, "checkout": repository, "time": float64(started.Unix())}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registration = %v, want %v", got, want)
	}
}

// a registration that fails still leaves the shims and HEAVY_SESSION in the env file, which the guard and the
// shims need more than the monitor needs the registration
func TestARegistrationThatCannotBeWrittenFailsWithThePath(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "env.sh")
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "shims"), 0o755); err != nil {
		t.Fatal(err)
	}
	deps := sessionstart.Dependencies{Alive: everyProcessLives, EnvFile: envFile, Root: root, StateDirectory: "/dev/null/state", ClaudePID: 7, Now: func() time.Time { return started }}
	var stdout, stderr bytes.Buffer

	status := sessionstart.Run(strings.NewReader(strings.Replace(sessionStart, "%s", t.TempDir(), 1)), &stdout, &stderr, deps)

	want := "heavy: reading the link to the plugin folder of heavy: readlink /dev/null/state/current: not a directory\n" +
		"heavy: registering the session in /dev/null/state/sessions: mkdir /dev/null: not a directory\n"
	if status != 1 || stdout.Len() != 0 || stderr.String() != want {
		t.Fatalf("hook = %d, %q, %q; want 1 and %q", status, stdout.String(), stderr.String(), want)
	}
	written, err := os.ReadFile(envFile)
	wantLines := "case \"$PATH\" in /dev/null/state/current/shims:*) ;; *) export PATH=/dev/null/state/current/shims:\"$PATH\" ;; esac\nexport HEAVY_SESSION=5968af83-2137-45a0-b3d2-6d3a974c6814\n"
	if err != nil || string(written) != wantLines {
		t.Fatalf("env file = %q, %v; want %q", written, err, wantLines)
	}
}

func TestAnInputThatIsNoJSONFailsWithWhatItGot(t *testing.T) {
	status, stdout, stderr, _ := hook(t, "session please", filepath.Join(t.TempDir(), "env.sh"))

	want := "heavy: expected a session_id in the SessionStart input, got session please\n"
	if status != 1 || stdout != "" || stderr != want {
		t.Fatalf("hook = %d, %q, %q; want 1 and %q", status, stdout, stderr, want)
	}
}

// everyProcessLives keeps every registration in the tests that are not about the cleanup.
func everyProcessLives(int) bool { return true }

// heavy-37 2026-11-08: a session start removes the registrations of Claude Code processes that ended and keeps its own,
// even when its Claude Code PID is not seen alive
func TestSessionStartRemovesTheRegistrationsOfEndedSessions(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "env.sh")
	state := testkit.StateDir(t)
	if err := sessions.Write(state, 25802, sessions.Registration{SessionID: "ended"}); err != nil {
		t.Fatal(err)
	}
	deps := sessionstart.Dependencies{
		Alive: func(int) bool { return false }, EnvFile: envFile, Root: "/s", StateDirectory: state, ClaudePID: 4242,
		Now: func() time.Time { return started }, HerdrPlugin: pluginOf(t, t.TempDir(), "1.0.1"),
	}
	var stdout, stderr bytes.Buffer

	status := sessionstart.Run(strings.NewReader(strings.Replace(sessionStart, "%s", t.TempDir(), 1)), &stdout, &stderr, deps)

	entries, err := os.ReadDir(filepath.Join(state, "sessions"))
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if status != 0 || err != nil || !reflect.DeepEqual(names, []string{"4242.json"}) {
		t.Fatalf("hook = %d (stderr %q); registrations %v, %v; want exit 0 and only 4242.json", status, stderr.String(), names, err)
	}
}
