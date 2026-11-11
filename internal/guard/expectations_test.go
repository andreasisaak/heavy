package guard_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// The expected answers of the cases.

func queued(command string) string {
	return "{ " + command + `; }; exit_status=$?; echo "heavy $exit_status"; exit $exit_status`
}

func rewritten(command string) *toolInput {
	return &toolInput{Command: command, Description: "example", RunInBackground: true}
}

func updated(response *answer) *toolInput {
	if response == nil {
		return nil
	}
	return response.HookSpecificOutput.UpdatedInput
}

func rewrite(input *toolInput, context string) *answer {
	return &answer{output{HookEventName: "PreToolUse", UpdatedInput: input, AdditionalContext: context}}
}

func denial(reason string) *answer {
	return &answer{output{HookEventName: "PreToolUse", PermissionDecision: "deny", PermissionDecisionReason: reason}}
}

// contextOf is the additional context of a rewrite.
func contextOf(targets, how, status, command string) string {
	return "Queued behind heavy: " + targets + ". " + how + ", so it starts as soon as this machine has room for it. " +
		status + " " + keepWorking + " " + targeted + " Running: " + command
}

func show(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return err.Error()
	}
	return string(data)
}

func check[T any](t *testing.T, what string, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s\n got %s\nwant %s", what, show(got), show(want))
	}
}

func runOf(state string, since int64, directory, command string) protocol.RunStatus {
	return protocol.RunStatus{State: state, PID: 1, Since: since, Directory: directory, Command: command}
}
