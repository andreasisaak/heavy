package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/andreasisaak/heavy/internal/testkit"
)

// installed makes the plugin folder of a heavy of the given version, as Claude Code keeps one per version: the
// built binary under bin/, its stamp, the manifest and a shim.
func installed(t *testing.T, version, stamp string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, ".claude-plugin", "plugin.json"), `{"name": "heavy", "version": "`+version+`"}`)
	write(t, filepath.Join(root, "bin", ".stamp"), stamp+"\n")
	binary, err := os.ReadFile(testkit.BuildBinary(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "heavy"), binary, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "shims"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../bin/heavy", filepath.Join(root, "shims", "make")); err != nil {
		t.Fatal(err)
	}
	return root
}

// shimOfTheSession is the program a shell with the environment of the session finds for make, with links resolved.
func shimOfTheSession(t *testing.T, envFile string) string {
	t.Helper()
	command := exec.Command("/bin/sh", "-c", `. "$1"; command -v make`, "sh", envFile)
	command.Env = []string{"PATH=/usr/bin:/bin"}
	found, err := command.Output()
	if err != nil {
		t.Fatalf("finding make with the environment of the session: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(string(found[:len(found)-1]))
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// A session started with an older heavy keeps its PATH for its whole life. An update installs a newer heavy in a
// folder of its own, and the first hook of the newer one, in whatever session, turns the link behind that PATH:
// the old session then runs the shims of the newer heavy without a restart. The hooks of the older heavy, which
// the old session may still run, do not turn the link back.
func TestASessionOfAnOlderHeavyRunsTheShimsOfANewerOneAfterItsFirstHook(t *testing.T) {
	older, newer := installed(t, "1.3.0", "aaa"), installed(t, "1.4.0", "bbb")
	envFile := filepath.Join(t.TempDir(), "env.sh")
	world := newHerdrWorld(t, "{}", "HEAVY_CLAUDE_PID=4242", "CLAUDE_ENV_FILE="+envFile)
	bash := `{"tool_name": "Bash", "cwd": "` + t.TempDir() + `", "session_id": "old-session", "tool_input": {"command": "git status"}}`

	start := world.run(t, filepath.Join(older, "bin", "heavy"), `{"session_id": "old-session", "cwd": "`+t.TempDir()+`"}`, "--hook", "session-start")
	atStart := shimOfTheSession(t, envFile)
	update := world.run(t, filepath.Join(newer, "bin", "heavy"), bash, "--hook", "guard")
	afterUpdate := shimOfTheSession(t, envFile)
	old := world.run(t, filepath.Join(older, "bin", "heavy"), bash, "--hook", "guard")
	afterOldHook := shimOfTheSession(t, envFile)

	started := outcome{stderr: "heavy: CLAUDE_PLUGIN_DATA is not set, so the herdr plugin is not generated\n"}
	if start != started || update != (outcome{}) || old != (outcome{}) {
		t.Fatalf("session start = %+v, hook of the newer heavy = %+v, hook of the older = %+v; want %+v and two silent passes", start, update, old, started)
	}
	got := []string{atStart, afterUpdate, afterOldHook}
	want := []string{filepath.Join(older, "bin", "heavy"), filepath.Join(newer, "bin", "heavy"), filepath.Join(newer, "bin", "heavy")}
	if got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("make of the session at its start, after the update, after a hook of the older heavy =\n%q\nwant\n%q", got, want)
	}
}

// The personal list is one file for every heavy of the machine. A newer heavy brings a key, the user sets it, and
// the sessions that still run the older heavy read the same file with every Bash call and every make or docker
// call: their hook passes the call and their shim runs the program, and both go on with what they know.
func TestTheHookAndTheShimOfAnOlderHeavyPassCallsWithAListOfANewerOne(t *testing.T) {
	older := installed(t, "1.3.0", "aaa")
	world := newHerdrWorld(t, "{}")
	home := ""
	for _, entry := range world.env {
		if value, found := strings.CutPrefix(entry, "HOME="); found {
			home = value
		}
	}
	write(t, filepath.Join(home, ".config", "heavy", "config.json"),
		`{"load": {"max_parallel": 4, "key_of_a_newer_heavy": 8589934592}, "section_of_a_newer_heavy": {"enabled": true}}`)
	tools := t.TempDir()
	testkit.FakeProgram(t, tools, "make", `echo "the real make got $*"`)
	world.env = append(world.env, "PATH="+filepath.Join(older, "shims")+":"+tools+":/usr/bin:/bin")
	cwd := t.TempDir()

	hook := world.run(t, filepath.Join(older, "bin", "heavy"), `{"tool_name": "Bash", "cwd": "`+cwd+`", "session_id": "old-session", "tool_input": {"command": "git status"}}`, "--hook", "guard")
	shim := world.run(t, filepath.Join(older, "shims", "make"), "", "--version")

	list := filepath.Join(home, ".config", "heavy", "config.json")
	wantHook := outcome{stderr: `level=WARN msg="a list holds something heavy does not use" warning="heavy: ` + list + `: unknown key \"load.key_of_a_newer_heavy\", ignored; a newer heavy may know it"` + "\n" +
		`level=WARN msg="a list holds something heavy does not use" warning="heavy: ` + list + `: unknown key \"section_of_a_newer_heavy\", ignored; a newer heavy may know it"` + "\n"}
	hook.stderr = regexp.MustCompile(`(?m)^time=\S+ `).ReplaceAllString(hook.stderr, "")
	if hook != wantHook {
		t.Fatalf("the hook = %+v\nwant     %+v", hook, wantHook)
	}
	if want := (outcome{stdout: "the real make got --version\n"}); shim != want {
		t.Fatalf("the shim = %+v, want %+v", shim, want)
	}
}
