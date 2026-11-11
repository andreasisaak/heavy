package catalogue

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/andreasisaak/heavy/internal/shell"
)

// target is what a launcher starts: a command, or a package script by name when script is set.
type target struct {
	command []string
	script  *packageScript
}

var (
	uvValues    = set("--directory", "--project", "--config-file", "--cache-dir", "--color")
	uvRunValues = set(
		"--with", "--with-editable", "--with-requirements", "-p", "--python", "--project", "--directory",
		"--group", "--only-group", "--no-group", "--extra", "--package", "--env-file", "--index", "--index-url",
		"--extra-index-url", "--exclude-newer",
	)
	uvxValues        = set("--from", "--with", "--with-editable", "--with-requirements", "-p", "--python", "--index")
	pythonValues     = set("-X", "-W")
	composerValues   = set("-d", "--working-dir")
	composerCommands = set(
		"install", "i", "update", "u", "upgrade", "require", "r", "remove", "rm", "dump-autoload", "dumpautoload",
		"show", "info", "outdated", "validate", "diagnose", "config", "create-project", "global", "init", "search",
		"why", "depends", "why-not", "prohibits", "licenses", "audit", "bump", "check-platform-reqs",
		"clear-cache", "clearcache", "self-update", "selfupdate", "status", "suggests", "fund", "archive",
		"browse", "home", "list", "help", "reinstall",
	)
	python          = regexp.MustCompile(`^python([0-9]+(\.[0-9]+)?)?$`)
	scriptExtension = regexp.MustCompile(`\.(php|[cm]?js)$`)
)

var launchers = map[string]func([]string) []target{
	"docker":   docker,
	"pnpm":     pnpm,
	"npm":      npm,
	"npx":      packageRunner,
	"pnpx":     packageRunner,
	"bunx":     packageRunner,
	"yarn":     yarn,
	"bun":      bun,
	"uv":       uv,
	"uvx":      uvx,
	"poetry":   runSubcommand,
	"pipenv":   runSubcommand,
	"php":      interpreted(set("-d", "-c", "-z"), set("-r", "-l")),
	"node":     interpreted(set("-r", "--require", "--import", "--loader", "--experimental-loader"), set("-e", "--eval", "-p", "--print")),
	"composer": composer,
	"ddev":     ddev,
}

// unwrap returns what a launcher starts; launcher is false when program is no launcher.
func unwrap(program string, arguments []string) (launched []target, launcher bool) {
	if shell.IsShell(program) {
		return shellString(arguments), true
	}
	if python.MatchString(program) {
		return pythonModule(arguments), true
	}
	unwrapper, found := launchers[program]
	if !found {
		return nil, false
	}
	return unwrapper(arguments), true
}

// SkipOptions returns the words after the leading options; an option in values takes the next word, `--` ends the options.
func SkipOptions(words []string, values map[string]bool) []string {
	index := 0
	for index < len(words) && strings.HasPrefix(words[index], "-") {
		if words[index] == "--" {
			return words[index+1:]
		}
		if values[words[index]] {
			index += 2
		} else {
			index++
		}
	}
	if index >= len(words) {
		return nil
	}
	return words[index:]
}

// shellString is every command of the string of `sh -c`. A string that is no valid Bash runs no command.
func shellString(arguments []string) []target {
	for index := 0; index < len(arguments)-1; index++ {
		word := arguments[index]
		if strings.HasPrefix(word, "-") && !strings.HasPrefix(word, "--") && strings.Contains(word, "c") {
			script, err := shell.Parse(arguments[index+1])
			if err != nil {
				return nil
			}
			launched := make([]target, 0, len(script.Commands))
			for _, command := range script.Commands {
				launched = append(launched, target{command: command.Argv})
			}
			return launched
		}
	}
	return nil
}

func uv(arguments []string) []target {
	rest := SkipOptions(arguments, uvValues)
	if len(rest) > 0 && rest[0] == "run" {
		return []target{{command: SkipOptions(rest[1:], uvRunValues)}}
	}
	if len(rest) > 1 && rest[0] == "tool" && rest[1] == "run" {
		return uvx(rest[2:])
	}
	return nil
}

func uvx(arguments []string) []target {
	return []target{{command: SkipOptions(arguments, uvxValues)}}
}

// runSubcommand is `poetry run <command>` and `pipenv run <command>`.
func runSubcommand(arguments []string) []target {
	if len(arguments) > 0 && arguments[0] == "run" {
		return []target{{command: arguments[1:]}}
	}
	return nil
}

// pythonModule is the module of `python -m <module>`; `python -c` runs no runner.
func pythonModule(arguments []string) []target {
	index := 0
	for index < len(arguments) && strings.HasPrefix(arguments[index], "-") {
		switch {
		case arguments[index] == "-m":
			return []target{{command: arguments[index+1:]}}
		case arguments[index] == "-c":
			return nil
		case pythonValues[arguments[index]]:
			index += 2
		default:
			index++
		}
	}
	return nil
}

// interpreted is php and node: the script they run counts as the program, `vendor/bin/phpunit` as `phpunit`.
func interpreted(values, stop map[string]bool) func([]string) []target {
	return func(arguments []string) []target {
		if anyOf(arguments, stop) {
			return nil
		}
		rest := SkipOptions(arguments, values)
		if len(rest) == 0 {
			return nil
		}
		program := scriptExtension.ReplaceAllString(filepath.Base(rest[0]), "")
		return []target{{command: append([]string{program}, rest[1:]...)}}
	}
}

func composer(arguments []string) []target {
	rest := SkipOptions(arguments, composerValues)
	if len(rest) == 0 {
		return nil
	}
	command, rest := rest[0], rest[1:]
	switch {
	case command == "run-script" || command == "run":
		return scriptTarget("composer", SkipOptions(rest, composerValues))
	case command == "exec":
		return []target{{command: rest}}
	case composerCommands[command]:
		return nil
	}
	return []target{{script: &packageScript{launcher: "composer", name: command, arguments: rest}}}
}

// ddev passes the rest to the tool of that name: `ddev composer test`, `ddev php artisan test`.
func ddev(arguments []string) []target {
	return []target{{command: arguments}}
}

func scriptTarget(launcher string, rest []string) []target {
	if len(rest) == 0 {
		return nil
	}
	return []target{{script: &packageScript{launcher: launcher, name: rest[0], arguments: rest[1:]}}}
}
