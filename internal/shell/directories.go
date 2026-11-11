package shell

import (
	"os"
	"path/filepath"
	"strings"
)

// Directories returns the shell's directory at each command, starting in cwd: a `cd <dir>`
// moves it for every command after it, inside a subshell or shell string too, as in 0.6.
func (s Script) Directories(cwd string) []string {
	directories := make([]string, 0, len(s.Commands))
	directory := cwd
	for _, command := range s.Commands {
		directories = append(directories, directory)
		if len(command.Argv) > 1 && filepath.Base(command.Argv[0]) == "cd" {
			directory = Directory(command.Argv[1], directory)
		}
	}
	return directories
}

// Directory is the directory a path names from cwd. A path the shell still has to expand
// ($D, ${ROOT}/x, backticks, ~user) keeps cwd: an unknown target in a project with heavy
// on still counts as that project, which errs towards the queue.
func Directory(path, cwd string) string {
	if strings.ContainsAny(path, "$`") {
		return cwd
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		path = os.Getenv("HOME") + path[1:]
	} else if strings.HasPrefix(path, "~") {
		return cwd
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(cwd, path)
}
