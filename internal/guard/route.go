package guard

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/andreasisaak/heavy/internal/detect"
	"github.com/andreasisaak/heavy/internal/shell"
)

// route is the rewrite or denial of a call with a heavy command or a heavy run of its own.
func route(current call, found []detect.Match, runs []startedRun, tool toolInput, input hookInput, deps Dependencies) (*answer, error) {
	if len(found) == 0 {
		return startedHeavy(current, runs, tool, input, deps)
	}
	placed, hidden := placement(current.script, found)
	all := append(slices.Clone(placed), hidden...)
	if tool.background {
		if cause := conflictingForm(current, input.Cwd); cause != "" {
			return conflictDenial(texts(all), all[0].Text, cause), nil
		}
	}
	// Several heavy commands run in one hold of the queue, through heavy as a whole, unless other work stands
	// before them: that work would wait in the queue as well. Then each heavy command queues on its own, and only
	// a heavy command heavy cannot go in front of leaves no way but to deny the call.
	others := otherWork(current.script, found)
	switch {
	case len(hidden) > 0 && len(others) > 0:
		return denial(fmt.Sprintf(mixedReason, targetList(others), targetList(texts(all)), targetList(texts(all)))), nil
	case len(hidden) > 0 || (len(distinct(texts(all))) > 1 && len(others) == 0):
		return rewrite(tool, whole(current.source, all[0], input.Cwd, deps.Heavy, input.AgentType), howWhole, texts(all), input.SessionID, deps)
	}
	return rewrite(tool, inFront(current, placed, deps.Heavy, input.AgentType), howInFront, texts(placed), input.SessionID, deps)
}

// placement splits the heavy commands into those heavy can go right in front of and those it cannot place; a
// command of the shell string of a placed one runs behind that heavy anyway.
func placement(script shell.Script, found []detect.Match) (placed, hidden []detect.Match) {
	placedIndex := map[int]bool{}
	for _, match := range found {
		if placeable(script.Commands[match.Index]) {
			placed = append(placed, match)
			placedIndex[match.Index] = true
		}
	}
	for _, match := range found {
		if !placedIndex[match.Index] && !descendsFrom(script, match.Index, placedIndex) {
			hidden = append(hidden, match)
		}
	}
	return placed, hidden
}

// inFront puts `heavy[ --directory D][ --agent A] ` in front of each placed command, after its assignments and
// before its wrappers; D is the project when it differs from the shell's directory there.
func inFront(current call, placed []detect.Match, heavy string, agent *string) string {
	ordered := slices.Clone(placed)
	sort.Slice(ordered, func(left, right int) bool {
		return current.script.Commands[ordered[left].Index].Offset > current.script.Commands[ordered[right].Index].Offset
	})
	rewritten := current.source
	for _, match := range ordered {
		offset := current.script.Commands[match.Index].Offset
		option := ""
		if match.Directory != match.ShellDirectory {
			option = " --directory " + shell.Quote(match.Directory)
		}
		rewritten = rewritten[:offset] + shell.Quote(heavy) + option + agentOption(agent) + " " + rewritten[offset:]
	}
	return rewritten
}

// whole runs the whole call through heavy in a shell of the user's kind, recorded under the project of its first
// heavy command.
func whole(command string, first detect.Match, cwd, heavy string, agent *string) string {
	option := ""
	if first.Directory != cwd {
		option = " --directory " + shell.Quote(first.Directory)
	}
	return shell.Quote(heavy) + option + agentOption(agent) + " " + wholeShell + " " + shell.Quote(command)
}

func agentOption(agent *string) string {
	if agent == nil {
		return ""
	}
	return " --agent " + shell.Quote(*agent)
}

// otherWork are the short names of the commands before the first heavy one that do more than glue a call together.
// Work after a heavy command waits for it anyway; work before it, such as a file edit, would wait for the lock only
// because the call goes through heavy as a whole.
func otherWork(script shell.Script, found []detect.Match) []string {
	var names []string
	for _, command := range script.Commands[:found[0].Index] {
		words := command.Argv
		// A program word that is a substitution names no program; its commands count on their own.
		if glue[filepath.Base(words[0])] || strings.Contains(words[0], "`") || strings.Contains(words[0], "$(") {
			continue
		}
		name := filepath.Base(words[0])
		if len(words) > 1 && !strings.HasPrefix(words[1], "-") {
			name += " " + words[1]
		}
		names = append(names, name)
	}
	return distinct(names)
}

func texts(found []detect.Match) []string {
	matched := make([]string, 0, len(found))
	for _, match := range found {
		matched = append(matched, match.Text)
	}
	return matched
}

func distinct(words []string) []string {
	var kept []string
	for _, word := range words {
		if !slices.Contains(kept, word) {
			kept = append(kept, word)
		}
	}
	return kept
}

func conflictDenial(targets []string, example, cause string) *answer {
	return denial(fmt.Sprintf(conflictReason, targetList(targets), cause, example, strings.Fields(example)[0]))
}
