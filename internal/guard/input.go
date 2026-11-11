package guard

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// hookInputForm is the hook input the guard needs, as its error messages name it.
const hookInputForm = `a JSON object with a string "tool_name", and for Bash a string "cwd" and a "tool_input" object with a string "command"`

// hookInput holds the fields of the PreToolUse input the guard reads. Claude Code sends more (transcript_path,
// permission_mode, tool_use_id, …) and adds fields between releases, so the decoding ignores unknown fields on
// purpose instead of refusing them.
type hookInput struct {
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
	Cwd       string          `json:"cwd"`
	SessionID string          `json:"session_id"`
	// AgentID is there for a call of a subagent; only its presence counts.
	AgentID json.RawMessage `json:"agent_id"`
	// AgentType names the agent of a subagent or of a session started with --agent.
	AgentType *string `json:"agent_type"`
	// raw is the input as read, for error messages.
	raw []byte
}

// toolInput is the input of the Bash tool: every field as read, so the rewrite keeps the ones the guard does not
// know, and the two it reads.
type toolInput struct {
	fields     map[string]json.RawMessage
	command    string
	background bool
}

// decodeHookInput reads the hook input; tool_name must be there.
func decodeHookInput(data []byte) (hookInput, error) {
	var input hookInput
	if !isObject(data) || json.Unmarshal(data, &input) != nil || input.ToolName == "" {
		return hookInput{}, inputError(data)
	}
	input.raw = data
	return input, nil
}

// bashInput reads cwd and the tool input of a Bash call; run_in_background counts only as JSON true, as in 0.6.
func (h hookInput) bashInput() (toolInput, error) {
	var fields map[string]json.RawMessage
	if h.Cwd == "" || !isObject(h.ToolInput) || json.Unmarshal(h.ToolInput, &fields) != nil {
		return toolInput{}, inputError(h.raw)
	}
	var command string
	if raw, found := fields["command"]; !found || !bytes.HasPrefix(bytes.TrimSpace(raw), []byte(`"`)) || json.Unmarshal(raw, &command) != nil {
		return toolInput{}, inputError(h.raw)
	}
	background := false
	if raw, found := fields["run_in_background"]; found {
		background = bytes.Equal(bytes.TrimSpace(raw), []byte("true"))
	}
	return toolInput{fields: fields, command: command, background: background}, nil
}

// subagent reports a call of a subagent.
func (h hookInput) subagent() bool {
	return len(h.AgentID) > 0
}

// session is the session id, which the guard needs to find the session's own runs in the queue.
func (h hookInput) session() (string, error) {
	if h.SessionID == "" {
		return "", fmt.Errorf(`heavy: hook input: expected a string "session_id" to find the session's own runs, got %s`, shown(h.raw))
	}
	return h.SessionID, nil
}

// updated is the tool input with the rewritten command, sent to the background.
func (t toolInput) updated(command string) (map[string]json.RawMessage, error) {
	encoded, err := encode(command)
	if err != nil {
		return nil, err
	}
	fields := make(map[string]json.RawMessage, len(t.fields)+1)
	for name, value := range t.fields {
		fields[name] = value
	}
	fields["command"] = encoded
	fields["run_in_background"] = json.RawMessage("true")
	return fields, nil
}

func isObject(data []byte) bool {
	return bytes.HasPrefix(bytes.TrimSpace(data), []byte("{"))
}

func inputError(data []byte) error {
	return fmt.Errorf("heavy: hook input: expected %s, got %s", hookInputForm, shown(data))
}

// shown is the hook input as error messages show it: compact JSON, or the text read with the JSON error.
func shown(data []byte) string {
	var buffer bytes.Buffer
	if err := json.Compact(&buffer, data); err != nil {
		return fmt.Sprintf("invalid JSON (%v): %s", err, data)
	}
	return buffer.String()
}
