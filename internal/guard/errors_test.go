package guard_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A list heavy cannot read whole never denies a call. Up to 1.2.0 the guard denied every Bash call of a session
// whose list held something it did not know, such as load.unpredicted_peak_bytes of 1.2.0 for the guard of 1.1.0.
func TestBrokenProjectListLetsTheCallPassAndLogsWhatItLeftOut(t *testing.T) {
	_, worktree := checkouts(t)
	path := filepath.Join(worktree, ".heavy.json")
	if err := os.WriteFile(path, []byte(`{"commands": "make ci"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	current := newWorld(t)

	got := current.run(t, bash("make ci", worktree, false))

	check(t, "answer", got, nil)
	check(t, "log", current.log.String(), `{"level":"WARN","msg":"a list holds something heavy does not use","warning":"heavy: `+path+
		`: cannot use \"commands\": \"make ci\", expected a list; heavy reads no command from it"}`+"\n")
}

func TestPersonalListHeavyCannotReadLetsEveryCallPass(t *testing.T) {
	_, worktree := listed(t)
	for name, content := range map[string]string{"broken value": `{"projects": []}`, "no JSON": `{"load": `, "key of a newer heavy": `{"load": {"key_of_a_newer_heavy": 1}}`} {
		t.Run(name, func(t *testing.T) {
			current := newWorld(t).withPersonal(t, content)

			got := current.run(t, bash("git status", worktree, false))

			check(t, "answer", got, nil)
		})
	}
}

// The guard queues a heavy call as ever and tells the session what the list holds that this heavy does not use.
func TestPersonalListOfANewerHeavyStillQueuesAndSaysWhatItLeftOut(t *testing.T) {
	_, worktree := listed(t)
	current := newWorld(t).withPersonal(t, `{"load": {"max_parallel": 4, "key_of_a_newer_heavy": 1}}`)

	got := current.run(t, bash("make ci", worktree, true))

	note := " heavy read a list only in part: heavy: " + current.personal + `: unknown key "load.key_of_a_newer_heavy", ignored; a newer heavy may know it.`
	check(t, "answer", got, rewrite(rewritten(fakeHeavy+" make ci"), contextOf("`make ci`", inFront, free, fakeHeavy+" make ci")+note))
}

// plan 4.1: Bash itself reports a call that is no valid Bash.
func TestCallThatIsNoValidBashPassesUnchanged(t *testing.T) {
	_, worktree := listed(t)
	current := newWorld(t)

	got := current.run(t, bash(`make ci "`, worktree, false))

	check(t, "answer", got, nil)
	check(t, "log", current.log.String(),
		`{"level":"WARN","msg":"the call is no valid Bash, so the guard lets it run for Bash to report","error":"1:9: reached EOF without closing quote `+"`"+`\"`+"`"+`"}`+"\n")
}

// test_heavy_guard.py:555
func TestUpdatedInputKeepsEveryOtherFieldOfTheToolInput(t *testing.T) {
	_, worktree := listed(t)
	input := hookInput{
		ToolName: "Bash", Cwd: worktree,
		ToolInput: toolInput{Command: "make ci", Description: "Run the CI", Timeout: 600000, DisableSandbox: true},
	}

	got := updated(newWorld(t).run(t, input))

	check(t, "updated input", got, &toolInput{
		Command: queued(fakeHeavy + " make ci"), Description: "Run the CI", Timeout: 600000, RunInBackground: true, DisableSandbox: true,
	})
}

func TestHookInputTheGuardCannotReadIsDenied(t *testing.T) {
	form := `heavy: hook input: expected a JSON object with a string "tool_name", and for Bash a string "cwd" and ` +
		`a "tool_input" object with a string "command", got `
	for _, current := range []struct{ input, reason string }{
		{`{"tool_name": "Bash", "cwd": "/sites/main", "tool_input": {}}`, form + `{"tool_name":"Bash","cwd":"/sites/main","tool_input":{}}`},
		{`{"tool_name": "Bash", "cwd": "/sites/main", "tool_input": {"command": 1}}`, form + `{"tool_name":"Bash","cwd":"/sites/main","tool_input":{"command":1}}`},
		{`{"tool_name": "Bash", "tool_input": {"command": "ls"}}`, form + `{"tool_name":"Bash","tool_input":{"command":"ls"}}`},
		{`{"tool_input": {"command": "ls"}}`, form + `{"tool_input":{"command":"ls"}}`},
		{`[1]`, form + `[1]`},
		{`{"tool_name": "Bash",`, form + `invalid JSON (unexpected end of JSON input): {"tool_name": "Bash",`},
	} {
		t.Run(current.input, func(t *testing.T) {
			check(t, "answer", decodeAnswer(t, newWorld(t).raw(t, current.input)), denial(current.reason))
		})
	}
}

func TestPollWithoutASessionIsDenied(t *testing.T) {
	_, worktree := listed(t)
	input := bash("heavy --status", worktree, false)
	input.SessionID = ""

	got := newWorld(t).run(t, input)

	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	check(t, "answer", got, denial(`heavy: hook input: expected a string "session_id" to find the session's own runs, got `+string(data)))
}
