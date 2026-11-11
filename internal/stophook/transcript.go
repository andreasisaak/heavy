package stophook

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// queuedMark starts the context the guard gives a Bash call it queued.
const queuedMark = "Queued behind heavy"

// guardAttachments are the transcript attachments that carry a hook's context for a tool call.
var guardAttachments = map[string]bool{"hook_additional_context": true, "hook_success": true}

type entry struct {
	Attachment json.RawMessage `json:"attachment"`
	Message    json.RawMessage `json:"message"`
}

type attachment struct {
	Type      string  `json:"type"`
	ToolUseID *string `json:"toolUseID"`
}

type message struct {
	Content json.RawMessage `json:"content"`
}

type part struct {
	Type      string          `json:"type"`
	ToolUseID *string         `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

// search collects, line by line, the tool calls the guard queued and the call whose result started the task.
type search struct {
	started []byte
	queued  map[string]bool
	call    string
	found   bool
}

// queuedByGuard reports whether the Bash call that started task got the guard's context, as the session
// transcript at path records it. The last tool result that names the task wins.
func queuedByGuard(path, task string) (bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("heavy: found no transcript at %s", path)
	}
	if err != nil {
		return false, fmt.Errorf("heavy: reading the transcript %s: %w", path, err)
	}
	defer file.Close()
	current := search{started: []byte("with ID: " + task + "."), queued: map[string]bool{}}
	reader := bufio.NewReader(file)
	for number := 1; ; number++ {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if err := current.read(line); err != nil {
				return false, fmt.Errorf("heavy: %s:%d: %w", path, number, err)
			}
		}
		if err == io.EOF {
			return current.found && current.queued[current.call], nil
		}
		if err != nil {
			return false, fmt.Errorf("heavy: reading the transcript %s: %w", path, err)
		}
	}
}

// read takes one transcript line; only lines with the guard's mark or the task's start are decoded.
func (s *search) read(line []byte) error {
	marked, started := bytes.Contains(line, []byte(queuedMark)), bytes.Contains(line, s.started)
	if !marked && !started {
		return nil
	}
	var current entry
	if err := decode(line, &current, "the entry"); err != nil {
		return err
	}
	if marked {
		if err := s.readAttachment(current.Attachment); err != nil {
			return err
		}
	}
	if started {
		return s.readMessage(current.Message)
	}
	return nil
}

func (s *search) readAttachment(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var current attachment
	if err := decode(raw, &current, "the attachment"); err != nil {
		return err
	}
	if !guardAttachments[current.Type] {
		return nil
	}
	if current.ToolUseID == nil {
		return fmt.Errorf("the %s attachment has no toolUseID", current.Type)
	}
	s.queued[*current.ToolUseID] = true
	return nil
}

// readMessage takes the tool results of a message whose content is a list of parts.
func (s *search) readMessage(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var current message
	if err := decode(raw, &current, "the message"); err != nil {
		return err
	}
	if len(current.Content) == 0 || current.Content[0] != '[' {
		return nil
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(current.Content, &parts); err != nil {
		return fmt.Errorf("expected the message content as a JSON array, got %w", err)
	}
	for _, raw := range parts {
		var result part
		if err := decode(raw, &result, "the message content part"); err != nil {
			return err
		}
		if result.Type != "tool_result" || !bytes.Contains(result.Content, s.started) {
			continue
		}
		if result.ToolUseID == nil {
			return errors.New("the tool_result has no tool_use_id")
		}
		s.call, s.found = *result.ToolUseID, true
	}
	return nil
}

// decode reads the JSON object raw into target; what names the value in the error.
func decode(raw []byte, target any, what string) error {
	err := json.Unmarshal(raw, target)
	var typeError *json.UnmarshalTypeError
	if errors.As(err, &typeError) && typeError.Field == "" {
		return fmt.Errorf("expected %s as a JSON object, got %s", what, typeError.Value)
	}
	if err != nil {
		return fmt.Errorf("expected %s as a JSON object, got %w", what, err)
	}
	return nil
}
