package guard_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/andreasisaak/heavy/internal/lists"
)

// test_heavy_guard.py:544; 0.6 raised a ValueError and let the call run, Go denies it (design G15).
func TestBrokenProjectListIsDeniedWithPathAndExpectedForm(t *testing.T) {
	for _, current := range []struct{ content, compact string }{
		{`{"commands": "make ci"}`, `{"commands":"make ci"}`},
		{`{"commands": [1]}`, `{"commands":[1]}`},
	} {
		t.Run(current.content, func(t *testing.T) {
			_, worktree := checkouts(t)
			path := filepath.Join(worktree, ".heavy.json")
			if err := os.WriteFile(path, []byte(current.content), 0o644); err != nil {
				t.Fatal(err)
			}

			got := guardOf(t, "make ci", worktree, false)

			check(t, "answer", got, denial("heavy: "+path+": expected "+lists.ProjectListForm+", got "+current.compact))
		})
	}
}

func TestBrokenPersonalListIsDenied(t *testing.T) {
	_, worktree := listed(t)
	current := newWorld(t).withPersonal(t, `{"projects": []}`)

	got := current.run(t, bash("git status", worktree, false))

	check(t, "answer", got, denial("heavy: "+current.personal+": expected "+lists.PersonalForm+`, got {"projects":[]}`))
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
