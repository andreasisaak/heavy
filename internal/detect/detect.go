// Package detect decides which simple commands of a call are heavy, for the guard, the shims and the label of
// a run.
//
// A simple command is heavy when an entry of its project's lists matches it (internal/lists), or when it starts
// a runner of the catalogue (internal/catalogue) in a project whose lists turn the catalogue on. An entry
// matches when the program has the entry's file name and the rest of the entry appears in the arguments in
// order; a script a shell starts counts as that script, and a list entry inside `bash -c '…'` counts too. An
// entry with exceptions ("unless") does not match a call that carries one of them: a word of the call, before
// the program or among its arguments, or a variable of the environment a shim sees.
//
// The project of a command is the directory it works in: the cwd, a `cd <dir>` earlier in the call, `make -C`,
// `pnpm -C`, `uv --directory`, `composer -d`, the compose file or project directory of `docker compose`, and for
// `docker exec <container>` the compose project the container belongs to.
package detect

import (
	"path/filepath"
	"strings"

	"github.com/andreasisaak/heavy/internal/catalogue"
	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/shell"
)

// Match is a heavy simple command.
type Match struct {
	// Index is the position of the command in the script.
	Index int
	// Text is what matched: the words of the list entry or the name of the runner.
	Text string
	// Directory is the project directory of the command.
	Directory string
	// ShellDirectory is the shell's directory at the command.
	ShellDirectory string
	// Entry is the list entry that matched, nil for a runner of the catalogue.
	Entry *lists.Entry
}

// ContainerDirectory returns the compose working directory of a container; found is false for any other.
type ContainerDirectory func(name string) (directory string, found bool)

// Detector finds the heavy commands of scripts; it keeps the projects it has read.
type Detector struct {
	personal    lists.Personal
	container   ContainerDirectory
	environment map[string]string
	projects    map[string]lists.Project
	warnings    []string
}

// NewDetector returns a detector over the personal list; environment holds the variables a shim sees, nil for
// a call the guard reads before the shell runs it.
func NewDetector(personal lists.Personal, container ContainerDirectory, environment map[string]string) *Detector {
	return &Detector{personal: personal, container: container, environment: environment, projects: map[string]lists.Project{}}
}

// Heavy returns every heavy simple command of the script run in cwd; a broken list is an error.
func (d *Detector) Heavy(script shell.Script, cwd string) ([]Match, error) {
	directories := script.Directories(cwd)
	var found []Match
	for index := range script.Commands {
		match, heavy, err := d.classify(script, directories, index)
		if err != nil {
			return nil, err
		}
		if heavy {
			found = append(found, match)
		}
	}
	return found, nil
}

// classify decides one command; a shell that runs a string is heavy by the first heavy command of that string.
func (d *Detector) classify(script shell.Script, directories []string, index int) (Match, bool, error) {
	command := script.Commands[index]
	words := command.Argv
	directory := directories[index]
	if len(words) == 0 || filepath.Base(words[0]) == "cd" || readsOnly(words) {
		return Match{}, false, nil
	}
	if shell.IsShell(words[0]) {
		for inner := index + 1; inner < len(script.Commands) && descends(script, inner, index); inner++ {
			match, heavy, err := d.classify(script, directories, inner)
			if err != nil || heavy {
				return Match{Index: index, Text: match.Text, Directory: match.Directory, ShellDirectory: directory, Entry: match.Entry}, heavy, err
			}
		}
	}
	projectDirectory := callDirectory(words, directory, d.environment)
	project, err := d.project(projectDirectory)
	if err != nil {
		return Match{}, false, err
	}
	program, arguments := programOf(words)
	carried := append([]string{}, command.Raw...)
	for name, value := range d.environment {
		carried = append(carried, name+"="+value)
	}
	for entryIndex := range project.Commands {
		entry := &project.Commands[entryIndex]
		if matches(entry.Words, program, arguments) && !excepted(entry.Unless, carried) {
			return Match{Index: index, Text: strings.Join(entry.Words, " "), Directory: projectDirectory, ShellDirectory: directory, Entry: entry}, true, nil
		}
	}
	runner, heavy := catalogue.Runner(words)
	if !heavy {
		return Match{}, false, nil
	}
	if project.Catalogue {
		return Match{Index: index, Text: runner, Directory: projectDirectory, ShellDirectory: directory}, true, nil
	}
	return d.containerMatch(words, runner, index, directory)
}

// containerMatch counts a runner of `docker exec <container>` in the compose project of the container, where only
// the catalogue of that project counts.
func (d *Detector) containerMatch(words []string, runner string, index int, directory string) (Match, bool, error) {
	container, found := execContainer(words)
	if !found || d.container == nil {
		return Match{}, false, nil
	}
	containerProject, found := d.container(container)
	if !found {
		return Match{}, false, nil
	}
	project, err := d.project(containerProject)
	if err != nil || !project.Catalogue {
		return Match{}, false, err
	}
	return Match{Index: index, Text: runner, Directory: containerProject, ShellDirectory: directory}, true, nil
}

// Warnings are the notes on what heavy left out of the .heavy.json files the detector has read so far, in the
// order it read them.
func (d *Detector) Warnings() []string {
	return d.warnings
}

func (d *Detector) project(directory string) (lists.Project, error) {
	if project, found := d.projects[directory]; found {
		return project, nil
	}
	project, err := lists.ProjectOf(directory, d.personal)
	if err != nil {
		return lists.Project{}, err
	}
	d.projects[directory] = project
	d.warnings = append(d.warnings, project.Warnings...)
	return project, nil
}

// descends reports whether the command at index comes from the shell string of the command at ancestor.
func descends(script shell.Script, index, ancestor int) bool {
	for parent := script.Commands[index].Parent; parent >= 0; parent = script.Commands[parent].Parent {
		if parent == ancestor {
			return true
		}
	}
	return false
}
