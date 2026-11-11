package catalogue

import (
	"regexp"
	"strings"
)

// maxTargetedFiles is the most source files a run may name and stay light.
const maxTargetedFiles = 3

// linters check what they are given: files of any number stay light; a directory, a glob or no target is a tree.
var linters = set("eslint", "stylelint")

var linterValueOptions = set(
	"-c", "--config", "-f", "--format", "--formatter", "-o", "--output-file", "--ext", "--rule", "--parser",
	"--plugin", "--rulesdir", "--ignore-path", "-i", "--ignore-pattern", "--max-warnings", "--cache-location",
	"--cache-strategy", "--env", "--global", "--parser-options", "--resolve-plugins-relative-to", "--custom-syntax",
	"--config-basedir",
)

// valueOptions are the options that take a value, for runners whose flags (`phpstan --no-progress`) often stand
// right before a file. A runner listed here has only these; any other long option is a flag. Taken from
// `phpstan analyse --help` 2.3.
var valueOptions = map[string]map[string]bool{
	"phpstan": set(
		"-c", "--configuration", "-l", "--level", "-a", "--autoload-file", "--error-format", "-b",
		"--generate-baseline", "--memory-limit", "--tmp-file", "--instead-of",
	),
}

var (
	glob       = regexp.MustCompile(`[*?\[{]`)
	sourceFile = regexp.MustCompile(`[^/\s]\.(php|py|[cm]?[jt]sx?|vue|svelte|rs|go|rb|css|scss)(::\S*)?$`)
	configFile = regexp.MustCompile(`(^|/)([^/]*\.config\.[^/]+|tsconfig[^/]*|vitest\.workspace\.[^/]+)$`)
)

// LintsFilesOnly reports a linter call that names source files and nothing to search: no directory, no glob, not
// no target at all.
func LintsFilesOnly(arguments []string) bool {
	values := map[int]bool{}
	for index, word := range arguments {
		if linterValueOptions[word] {
			values[index+1] = true
		}
	}
	targets := 0
	for index, word := range arguments {
		if values[index] || strings.HasPrefix(word, "-") {
			continue
		}
		if !sourceFile.MatchString(word) || glob.MatchString(word) {
			return false
		}
		targets++
	}
	return targets > 0
}

// Targeted reports a call that names one to three source files and no directory, so it checks only those files.
//
// A word right after an option of valueOptions is that option's value (`phpstan -a tools/autoload.php`), not a
// target. Without such a list (nil), a word right after any long option without `=` counts as its value
// (`--setupFiles tests/setup.ts`): a flag such as `--coverage` in front of a file then makes the run count as
// heavy, which errs towards the queue.
func Targeted(arguments []string, valueOptions map[string]bool) bool {
	values := map[int]bool{}
	for index, word := range arguments {
		if valueOptions == nil {
			if strings.HasPrefix(word, "--") && len(word) > 2 && !strings.Contains(word, "=") {
				values[index+1] = true
			}
		} else if valueOptions[word] {
			values[index+1] = true
		}
	}
	files, paths := 0, 0
	for index, word := range arguments {
		if values[index] || strings.HasPrefix(word, "-") || configFile.MatchString(word) {
			continue
		}
		if sourceFile.MatchString(word) {
			files++
		} else if strings.Contains(word, "/") || word == "." {
			paths++
		}
	}
	return files >= 1 && files <= maxTargetedFiles && paths == 0
}
