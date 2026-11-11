package shell

import (
	"regexp"
	"strings"
)

var unsafeCharacter = regexp.MustCompile(`[^A-Za-z0-9_@%+=:,./-]`)

// Quote quotes a word for the shell as Python's shlex.quote does: a word of safe characters
// stays, any other goes into single quotes, with each single quote written as '"'"'.
func Quote(word string) string {
	if word == "" {
		return "''"
	}
	if !unsafeCharacter.MatchString(word) {
		return word
	}
	return "'" + strings.ReplaceAll(word, "'", `'"'"'`) + "'"
}
