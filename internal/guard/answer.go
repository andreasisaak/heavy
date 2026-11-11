package guard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"
)

// answer is the hook output: a rewrite (updatedInput and additionalContext, no permission decision) or a denial.
type answer struct {
	HookSpecificOutput output `json:"hookSpecificOutput"`
}

type output struct {
	HookEventName            string                     `json:"hookEventName"`
	PermissionDecision       string                     `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string                     `json:"permissionDecisionReason,omitempty"`
	UpdatedInput             map[string]json.RawMessage `json:"updatedInput,omitempty"`
	AdditionalContext        string                     `json:"additionalContext,omitempty"`
}

func denial(reason string) *answer {
	return &answer{output{HookEventName: "PreToolUse", PermissionDecision: "deny", PermissionDecisionReason: reason}}
}

// rewrite sends the call to the background with the context that names the queue; a call in the foreground goes
// into { … } that echoes its exit status, so the session reads it when it is woken.
// Without a monitor of the session, whose session the hook input names, the context ends with how to start one
// (FR-29).
func rewrite(tool toolInput, command, how string, targets []string, session string, deps Dependencies) (*answer, error) {
	if !tool.background {
		body := terminated(command)
		if !strings.HasSuffix(body, "\n") {
			body += " "
		}
		command = "{ " + body + `}; exit_status=$?; echo "heavy $exit_status"; exit $exit_status`
		how += sentToBackground[how]
	}
	status, err := deps.Queue()
	if err != nil {
		return nil, err
	}
	fields, err := tool.updated(command)
	if err != nil {
		return nil, err
	}
	context := fmt.Sprintf(contextText, targetList(targets), how, queueStatus(status, deps.Now()), command)
	if session != "" && !slices.Contains(status.Monitors, session) {
		context += fmt.Sprintf(noMonitorNote, session)
	}
	return &answer{output{HookEventName: "PreToolUse", UpdatedInput: fields, AdditionalContext: context}}, nil
}

// terminated is the command as the last list inside { … }: it ends in `;`, or in `&` when the shell sends it to
// the background. A command of several lines or with a comment ends in a line break instead: a `;` behind a
// comment or behind the end of a heredoc would be part of them.
func terminated(command string) string {
	if strings.ContainsAny(command, "\n#") {
		return strings.TrimRightFunc(command, unicode.IsSpace) + "\n"
	}
	command = strings.TrimRightFunc(strings.TrimRight(strings.TrimRightFunc(command, unicode.IsSpace), ";"), unicode.IsSpace)
	if strings.HasSuffix(command, "&") && !strings.HasSuffix(command, "&&") {
		return command
	}
	return command + ";"
}

// warned adds to a rewrite what the lists hold that heavy does not use, so the session can tell the user.
func warned(response *answer, warnings []string) *answer {
	if len(warnings) == 0 || response == nil || response.HookSpecificOutput.AdditionalContext == "" {
		return response
	}
	response.HookSpecificOutput.AdditionalContext += " heavy read a list only in part: " + strings.Join(warnings, "; ") + "."
	return response
}

// noting adds the note on the removed filters to a rewrite.
func noting(response *answer, removed []string) *answer {
	if len(removed) == 0 || response == nil || response.HookSpecificOutput.AdditionalContext == "" {
		return response
	}
	response.HookSpecificOutput.AdditionalContext += fmt.Sprintf(filtersNote, targetList(removed))
	return response
}

// targetList is "`make ci`, `make test`", each text once.
func targetList(texts []string) string {
	var quoted []string
	seen := map[string]bool{}
	for _, text := range texts {
		if !seen[text] {
			seen[text] = true
			quoted = append(quoted, "`"+text+"`")
		}
	}
	return strings.Join(quoted, ", ")
}

// encode writes a value as JSON without escaping <, > and &, which commands are full of.
func encode[T string | answer](value T) (json.RawMessage, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("heavy: encoding the hook answer: %w", err)
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

func write(stdout io.Writer, response answer) error {
	encoded, err := encode(response)
	if err != nil {
		return err
	}
	if _, err := stdout.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("heavy: writing the hook answer: %w", err)
	}
	return nil
}
