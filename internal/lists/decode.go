package lists

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
)

// notes collects what a list holds that heavy does not use. A list is read as far as it can be: a key heavy does
// not know is left out, since a newer heavy may have written it, and a value heavy cannot use costs only what it
// stands for. Neither stops a call: a session of an older heavy keeps working beside a list written for a newer.
type notes struct {
	path     string
	warnings []string
	// broken is set by a value or a file heavy cannot use; an unknown key alone leaves it false.
	broken bool
}

// unknown notes a key heavy does not know.
func (n *notes) unknown(key string) {
	n.warnings = append(n.warnings, fmt.Sprintf("heavy: %s: unknown key %q, ignored; a newer heavy may know it", n.path, key))
}

// unusable notes a value heavy cannot use, what it expected and what it does instead.
func (n *notes) unusable(key string, raw json.RawMessage, expected, instead string) {
	shown := "nothing"
	if raw != nil {
		var compact bytes.Buffer
		if json.Compact(&compact, raw) == nil {
			shown = compact.String()
		}
	}
	n.warnings = append(n.warnings, fmt.Sprintf("heavy: %s: cannot use %q: %s, expected %s; %s", n.path, key, shown, expected, instead))
	n.broken = true
}

// unreadable notes a file heavy reads nothing from.
func (n *notes) unreadable(what string) {
	n.warnings = append(n.warnings, fmt.Sprintf("heavy: %s: %s; heavy reads nothing from it", n.path, what))
	n.broken = true
}

// readObject reads a list file as a JSON object; found is false, with a note, for a file that cannot be read, is
// no JSON or no object.
func readObject(n *notes) (fields map[string]json.RawMessage, found bool) {
	data, err := os.ReadFile(n.path)
	if err != nil {
		var pathError *os.PathError
		if errors.As(err, &pathError) {
			n.unreadable(pathError.Op + " " + pathError.Path + ": " + pathError.Err.Error())
		} else {
			n.unreadable(err.Error())
		}
		return nil, false
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		n.unreadable(fmt.Sprintf("invalid JSON (%v)", err))
		return nil, false
	}
	fields, isObject := decodeObject(data)
	if !isObject {
		n.unreadable("expected a JSON object, got " + compact.String())
		return nil, false
	}
	return fields, true
}

// sortedKeys are the keys of an object in the order the notes name them.
func sortedKeys(fields map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func failure(format string, arguments ...any) error {
	return errors.New("heavy: " + fmt.Sprintf(format, arguments...))
}

// decodeObject reads a JSON object into its raw values; null or any other value is no object.
func decodeObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return nil, false
	}
	var fields map[string]json.RawMessage
	return fields, json.Unmarshal(raw, &fields) == nil
}

// decodeArray reads a JSON array into its raw values; null or any other value is no array.
func decodeArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
		return nil, false
	}
	var values []json.RawMessage
	return values, json.Unmarshal(raw, &values) == nil
}

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func decodeString(raw json.RawMessage, target *string) bool {
	return !isNull(raw) && json.Unmarshal(raw, target) == nil
}

func decodeBool(raw json.RawMessage, target *bool) bool {
	return !isNull(raw) && json.Unmarshal(raw, target) == nil
}

// decodeWords reads a non-empty array of non-empty strings.
func decodeWords(raw json.RawMessage, target *[]string) bool {
	values, isArray := decodeArray(raw)
	if !isArray || len(values) == 0 {
		return false
	}
	words := make([]string, 0, len(values))
	for _, value := range values {
		var word string
		if !decodeString(value, &word) || word == "" {
			return false
		}
		words = append(words, word)
	}
	*target = words
	return true
}
