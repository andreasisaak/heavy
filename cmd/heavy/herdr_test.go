package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/andreasisaak/heavy/internal/herdr"
	"github.com/andreasisaak/heavy/internal/shell"
	"github.com/andreasisaak/heavy/internal/testkit"
)

// herdrWorld is a fake herdr that answers `agent list` with listing and records its calls, a temporary home and
// state directory, and the environment of a herdr plugin command without the variables of the real herdr.
type herdrWorld struct {
	calls string
	state string
	env   []string
}

func newHerdrWorld(t *testing.T, listing string, variables ...string) herdrWorld {
	t.Helper()
	directory := t.TempDir()
	listingFile, calls := filepath.Join(directory, "agents.json"), filepath.Join(directory, "calls")
	if err := os.WriteFile(listingFile, []byte(listing), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := testkit.FakeProgram(t, directory, "herdr", `{ for argument in "$@"; do printf '%s\037' "$argument"; done; printf '\n'; } >> `+shell.Quote(calls)+`
if [ "$1 $2" = "agent list" ]; then cat `+shell.Quote(listingFile)+`; fi`)
	state := testkit.StateDir(t)
	var env []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(name, "HERDR_") && !strings.HasPrefix(name, "HEAVY_") && !strings.HasPrefix(name, "CLAUDE_") && name != "HOME" {
			env = append(env, entry)
		}
	}
	env = append(env, "HOME="+t.TempDir(), "HEAVY_STATE_DIRECTORY="+state, "HERDR_BIN_PATH="+fake)
	return herdrWorld{calls: calls, state: state, env: append(env, variables...)}
}

func (w herdrWorld) run(t *testing.T, binary, stdin string, arguments ...string) outcome {
	t.Helper()
	command := exec.Command(binary, arguments...)
	var stdout, stderr bytes.Buffer
	command.Env, command.Stdin, command.Stdout, command.Stderr = w.env, strings.NewReader(stdin), &stdout, &stderr
	err := command.Run()
	exitStatus := 0
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		exitStatus = exitError.ExitCode()
	} else if err != nil {
		t.Fatalf("running heavy %q: %v", arguments, err)
	}
	return outcome{exitStatus: exitStatus, stdout: stdout.String(), stderr: stderr.String()}
}

func (w herdrWorld) recorded(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(w.calls)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		calls = append(calls, strings.ReplaceAll(strings.TrimSuffix(line, "\x1f"), "\x1f", " "))
	}
	return calls
}

func checkHerdr(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s:\n got %#v\nwant %#v", what, got, want)
	}
}

// without a daemon no run shows, and the refresh starts none: the row an agent still shows goes
func TestHerdrRefreshWithoutADaemonClearsTheRowOfAnAgent(t *testing.T) {
	world := newHerdrWorld(t, `{"result": {"agents": [{"pane_id": "w1:p1", "cwd": "/sites/main", "tokens": {"heavy": "▶ 3m · ci"}}]}}`, "HERDR_PLUGIN_EVENT=pane.agent_detected")

	got := world.run(t, testkit.BuildBinary(t), "", "--herdr-refresh")

	checkHerdr(t, "outcome", got, outcome{stdout: "heavy-queue: event=pane.agent_detected agents=1 reported=1\n"})
	checkHerdr(t, "herdr calls", world.recorded(t), []string{"agent list", "pane report-metadata w1:p1 --source heavy-queue --clear-token heavy"})
	_, err := os.Stat(filepath.Join(world.state, "daemon.lock"))
	checkHerdr(t, "daemon started", !os.IsNotExist(err), false)
}

func TestHerdrForwardWithoutAQueuedRunSaysSoInANotification(t *testing.T) {
	world := newHerdrWorld(t, "{}", "HERDR_PANE_ID=w1:p1")

	got := world.run(t, testkit.BuildBinary(t), "", "--herdr-forward")

	checkHerdr(t, "outcome", got, outcome{})
	checkHerdr(t, "herdr calls", world.recorded(t), []string{"notification show heavy --body no queued run in this pane"})
}

