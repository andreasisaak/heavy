// Package label names a run in a word or two, for the queue and the herdr sidebar: a heavy command by what
// matched (the list entry or the runner of the catalogue, `vitest`, `artisan test`), a make entry by its target
// alone (`ci`). Any other command, such as one started with heavy by hand, is named by its make target or by the
// file name of its program (`checkall.sh`).
package label

import (
	"path/filepath"
	"strings"

	"github.com/andreasisaak/heavy/internal/catalogue"
	"github.com/andreasisaak/heavy/internal/detect"
	"github.com/andreasisaak/heavy/internal/shell"
)

// makeValues are make options whose value is the next word, not a target.
var makeValues = map[string]bool{
	"-C": true, "-f": true, "-I": true, "-o": true, "-W": true, "-j": true, "-l": true,
	"--directory": true, "--file": true, "--makefile": true, "--include-dir": true,
}

// Label returns the short name of the run of argv started in cwd; a broken list is an error.
func Label(argv []string, cwd string, detector *detect.Detector) (string, error) {
	found, err := detector.Heavy(shell.FromArgv(argv), cwd)
	if err != nil {
		return "", err
	}
	matched := ""
	if len(found) > 0 && found[0].Index == 0 {
		matched = found[0].Text
	}
	words := catalogue.CommandWords(argv)
	if matched != "" {
		words = strings.Fields(matched)
	}
	if len(words) == 0 {
		return filepath.Base(argv[0]), nil
	}
	program := filepath.Base(words[0])
	if program == "make" || program == "gmake" {
		if target, found := makeTarget(words[1:]); found {
			return target, nil
		}
		return program, nil
	}
	if matched != "" {
		return matched, nil
	}
	return program, nil
}

// makeTarget is the first target of a make call: no option, option value, variable assignment or job count.
func makeTarget(arguments []string) (string, bool) {
	for index := 0; index < len(arguments); index++ {
		word := arguments[index]
		switch {
		case makeValues[word]:
			index++
		case strings.HasPrefix(word, "-") || shell.IsAssignment(word) || isNumber(word):
		default:
			return word, true
		}
	}
	return "", false
}

func isNumber(word string) bool {
	if word == "" {
		return false
	}
	for _, character := range word {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
