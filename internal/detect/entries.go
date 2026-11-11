package detect

import (
	"path/filepath"
	"strings"

	"github.com/andreasisaak/heavy/internal/shell"
)

// programAliases are programs that list entries name by another file name.
var programAliases = map[string]string{"gmake": "make"}

func programName(word string) string {
	name := filepath.Base(word)
	if alias, found := programAliases[name]; found {
		return alias
	}
	return name
}

// readsOnly reports a shell that only checks the syntax of its script (`bash -n run.sh`, `-o noexec`) and runs none of it.
func readsOnly(words []string) bool {
	if !shell.IsShell(words[0]) {
		return false
	}
	for index := 1; index < len(words); index++ {
		word := words[index]
		if !strings.HasPrefix(word, "-") || strings.HasPrefix(word, "--") {
			return false
		}
		if strings.Contains(word[1:], "n") || (word == "-o" && index+1 < len(words) && words[index+1] == "noexec") {
			return true
		}
	}
	return false
}

// programOf returns the file name of the program and its arguments; a script a shell runs (`bash -x run.sh`) is the program.
func programOf(words []string) (string, []string) {
	program := programName(words[0])
	if shell.IsShell(words[0]) {
		for index := 1; index < len(words); index++ {
			word := words[index]
			if !strings.HasPrefix(word, "-") {
				return filepath.Base(word), words[index+1:]
			}
			if !strings.HasPrefix(word, "--") && strings.Contains(word, "c") {
				break
			}
		}
	}
	return program, words[1:]
}

// matches reports whether the program matches and the rest of the entry appears in the arguments, in order.
func matches(entry []string, program string, arguments []string) bool {
	if entry[0] != program {
		return false
	}
	position := 0
	for _, word := range entry[1:] {
		for position < len(arguments) && arguments[position] != word {
			position++
		}
		if position == len(arguments) {
			return false
		}
		position++
	}
	return true
}

// excepted reports whether one of the exceptions is among the words and variables the call carries: one ending in
// "=" needs that variable with a value that is not empty, any other the exact word.
func excepted(unless, carried []string) bool {
	for _, exception := range unless {
		for _, word := range carried {
			if strings.HasSuffix(exception, "=") {
				if strings.HasPrefix(word, exception) && len(word) > len(exception) {
					return true
				}
			} else if word == exception {
				return true
			}
		}
	}
	return false
}
