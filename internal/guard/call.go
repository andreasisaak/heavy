package guard

import (
	"strings"

	"github.com/andreasisaak/heavy/internal/shell"
	"mvdan.cc/sh/v3/syntax"
)

// call is a Bash call parsed twice: its simple commands (internal/shell), which the detection reads and whose
// offsets place heavy, and its syntax tree, which shows the redirects, pipelines and word ends the rewrites need.
type call struct {
	source string
	script shell.Script
	file   *syntax.File
}

// parseCall parses a call; a call that is no valid Bash is the parser's error.
func parseCall(source string) (call, error) {
	script, err := shell.Parse(source)
	if err != nil {
		return call{}, err
	}
	file, err := syntax.NewParser(syntax.KeepComments(true), syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(source), "")
	if err != nil {
		return call{}, err
	}
	return call{source: source, script: script, file: file}, nil
}

// placeable reports a command heavy can go right in front of: in the top-level list of a call, or in an if, while,
// until, for, { } or ( ) of that list, outside substitutions and shell strings. A heavy command anywhere else runs
// with the whole call through heavy.
func placeable(command shell.Command) bool {
	return command.Parent < 0 && !command.Substituted && (command.TopLevel || command.Clause) && command.Offset >= 0
}

// rootOffset is the offset in the call of the command at index, or of the command whose shell string holds it.
func rootOffset(script shell.Script, index int) int {
	for script.Commands[index].Parent >= 0 {
		index = script.Commands[index].Parent
	}
	return script.Commands[index].Offset
}

// descendsFrom reports whether the command at index comes from the shell string of one of the commands in ancestors.
func descendsFrom(script shell.Script, index int, ancestors map[int]bool) bool {
	for parent := script.Commands[index].Parent; parent >= 0; parent = script.Commands[parent].Parent {
		if ancestors[parent] {
			return true
		}
	}
	return false
}

// commandAt is the simple command of the call whose first word after the assignments starts at offset.
func commandAt(script shell.Script, offset int) (shell.Command, bool) {
	for _, command := range script.Commands {
		if command.Parent < 0 && command.Offset == offset {
			return command, true
		}
	}
	return shell.Command{}, false
}

// callAt is the syntax node of the simple command whose first word after the assignments starts at offset.
func callAt(file *syntax.File, offset int) *syntax.CallExpr {
	var found *syntax.CallExpr
	syntax.Walk(file, func(node syntax.Node) bool {
		if expression, isCall := node.(*syntax.CallExpr); isCall && len(expression.Args) > 0 && int(expression.Args[0].Pos().Offset()) == offset {
			found = expression
		}
		return found == nil
	})
	return found
}

// walkOutsideSubstitutions visits the nodes of the call that the shell runs as part of it, not inside $( ),
// backticks or <( ); visit returns false to skip the children of a node and never gets the nil that ends them.
func walkOutsideSubstitutions(file *syntax.File, visit func(node syntax.Node) bool) {
	syntax.Walk(file, func(node syntax.Node) bool {
		switch node.(type) {
		case nil:
			return true
		case *syntax.CmdSubst, *syntax.ProcSubst:
			return false
		}
		return visit(node)
	})
}

// sourceOf is the text of a node as written in the call.
func sourceOf(source string, node syntax.Node) string {
	return source[node.Pos().Offset():node.End().Offset()]
}
