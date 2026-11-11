package guard

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/andreasisaak/heavy/internal/shell"
	"mvdan.cc/sh/v3/syntax"
)

// Filters hide the live output of a heavy run when they end its pipeline: tail holds it back until the run ends,
// grep shows only part of it, head ends the run early once it has its lines. Port of hooks/output_filters.py, on
// the syntax tree instead of a scan of the text.
var (
	filters = map[string]bool{"tail": true, "head": true, "grep": true, "egrep": true, "fgrep": true}
	greps   = map[string]bool{"grep": true, "egrep": true, "fgrep": true}
	// grepDeciding are grep options whose result decides or counts instead of showing lines: -q, -c, -l, -L, alone
	// or in a cluster.
	grepDeciding  = regexp.MustCompile(`^(-[A-Za-z]*[qclL][A-Za-z0-9]*|--(quiet|silent|count|files-with-matches|files-without-match))$`)
	tailFollowing = regexp.MustCompile(`^(-[A-Za-z0-9]*[fF][A-Za-z0-9]*|--follow.*)$`)
)

// span is the text of one stage of a pipeline in the call.
type span struct {
	start, end int
}

// stripFilters removes the filters at the end of each pipeline with a stage that runs heavy, at any depth of
// ( … ) and { … }, and returns the call without them and the removed parts in order. It keeps a filter whose
// result the call uses: a grep -q or -c that decides, a tail -f, a filter whose output goes to a file or into a
// further stage. Text inside quotes, substitutions and shell strings stays as it is, and so does a call with a
// heredoc or a case, which 0.6 did not follow. runsHeavy says whether the text from start to end runs heavy.
func stripFilters(current call, runsHeavy func(start, end int) bool) (string, []string) {
	if unfollowed(current.file) {
		return current.source, nil
	}
	var cuts []span
	for _, stages := range pipelines(current.file) {
		keep := len(stages)
		for keep > 1 && hidesOutput(current, stages[keep-1]) {
			keep--
		}
		if keep == len(stages) {
			continue
		}
		for _, stage := range stages[:keep] {
			if runsHeavy(int(stage.Pos().Offset()), int(stage.End().Offset())) {
				cuts = append(cuts, span{int(stages[keep-1].End().Offset()), int(stages[len(stages)-1].End().Offset())})
				break
			}
		}
	}
	if len(cuts) == 0 {
		return current.source, nil
	}
	sort.Slice(cuts, func(left, right int) bool { return cuts[left].start < cuts[right].start })
	removed := make([]string, 0, len(cuts))
	stripped := current.source
	for index := len(cuts) - 1; index >= 0; index-- {
		cut := cuts[index]
		removed = append([]string{strings.TrimSpace(current.source[cut.start:cut.end])}, removed...)
		stripped = stripped[:cut.start] + stripped[cut.end:]
	}
	return stripped, removed
}

// unfollowed reports a heredoc or a case in the call, outside substitutions.
func unfollowed(file *syntax.File) bool {
	found := false
	walkOutsideSubstitutions(file, func(node syntax.Node) bool {
		switch inner := node.(type) {
		case *syntax.CaseClause:
			found = true
		case *syntax.Redirect:
			found = found || inner.Op == syntax.Hdoc || inner.Op == syntax.DashHdoc
		}
		return !found
	})
	return found
}

// pipelines are the stages of every pipeline of two or more stages, outside substitutions; a group such as
// `(make a && make b) 2>&1` is one stage of the pipeline around it, and its inner pipelines count on their own.
func pipelines(file *syntax.File) [][]*syntax.Stmt {
	var found [][]*syntax.Stmt
	inner := map[*syntax.BinaryCmd]bool{}
	walkOutsideSubstitutions(file, func(node syntax.Node) bool {
		if binary, isPipe := pipe(node); isPipe && !inner[binary] {
			found = append(found, append(stagesOf(binary.X, inner), stagesOf(binary.Y, inner)...))
		}
		return true
	})
	return found
}

func pipe(node syntax.Node) (*syntax.BinaryCmd, bool) {
	binary, isBinary := node.(*syntax.BinaryCmd)
	return binary, isBinary && (binary.Op == syntax.Pipe || binary.Op == syntax.PipeAll)
}

// stagesOf flattens one side of a pipe and marks the pipes inside it as part of the same pipeline.
func stagesOf(statement *syntax.Stmt, inner map[*syntax.BinaryCmd]bool) []*syntax.Stmt {
	if binary, isPipe := pipe(statement.Cmd); isPipe && !statement.Negated && len(statement.Redirs) == 0 {
		inner[binary] = true
		return append(stagesOf(binary.X, inner), stagesOf(binary.Y, inner)...)
	}
	return []*syntax.Stmt{statement}
}

// hidesOutput reports a tail, head or grep stage that only cuts what the user sees of the output before it.
func hidesOutput(current call, stage *syntax.Stmt) bool {
	expression, isCall := stage.Cmd.(*syntax.CallExpr)
	if !isCall || stage.Negated || len(expression.Args) == 0 {
		return false
	}
	for _, redirect := range stage.Redirs {
		if strings.Contains(redirect.Op.String(), ">") {
			return false
		}
	}
	command, found := commandAt(current.script, int(expression.Args[0].Pos().Offset()))
	if !found {
		return false
	}
	words := command.Raw
	for len(words) > 0 && shell.IsAssignment(words[0]) {
		words = words[1:]
	}
	if len(words) == 0 {
		return false
	}
	program := filepath.Base(words[0])
	if !filters[program] {
		return false
	}
	options := words[1:]
	if greps[program] && matchesAny(grepDeciding, options) {
		return false
	}
	return !(program == "tail" && matchesAny(tailFollowing, options))
}

func matchesAny(pattern *regexp.Regexp, words []string) bool {
	for _, word := range words {
		if pattern.MatchString(word) {
			return true
		}
	}
	return false
}