func TestHerdrForwardWithoutAPaneFailsWithTheReason(t *testing.T) {
	world := newHerdrWorld(t, "{}")

	got := world.run(t, testkit.BuildBinary(t), "", "--herdr-forward")

	checkHerdr(t, "outcome", got, outcome{exitStatus: 1, stderr: "heavy: --herdr-forward needs HERDR_PANE_ID, the focused pane herdr sets for a plugin action\n"})
	checkHerdr(t, "herdr calls", world.recorded(t), []string(nil))
}

func TestHerdrOptionsTakeNoArguments(t *testing.T) {
	for _, option := range []string{"--herdr-refresh", "--herdr-forward"} {
		t.Run(option, func(t *testing.T) {
			world := newHerdrWorld(t, "{}", "HERDR_PANE_ID=w1:p1")

			got := world.run(t, testkit.BuildBinary(t), "", option, "now")

			checkHerdr(t, "outcome", got, outcome{exitStatus: 64, stderr: "heavy: " + option + " takes no arguments, got now. " + usage + "\n"})
			checkHerdr(t, "herdr calls", world.recorded(t), []string(nil))
		})
	}
}

// installedHeavy is a copy of the built heavy in a plugin folder of version, as the plugin cache holds it; the
// herdr plugin in data holds the manifest of present.
func installedHeavy(t *testing.T, version, data, present string) (binary string) {
	t.Helper()
	root := resolvedTemp(t)
	binary = filepath.Join(root, "bin", "heavy")
	content, err := os.ReadFile(testkit.BuildBinary(t))
	if err != nil {
		t.Fatal(err)
	}
	for path, file := range map[string][]byte{binary: content, filepath.Join(root, ".claude-plugin", "plugin.json"): []byte(`{"name": "heavy", "version": "` + version + `"}`)} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, file, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := (herdr.Plugin{Data: data, Version: present, Heavy: "/plugins/heavy/" + present + "/bin/heavy"}).Generate(); err != nil {
		t.Fatal(err)
	}
	return binary
}

func manifestOf(t *testing.T, data string) string {
	t.Helper()
	content, err := os.ReadFile(herdr.Plugin{Data: data}.ManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

// FR-35, G12: the session start of a newer heavy writes the manifest with the absolute path of its bin/heavy
func TestSessionStartHookGeneratesTheHerdrPluginOfThisHeavy(t *testing.T) {
	data := t.TempDir()
	binary := installedHeavy(t, "1.0.1", data, "1.0.0")
	world := newHerdrWorld(t, "{}", "CLAUDE_PLUGIN_DATA="+data, "CLAUDE_ENV_FILE="+filepath.Join(t.TempDir(), "env.sh"), "HEAVY_CLAUDE_PID=4242")

	got := world.run(t, binary, `{"session_id": "example-session", "cwd": "`+t.TempDir()+`"}`, "--hook", "session-start")

	checkHerdr(t, "outcome", got, outcome{})
	checkHerdr(t, "manifest", manifestOf(t, data), herdr.Plugin{Data: data, Version: "1.0.1", Heavy: binary}.Manifest())
}

// G12: after /reload-plugins the guard of the newer heavy writes the manifest again
func TestGuardHookAfterAReloadRewritesTheHerdrPlugin(t *testing.T) {
	data := t.TempDir()
	binary := installedHeavy(t, "1.0.1", data, "1.0.0")
	world := newHerdrWorld(t, "{}", "CLAUDE_PLUGIN_DATA="+data)

	got := world.run(t, binary, `{"tool_name": "Bash", "cwd": "`+t.TempDir()+`", "session_id": "example-session", "tool_input": {"command": "git status"}}`, "--hook", "guard")

	checkHerdr(t, "outcome", got, outcome{})
	checkHerdr(t, "manifest", manifestOf(t, data), herdr.Plugin{Data: data, Version: "1.0.1", Heavy: binary}.Manifest())
}
