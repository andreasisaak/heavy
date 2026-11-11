package detect

import (
	"path/filepath"
	"strings"

	"github.com/andreasisaak/heavy/internal/catalogue"
	"github.com/andreasisaak/heavy/internal/shell"
)

// directoryOptions name the directory a program works in; the last one wins.
var directoryOptions = map[string]map[string]bool{
	"make":     {"-C": true, "--directory": true},
	"pnpm":     {"-C": true, "--dir": true},
	"npm":      {"--prefix": true},
	"uv":       {"--directory": true, "--project": true},
	"composer": {"-d": true, "--working-dir": true},
}

// callDirectory is the directory a command works in, from its directory options; `docker compose` also reads COMPOSE_FILE.
func callDirectory(words []string, cwd string, environment map[string]string) string {
	program := programName(words[0])
	if program == "docker" {
		return composeDirectory(words[1:], cwd, environment)
	}
	options := directoryOptions[program]
	directory := cwd
	for index := 1; index < len(words); index++ {
		word := words[index]
		name, value, assigned := strings.Cut(word, "=")
		switch {
		case options[word] && index+1 < len(words):
			directory = shell.Directory(words[index+1], cwd)
		case assigned && options[name]:
			directory = shell.Directory(value, cwd)
		case program == "make" && strings.HasPrefix(word, "-C") && len(word) > 2:
			directory = shell.Directory(word[2:], cwd)
		}
	}
	return directory
}

// composeDirectory is the compose project directory: options before the compose subcommand win over the
// environment, as in compose. --project-directory or COMPOSE_PROJECT_DIRECTORY, else the directory of the first
// compose file from -f or COMPOSE_FILE, else the cwd.
func composeDirectory(arguments []string, cwd string, environment map[string]string) string {
	rest := catalogue.SkipOptions(arguments, catalogue.DockerValues)
	var words []string
	if len(rest) > 0 && rest[0] == "compose" {
		words = rest[1:]
	}
	options := map[string]string{}
	for index := 0; index < len(words) && strings.HasPrefix(words[index], "-"); {
		option, value, assigned := strings.Cut(words[index], "=")
		takesValue := catalogue.ComposeValues[option] && !assigned
		if takesValue && index+1 < len(words) {
			value = words[index+1]
		}
		if option == "-f" {
			option = "--file"
		}
		if _, seen := options[option]; !seen {
			options[option] = value
		}
		if takesValue {
			index += 2
		} else {
			index++
		}
	}
	projectDirectory := options["--project-directory"]
	if projectDirectory == "" {
		projectDirectory = environment["COMPOSE_PROJECT_DIRECTORY"]
	}
	if projectDirectory != "" {
		return shell.Directory(projectDirectory, cwd)
	}
	composeFile := options["--file"]
	if files := filepath.SplitList(environment["COMPOSE_FILE"]); composeFile == "" && len(files) > 0 {
		composeFile = files[0]
	}
	if composeFile == "" {
		return cwd
	}
	return shell.Directory(filepath.Dir(composeFile), cwd)
}

// execContainer is the container of `docker exec <container> …`.
func execContainer(words []string) (string, bool) {
	if filepath.Base(words[0]) != "docker" {
		return "", false
	}
	rest := catalogue.SkipOptions(words[1:], catalogue.DockerValues)
	if len(rest) > 0 && rest[0] == "container" {
		rest = rest[1:]
	}
	if len(rest) == 0 || rest[0] != "exec" {
		return "", false
	}
	target := catalogue.SkipOptions(rest[1:], catalogue.ExecValues)
	if len(target) == 0 {
		return "", false
	}
	return target[0], true
}
