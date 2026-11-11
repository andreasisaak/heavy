// Package lists loads the lists that name a project's heavy commands: .heavy.json at the root
// of each checkout, and the personal list ~/.config/heavy/config.json, which adds commands per
// main checkout for all its worktrees, can switch a project's .heavy.json off, and holds the
// load thresholds of the queue. Either list turns the catalogue of heavy runners on.
//
// A list is read as far as it can be: what heavy does not know or cannot use is noted as a
// warning and left out, and never stops a call.
package lists

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// entryFields are the keys of an entry object; every key but "command" is optional.
var entryFields = map[string]bool{"command": true, "unless": true, "red": true, "exclusive": true, "restart": true}

// decodeEntry reads a command as a non-empty string or as an object with a command and its optional keys. An entry
// heavy cannot use is noted and left out as a whole, since a command queued without its exceptions or flags would
// not be the command the list names; a red pattern that does not compile and a key heavy does not know are noted
// and left out alone. where names the entry in the notes.
func decodeEntry(raw json.RawMessage, where string, n *notes) (Entry, bool) {
	var command string
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte(`"`)) {
		if json.Unmarshal(raw, &command) == nil && len(strings.Fields(command)) > 0 {
			return Entry{Words: split(command)}, true
		}
	}
	entry, patterns, valid := decodeEntryObject(raw)
	if !valid {
		n.unusable(where, raw, "a command as in .heavy.json", "heavy leaves this command out")
		return Entry{}, false
	}
	for index, pattern := range patterns {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			shown, _ := json.Marshal(pattern)
			n.unusable(fmt.Sprintf("%s.red[%d]", where, index), shown, fmt.Sprintf("an RE2 pattern (%v)", err), "heavy leaves this pattern out")
			continue
		}
		entry.Red = append(entry.Red, compiled)
	}
	fields, _ := decodeObject(raw)
	for _, key := range sortedKeys(fields) {
		if !entryFields[key] {
			n.unknown(where + "." + key)
		}
	}
	return entry, true
}

// decodeEntryObject reads the known keys of an entry object and its red patterns as written.
func decodeEntryObject(raw json.RawMessage) (entry Entry, patterns []string, valid bool) {
	fields, isObject := decodeObject(raw)
	var command string
	if !isObject || !decodeString(fields["command"], &command) || len(strings.Fields(command)) == 0 {
		return Entry{}, nil, false
	}
	entry = Entry{Words: split(command)}
	if value, found := fields["unless"]; found {
		if !decodeWords(value, &entry.Unless) || !allExceptions(entry.Unless) {
			return Entry{}, nil, false
		}
	}
	if value, found := fields["red"]; found && !decodeWords(value, &patterns) {
		return Entry{}, nil, false
	}
	for key, target := range map[string]*bool{"exclusive": &entry.Exclusive, "restart": &entry.Restart} {
		if value, found := fields[key]; found && !decodeBool(value, target) {
			return Entry{}, nil, false
		}
	}
	return entry, patterns, true
}

// decodeEntries reads a list of commands; one that is no list is noted and gives none.
func decodeEntries(raw json.RawMessage, where string, n *notes) []Entry {
	values, isArray := decodeArray(raw)
	if !isArray {
		n.unusable(where, raw, "a list", "heavy reads no command from it")
		return nil
	}
	var entries []Entry
	for index, value := range values {
		if entry, valid := decodeEntry(value, fmt.Sprintf("%s[%d]", where, index), n); valid {
			entries = append(entries, entry)
		}
	}
	return entries
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
