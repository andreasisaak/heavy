// Package stophook is the PreToolUse hook for TaskStop and KillShell (`heavy --hook stop`): a speed bump before
// a session stops its own heavy run.
//
// Sessions stopped background runs that waited in the queue, to start them again after a fix, and lost their
// place; a waiting run reads the files only when it starts, so it checks such a fix without a restart. Others
// stopped `make ci` after a quarter of an hour of work. The first stop of a task whose Bash call the guard queued
// (its hook context starts with "Queued behind heavy" in the session transcript) is denied while a run of the
// session waits or runs, with what stopping costs. The same stop again within ten minutes passes: the session has
// read the cost and still wants it, for example because the user asked.
package stophook

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"time"

	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/statusview"
)

const stopReason = "Task %s is a heavy run of this session: %s. %s " +
	"A waiting run reads the files only when it starts, so it checks a fix made while it waits without a restart. " +
	"Stop it only if its command is wrong or the user asked for it; then stop it again within 10 minutes, and that stop passes."

// plainID is the form of a task id the hook accepts: it names the stop marker file.
var plainID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Dependencies are what the hook reads besides its input: the state directory that holds the stop markers, the
// queue of the daemon and the clock.
type Dependencies struct {
	StateDirectory string
	Queue          func() ([]protocol.RunStatus, error)
	Now            func() time.Time
}

// hookInput is the part of the PreToolUse input the hook reads. Claude Code adds fields to it over time, so
// unknown fields are allowed.
type hookInput struct {
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	SessionID      string          `json:"session_id"`
	TranscriptPath string          `json:"transcript_path"`
}

// taskInput is the tool input of TaskStop (task_id) or KillShell (shell_id).
type taskInput struct {
	TaskID  string `json:"task_id"`
	ShellID string `json:"shell_id"`
}

type answer struct {
	HookSpecificOutput decision `json:"hookSpecificOutput"`
}

type decision struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

// Run reads the hook input from stdin and writes a denial to stdout, or nothing when the stop passes. A problem
// of the hook itself denies the stop with the problem (design G15); the error is only a denial it could not write.
func Run(stdin io.Reader, stdout io.Writer, deps Dependencies) error {
	reason, err := decide(stdin, deps)
	if err != nil {
		reason = err.Error()
	}
	if reason == "" {
		return nil
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	denial := answer{decision{HookEventName: "PreToolUse", PermissionDecision: "deny", PermissionDecisionReason: reason}}
	if err := encoder.Encode(denial); err != nil {
		return fmt.Errorf("heavy: writing the hook answer: %w", err)
	}
	return nil
}

// decide is the reason to deny the stop, or "" when it passes.
func decide(stdin io.Reader, deps Dependencies) (string, error) {
	input, task, err := readInput(stdin)
	if err != nil {
		return "", err
	}
	queued, err := queuedByGuard(input.TranscriptPath, task)
	if err != nil || !queued {
		return "", err
	}
	queue, err := deps.Queue()
	if err != nil {
		return "", err
	}
	own := statusview.Own(queue, input.SessionID)
	if len(own) == 0 {
		return "", nil
	}
	now := deps.Now()
	repeated, err := repeatedStop(filepath.Join(deps.StateDirectory, "stops"), task, now)
	if err != nil || repeated {
		return "", err
	}
	return fmt.Sprintf(stopReason, task, describeAll(own, now), cost(own)), nil
}

// readInput decodes the hook input and returns it with the task id it names.
func readInput(stdin io.Reader) (hookInput, string, error) {
	data, err := io.ReadAll(stdin)
	if err != nil {
		return hookInput{}, "", fmt.Errorf("heavy: reading the hook input: %w", err)
	}
	var input hookInput
	if err := json.Unmarshal(data, &input); err != nil {
		return hookInput{}, "", fmt.Errorf("heavy: reading the hook input: %w", err)
	}
	if len(input.ToolInput) == 0 {
		return hookInput{}, "", fmt.Errorf("heavy: the hook input of %s has no tool_input", input.ToolName)
	}
	var fields taskInput
	if err := json.Unmarshal(input.ToolInput, &fields); err != nil {
		return hookInput{}, "", fmt.Errorf("heavy: expected task_id or shell_id as a string in the input of %s, got %s", input.ToolName, input.ToolInput)
	}
	task := fields.TaskID
	if task == "" {
		task = fields.ShellID
	}
	switch {
	case task == "":
		return hookInput{}, "", fmt.Errorf("heavy: expected task_id or shell_id in the input of %s, got %s", input.ToolName, input.ToolInput)
	case !plainID.MatchString(task):
		return hookInput{}, "", fmt.Errorf("heavy: task id %s is not a plain id", task)
	case input.TranscriptPath == "":
		return hookInput{}, "", fmt.Errorf("heavy: the hook input of %s has no transcript_path", input.ToolName)
	case input.SessionID == "":
		return hookInput{}, "", fmt.Errorf("heavy: the hook input of %s has no session_id", input.ToolName)
	}
	return input, task, nil
}
