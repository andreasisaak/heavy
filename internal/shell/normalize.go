package shell

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// wrappers start the command after them; the set lists their options that take the next word.
var wrappers = map[string]map[string]bool{
	"command":    nil,
	"builtin":    nil,
	"exec":       nil,
	"env":        {"-u": true},
	"sudo":       {"-u": true, "-g": true},
	"nohup":      nil,
	"time":       nil,
	"nice":       {"-n": true},
	"xargs":      {"-n": true, "-I": true, "-L": true, "-P": true, "-d": true, "-s": true, "-E": true},
	"timeout":    nil,
	"caffeinate": nil,
}

// wrapperPrefixes are words that start the command after them as a pair.
var wrapperPrefixes = [][2]string{{"ddev", "exec"}}

var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "fish": true}

var (
	environmentAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	duration              = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?[smhd]?$`)
)

// IsShell reports whether program names a shell that runs the string of -c.
func IsShell(program string) bool {
	return shells[filepath.Base(program)]
}

// IsAssignment reports whether word assigns an environment variable, such as FULL=1.
func IsAssignment(word string) bool {
	return environmentAssignment.MatchString(word)
}

// Normalize drops environment assignments and wrapper commands, so the first word is the
// real program: `timeout -s KILL 15m nice -n 5 env A=1 make ci` is `make ci`.
func Normalize(words []string) []string {
	index := 0
	for index < len(words) {
		word := words[index]
		if IsAssignment(word) {
			index++
			continue
		}
		if values, isWrapper := wrappers[word]; isWrapper {
			index++
			for index < len(words) && strings.HasPrefix(words[index], "-") {
				if values[words[index]] {
					index += 2
				} else {
					index++
				}
			}
			if word == "timeout" {
				index = afterDuration(words, index)
			}
			continue
		}
		if length := wrapperPrefix(words[index:]); length > 0 {
			index += length
			continue
		}
		break
	}
	if index >= len(words) {
		return nil
	}
	remainder := append([]string{}, words[index:]...)
	remainder[0] = strings.TrimLeft(remainder[0], `\`)
	return remainder
}

// wrapperPrefix is the number of words of the wrapper prefix words start with, 0 for none.
func wrapperPrefix(words []string) int {
	for _, prefix := range wrapperPrefixes {
		if len(words) >= len(prefix) && words[0] == prefix[0] && words[1] == prefix[1] {
			return len(prefix)
		}
	}
	return 0
}

// afterDuration is the index after the duration of `timeout [-s KILL] 900 cmd`: option
// values come first, the duration ends them.
func afterDuration(words []string, index int) int {
	for index < len(words) {
		index++
		if duration.MatchString(words[index-1]) {
			return index
		}
	}
	return index
}

// shellStrings are the arguments a program parses again as commands: the string of
// `sh -c` (none under -n or -o noexec, which only read it), the arguments of eval joined,
// and the arguments of ssh that hold whitespace.
func shellStrings(argv []string) []string {
	program := filepath.Base(argv[0])
	switch {
	case shells[program]:
		for index := 1; index < len(argv)-1; index++ {
			word := argv[index]
			if word == "-o" && argv[index+1] == "noexec" {
				return nil
			}
			if !strings.HasPrefix(word, "-") || strings.HasPrefix(word, "--") {
				continue
			}
			if strings.Contains(word[1:], "n") {
				return nil
			}
			if strings.Contains(word, "c") {
				return []string{argv[index+1]}
			}
		}
		return nil
	case program == "eval":
		return []string{strings.Join(argv[1:], " ")}
	case program == "ssh":
		var found []string
		for _, word := range argv[1:] {
			if strings.ContainsFunc(word, unicode.IsSpace) {
				found = append(found, word)
			}
		}
		return found
	}
	return nil
}
