package lists

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// document is a list file that holds valid JSON.
type document struct {
	path    string
	form    string
	data    []byte
	compact string
}

// readJSON reads a list file; invalid JSON is an error that names the file, the form and the text.
func readJSON(path, form string) (document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return document{}, failure("%v", err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		return document{}, failure("%s: expected %s, got invalid JSON (%v): %s", path, form, err, data)
	}
	return document{path: path, form: form, data: data, compact: compact.String()}, nil
}

// shapeError names the file, the expected form and the content read, as compact JSON.
func (d document) shapeError() error {
	return failure("%s: expected %s, got %s", d.path, d.form, d.compact)
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

func onlyKeys(fields map[string]json.RawMessage, allowed map[string]bool) bool {
	for key := range fields {
		if !allowed[key] {
			return false
		}
	}
	return true
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
