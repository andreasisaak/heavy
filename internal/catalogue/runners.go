// Package catalogue names the heavy runner a simple command starts: test runners, type and
// static checks of a whole project, and builds, through any launcher it knows
// (`docker compose exec web pnpm exec vitest run` is `vitest`), or a package script with a
// heavy name (`pnpm typecheck`). A call is light when it names one to three source files,
// when a linter gets a list of files, when it never ends (`--watch`, `test:watch`), when it
// only prints help or a version, and when the catalogue does not name its program.
package catalogue

import (
	"path/filepath"
	"strings"

	"github.com/andreasisaak/heavy/internal/shell"
)

// fileRunners take source files; requiredSubcommands and lightSubcommands refine them.
var fileRunners = set(
	"vitest", "jest", "playwright", "phpunit", "paratest", "pest", "pytest", "py.test", "mocha", "ava",
	"cypress", "test-storybook", "vue-tsc", "tsc", "phpstan", "psalm", "mypy", "pyright", "eslint", "stylelint",
)

// alwaysRunners load the machine whatever they name: infection mutates the whole suite (FR-5).
var alwaysRunners = set("infection")

var (
	requiredSubcommands = map[string]map[string]bool{
		"playwright": set("test"), "cypress": set("run"), "phpstan": set("analyse", "analyze"),
	}
	lightSubcommands = map[string]map[string]bool{"vitest": set("watch", "dev", "list", "init")}
)

// subcommandRunners start the heavy run with one of their subcommands.
var subcommandRunners = map[string]map[string]bool{
	"artisan":   set("test"),
	"bun":       set("test"),
	"deno":      set("test"),
	"cargo":     set("test", "nextest", "build", "clippy", "bench", "check"),
	"go":        set("test", "build", "vet"),
	"deptrac":   set("analyse", "analyze"),
	"vite":      set("build"),
	"nuxt":      set("build", "generate", "typecheck"),
	"nuxi":      set("build", "generate", "typecheck"),
	"next":      set("build"),
	"astro":     set("build"),
	"storybook": set("build"),
}

var (
	// targetedSubcommandRunners stay light for one to three files as well.
	targetedSubcommandRunners = set("artisan", "bun", "deno")
	// bareRunners are heavy without a subcommand too.
	bareRunners       = set("deptrac")
	watchOptions      = set("--watch", "--watchAll", "--watch-all", "--ui")
	shortWatchRunners = set("vitest", "tsc", "vue-tsc")
	helpOptions       = set("--help", "-h", "--version")
)

// Runner names the heavy runner the simple command argv starts; heavy is false for a light call.
func Runner(argv []string) (name string, heavy bool) {
	words := CommandWords(argv)
	if len(words) == 0 {
		return "", false
	}
	program, arguments := filepath.Base(words[0]), words[1:]
	if name, heavy := programRunner(program, arguments); heavy {
		return name, true
	}
	launched, _ := unwrap(program, arguments)
	for _, inner := range launched {
		if inner.script != nil {
			name, heavy = scriptRunner(*inner.script)
		} else {
			name, heavy = Runner(inner.command)
		}
		if heavy {
			return name, true
		}
	}
	return "", false
}

// CommandWords are the words from the program on, with environment assignments and wrappers such as nice dropped.
func CommandWords(argv []string) []string {
	return shell.Normalize(argv)
}

func programRunner(program string, arguments []string) (string, bool) {
	if alwaysRunners[program] {
		return program, !anyOf(arguments, helpOptions)
	}
	if !runs(program, arguments) {
		return "", false
	}
	subcommand, hasSubcommand := "", false
	for _, word := range arguments {
		if !strings.HasPrefix(word, "-") && !strings.HasPrefix(word, "+") {
			subcommand, hasSubcommand = word, true
			break
		}
	}
	if fileRunners[program] {
		if lightSubcommands[program][subcommand] {
			return "", false
		}
		if required, found := requiredSubcommands[program]; found && !required[subcommand] {
			return "", false
		}
		if linters[program] {
			return program, !LintsFilesOnly(arguments)
		}
		return program, !Targeted(arguments, valueOptions[program])
	}
	if subcommands, found := subcommandRunners[program]; found {
		if !hasSubcommand && bareRunners[program] {
			return program, true
		}
		if !subcommands[subcommand] {
			return "", false
		}
		if targetedSubcommandRunners[program] && Targeted(arguments, nil) {
			return "", false
		}
		if bareRunners[program] {
			return program, true
		}
		return program + " " + subcommand, true
	}
	return "", false
}

// runs is false for a call that never ends or only prints help or a version.
func runs(program string, arguments []string) bool {
	if anyOf(arguments, watchOptions) || anyOf(arguments, helpOptions) {
		return false
	}
	return !(shortWatchRunners[program] && anyOf(arguments, set("-w")))
}

func anyOf(words []string, options map[string]bool) bool {
	for _, word := range words {
		if options[word] {
			return true
		}
	}
	return false
}

func set(words ...string) map[string]bool {
	members := make(map[string]bool, len(words))
	for _, word := range words {
		members[word] = true
	}
	return members
}
