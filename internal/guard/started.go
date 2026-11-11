package guard

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/andreasisaak/heavy/internal/shell"
)

// startedRun is a simple command that starts heavy itself, and the command heavy runs.
type startedRun struct {
	index int
	words []string
}

// heavyRuns are the simple commands of the call that start a heavy run: `heavy [--directory D] [--agent A] <cmd…>`,
// not `heavy --status`, `heavy --help` and the like.
func heavyRuns(script shell.Script) []startedRun {
	var runs []startedRun
	for index, command := range script.Commands {
		if words := heavyRun(command.Argv); words != nil {
			runs = append(runs, startedRun{index: index, words: words})
		}
	}
	return runs
}

func heavyRun(argv []string) []string {
	if len(argv) == 0 || !isHeavy(argv[0]) {
		return nil
	}
	rest := argv[1:]
	for len(rest) > 0 && valueOptions[rest[0]] {
		rest = rest[min(2, len(rest)):]
	}
	if len(rest) == 0 || strings.HasPrefix(rest[0], "--") || noRunWords[rest[0]] {
		return nil
	}
	return rest
}

func isHeavy(program string) bool {
	return filepath.Base(program) == "heavy"
}

func runTexts(runs []startedRun) []string {
	joined := make([]string, 0, len(runs))
	for _, run := range runs {
		joined = append(joined, strings.Join(run.words, " "))
	}
	return joined
}

// startedHeavy sends a call that starts heavy itself to the background with the context, so the session waits to
// be woken instead of polling the queue; a session started with --agent gets its agent named to each heavy.
func startedHeavy(current call, runs []startedRun, tool toolInput, input hookInput, deps Dependencies) (*answer, error) {
	if len(runs) == 0 {
		return nil, nil
	}
	targets := runTexts(runs)
	if tool.background {
		if cause := conflictingForm(current, input.Cwd); cause != "" {
			return conflictDenial(targets, "heavy "+targets[0], cause), nil
		}
	}
	named := current.source
	if input.AgentType != nil {
		named = nameAgent(current, runs, *input.AgentType)
	}
	return rewrite(tool, named, howDirect, targets, input.SessionID, deps)
}

// nameAgent puts `--agent <agent>` right behind each placeable heavy that names no agent yet; a heavy behind a
// wrapper such as nice counts too. A heavy run the guard cannot place (in a subshell, a substitution, a call of
// several lines) keeps its words, as in 0.6.
func nameAgent(current call, runs []startedRun, agent string) string {
	var ends []int
	for _, run := range runs {
		command := current.script.Commands[run.index]
		if !placeable(command) || slices.Contains(command.Raw, "--agent") {
			continue
		}
		expression := callAt(current.file, command.Offset)
		program := expression.Args[len(expression.Args)-len(command.Argv)]
		ends = append(ends, int(program.End().Offset()))
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ends)))
	named := current.source
	for _, end := range ends {
		named = named[:end] + " --agent " + shell.Quote(agent) + named[end:]
	}
	return named
}
