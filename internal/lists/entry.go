// Package lists loads the lists that name a project's heavy commands: .heavy.json at the root
// of each checkout, and the personal list ~/.config/heavy/config.json, which adds commands per
// main checkout for all its worktrees, can switch a project's .heavy.json off, and holds the
// load thresholds of the queue. Either list turns the catalogue of heavy runners on.
//
// A list of the wrong shape is an error that names the file, the expected shape and what it
// read.
package lists

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// Entry is a listed heavy command split into words, its program as a file name.
type Entry struct {
	Words []string
	// Unless lists the words that free a call from the entry: a word ending in "=" stands for
	// an assignment of that variable with a value, any other word must appear exactly.
	Unless []string
	// Red lists the patterns of a line that stops the run as red.
	Red []*regexp.Regexp
	// Exclusive runs start only while no other run is active and admit none beside them.
	Exclusive bool
	// Restart runs may be ended under memory pressure and queued again as #1.
	Restart bool
}

// namedAssignment is an exception ending in "=": it needs a variable name in front.
var namedAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=$`)

// entryFields are the keys of an entry object; every key but "command" is optional, and at least one must be there.
var entryFields = map[string]bool{"command": true, "unless": true, "red": true, "exclusive": true, "restart": true}

// rawEntry is an entry before its red patterns compile.
type rawEntry struct {
	entry    Entry
	patterns []string
}

// decodeEntry reads a command as a non-empty string or as an object with at least one optional key.
func decodeEntry(raw json.RawMessage) (rawEntry, bool) {
	var command string
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte(`"`)) {
		if json.Unmarshal(raw, &command) != nil || len(strings.Fields(command)) == 0 {
			return rawEntry{}, false
		}
		return rawEntry{entry: Entry{Words: split(command)}}, true
	}
	fields, isObject := decodeObject(raw)
	if !isObject || len(fields) < 2 || !onlyKeys(fields, entryFields) {
		return rawEntry{}, false
	}
	if !decodeString(fields["command"], &command) || len(strings.Fields(command)) == 0 {
		return rawEntry{}, false
	}
	decoded := rawEntry{entry: Entry{Words: split(command)}}
	if value, found := fields["unless"]; found {
		if !decodeWords(value, &decoded.entry.Unless) || !allExceptions(decoded.entry.Unless) {
			return rawEntry{}, false
		}
	}
	if value, found := fields["red"]; found {
		if !decodeWords(value, &decoded.patterns) {
			return rawEntry{}, false
		}
	}
	for key, target := range map[string]*bool{"exclusive": &decoded.entry.Exclusive, "restart": &decoded.entry.Restart} {
		if value, found := fields[key]; found && !decodeBool(value, target) {
			return rawEntry{}, false
		}
	}
	return decoded, true
}

// compile turns the red patterns into regular expressions; a pattern that does not compile is an error naming it.
func (raw rawEntry) compile(path string) (Entry, error) {
	entry := raw.entry
	for _, pattern := range raw.patterns {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return Entry{}, failure("%s: red pattern %s does not compile: %v", path, pattern, err)
		}
		entry.Red = append(entry.Red, compiled)
	}
	return entry, nil
}

// split cuts a command at whitespace and keeps its program as a file name.
func split(command string) []string {
	words := strings.Fields(command)
	words[0] = filepath.Base(words[0])
	return words
}

// allExceptions reports single words without whitespace, where one ending in "=" names a variable (CHECKALL_CASES=).
func allExceptions(words []string) bool {
	for _, word := range words {
		if strings.ContainsFunc(word, unicode.IsSpace) {
			return false
		}
		if strings.HasSuffix(word, "=") && !namedAssignment.MatchString(word) {
			return false
		}
	}
	return true
}
