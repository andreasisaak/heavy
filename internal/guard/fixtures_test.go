package guard_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/guard"
	"github.com/andreasisaak/heavy/internal/herdr"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/testkit"
)

// The fixtures of hooks/test_heavy_guard.py.
const (
	fakeHeavy   = "/opt/heavy/bin/heavy"
	session     = "5968af83-example"
	free        = "No other heavy run is active, your run starts right away."
	targeted    = "A run that names one to three test files stays outside the queue."
	keepWorking = "You will be woken when your run ends: do not poll the queue, and do not stop the run to start it again. " +
		"The heavy monitor of the session reports the start, the first red line and the end of the run with its exit status, " +
		"so build no monitor, tail or watch loop of your own for it. " +
		"Until then keep working without asking the user for approval. " +
		"Change only files the run does not read, or it checks a different state than the one you commit: " +
		"a backend test does not read frontend files, and reading, planning the next task and reviewing the diff change nothing. " +
		"End your turn only when no such work is left."
	wholeShell = `"${SHELL:-/bin/bash}" -c`
	inFront    = "The hook put `heavy` in front of each heavy command"
	whole      = "The hook runs the whole command through `heavy`"
	direct     = "The call starts `heavy` itself"
)

var now = time.Unix(1_800_000_000, 0)

// toolInput is the Bash tool input of the cases; DisableSandbox stands for a field the guard does not know.
type toolInput struct {
	Command         string `json:"command"`
	Description     string `json:"description,omitempty"`
	Timeout         int    `json:"timeout,omitempty"`
	RunInBackground bool   `json:"run_in_background,omitempty"`
	DisableSandbox  bool   `json:"dangerouslyDisableSandbox,omitempty"`
}

type hookInput struct {
	ToolName  string    `json:"tool_name"`
	Cwd       string    `json:"cwd"`
	SessionID string    `json:"session_id,omitempty"`
	AgentID   string    `json:"agent_id,omitempty"`
	AgentType string    `json:"agent_type,omitempty"`
	ToolInput toolInput `json:"tool_input"`
}

type answer struct {
	HookSpecificOutput output `json:"hookSpecificOutput"`
}

type output struct {
	HookEventName            string     `json:"hookEventName"`
	PermissionDecision       string     `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string     `json:"permissionDecisionReason,omitempty"`
	UpdatedInput             *toolInput `json:"updatedInput,omitempty"`
	AdditionalContext        string     `json:"additionalContext,omitempty"`
}

// world is what the guard sees besides the hook input: the personal list, the queue, the containers and the log.
type world struct {
	personal   string
	queue      []protocol.RunStatus
	queueError error
	containers map[string]string
	queueCalls *int
	// monitors are the sessions with a connected monitor; newWorld connects one for the session of bash.
	monitors []string
	log      *bytes.Buffer
	// heavy is the heavy the rewrites name, fakeHeavy when empty.
	heavy string
	// herdr is the herdr plugin of the guard, none when its Data is empty.
	herdr herdr.Plugin
}

// newWorld has no personal list, a free queue and no container; HOME is a temporary directory.
func newWorld(t *testing.T) world {
	t.Helper()
	testkit.Home(t)
	return world{personal: filepath.Join(t.TempDir(), "config.json"), queueCalls: new(int), log: &bytes.Buffer{}, monitors: []string{session}}
}

func (w world) withQueue(runs ...protocol.RunStatus) world {
	w.queue = runs
	return w
}

// withPersonal writes the personal list ~/.config/heavy/config.json of the world.
func (w world) withPersonal(t *testing.T, content string) world {
	t.Helper()
	if err := os.WriteFile(w.personal, []byte(content), 0o644); err != nil {
		t.Fatalf("writing the personal list: %v", err)
	}
	return w
}

func (w world) dependencies(t *testing.T) guard.Dependencies {
	heavy := w.heavy
	if heavy == "" {
		heavy = fakeHeavy
	}
	return guard.Dependencies{
		Heavy:        heavy,
		PersonalList: w.personal,
		Queue: func() (protocol.Status, error) {
			*w.queueCalls++
			return protocol.Status{Runs: w.queue, Monitors: w.monitors}, w.queueError
		},
		Container: func(name string) (string, bool) {
			directory, found := w.containers[name]
			if !found {
				t.Errorf("asked for the directory of container %s", name)
			}
			return directory, found
		},
		Now:         func() time.Time { return now },
		Log:         slog.New(slog.NewJSONHandler(w.log, &slog.HandlerOptions{ReplaceAttr: withoutTime})),
		HerdrPlugin: w.herdr,
	}
}

func withoutTime(groups []string, attribute slog.Attr) slog.Attr {
	if len(groups) == 0 && attribute.Key == slog.TimeKey {
		return slog.Attr{}
	}
	return attribute
}

// raw runs the guard on stdin and returns what it wrote.
func (w world) raw(t *testing.T, stdin string) string {
	t.Helper()
	var stdout bytes.Buffer
	if err := guard.Run(strings.NewReader(stdin), &stdout, w.dependencies(t)); err != nil {
		t.Fatalf("guard.Run failed: %v", err)
	}
	return stdout.String()
}

// run runs the guard on the hook input and returns its answer, nil for no output.
func (w world) run(t *testing.T, input hookInput) *answer {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("encoding the hook input: %v", err)
	}
	return decodeAnswer(t, w.raw(t, string(data)))
}

func decodeAnswer(t *testing.T, stdout string) *answer {
	t.Helper()
	if stdout == "" {
		return nil
	}
	if !strings.HasSuffix(stdout, "}\n") || strings.Count(stdout, "\n") != 1 {
		t.Fatalf("output %q, want one line of JSON", stdout)
	}
	decoder := json.NewDecoder(strings.NewReader(stdout))
	decoder.DisallowUnknownFields()
	var decoded answer
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("output %q is no hook answer: %v", stdout, err)
	}
	return &decoded
}

func bash(command, cwd string, background bool) hookInput {
	return hookInput{
		ToolName: "Bash", Cwd: cwd, SessionID: session,
		ToolInput: toolInput{Command: command, Description: "example", RunInBackground: background},
	}
}

func subagent(command, cwd string, background bool) hookInput {
	input := bash(command, cwd, background)
	input.AgentID, input.AgentType = "a1b2c3", "test-author"
	return input
}

// agentSession is a session started with `--agent test-author`: agent_type without agent_id.
func agentSession(command, cwd string, background bool) hookInput {
	input := bash(command, cwd, background)
	input.AgentType = "test-author"
	return input
}

// guardOf runs the guard on a Bash call in a world without personal list and with a free queue.
func guardOf(t *testing.T, command, cwd string, background bool) *answer {
	t.Helper()
	return newWorld(t).run(t, bash(command, cwd, background))
}
