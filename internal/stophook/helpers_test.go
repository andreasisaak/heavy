package stophook_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/stophook"
	"github.com/andreasisaak/heavy/internal/testkit"
)

const (
	session    = "5968af83-example"
	queuedTask = "bq7heavy1"
	otherTask  = "bdevserve"
	advice     = "A waiting run reads the files only when it starts, so it checks a fix made while it waits without a restart. " +
		"Stop it only if its command is wrong or the user asked for it; then stop it again within 10 minutes, and that stop passes."
)

var now = time.Unix(1_800_000_000, 0)

// fixture is one session: its transcript, the state directory and how often the hook asked for the queue.
type fixture struct {
	directory  string
	transcript string
	state      string
	queueCalls int
}

// newFixture writes a session transcript with a call the guard queued and a dev server it did not touch.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	directory := t.TempDir()
	lines := []string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_ci","name":"Bash","input":{"command":"make ci"}}]}}`,
		`{"type":"attachment","attachment":{"type":"hook_additional_context","toolUseID":"toolu_ci","content":["Queued behind heavy: ` + "`make ci`" + `. …"]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_ci","content":"Command running in background with ID: ` + queuedTask + `. Output is being written to: /tmp/ci.output"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_dev","name":"Bash","input":{"command":"pnpm dev"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_dev","content":"Command running in background with ID: ` + otherTask + `. Output is being written to: /tmp/dev.output"}]}}`,
		`{"type":"user","message":{"content":"plain text of the user"}}`,
	}
	transcript := filepath.Join(directory, "session.jsonl")
	writeFile(t, transcript, strings.Join(lines, "\n")+"\n")
	return &fixture{directory: directory, transcript: transcript, state: testkit.StateDir(t)}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// own is a run of the session that started 5 minutes before now; a waiting one waits at position 2.
func own(state string) protocol.RunStatus {
	run := protocol.RunStatus{State: state, PID: 7, Since: now.Unix() - 300, Directory: "/work/app", Command: "make ci", Session: session}
	if state == "waiting" {
		run.Position = 2
	}
	return run
}

// input is the hook input of a TaskStop call that names the task in field.
func (f *fixture) input(t *testing.T, field, task string) string {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"session_id": session, "transcript_path": f.transcript, "cwd": f.directory, "permission_mode": "default",
		"hook_event_name": "PreToolUse", "tool_name": "TaskStop", "tool_input": map[string]string{field: task}, "tool_use_id": "toolu_stop",
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// stop runs the hook at the given time with the given input against a queue that answers queue.
func (f *fixture) stop(t *testing.T, input string, queue []protocol.RunStatus, at time.Time) string {
	t.Helper()
	return f.stopWith(t, input, func() ([]protocol.RunStatus, error) { return queue, nil }, at)
}

func (f *fixture) stopWith(t *testing.T, input string, queue func() ([]protocol.RunStatus, error), at time.Time) string {
	t.Helper()
	var stdout bytes.Buffer
	deps := stophook.Dependencies{
		StateDirectory: f.state,
		Queue: func() ([]protocol.RunStatus, error) {
			f.queueCalls++
			return queue()
		},
		Now: func() time.Time { return at },
	}
	if err := stophook.Run(strings.NewReader(input), &stdout, deps); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	return stdout.String()
}

// denial is the hook answer that denies the stop with reason, as Claude Code reads it.
func denial(t *testing.T, reason string) string {
	t.Helper()
	var quoted bytes.Buffer
	encoder := json.NewEncoder(&quoted)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(reason); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf(`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":%s}}`+"\n",
		strings.TrimSuffix(quoted.String(), "\n"))
}

func expect(t *testing.T, what, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s:\n got %q\nwant %q", what, got, want)
	}
}
