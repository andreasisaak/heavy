package shell

import (
	"mvdan.cc/sh/v3/syntax"
)

// place says where a statement sits: in the top-level list, inside if, while, until, for
// or { } of that list, or anywhere else (subshell, case, function, substitution).
type place int

const (
	placeList place = iota
	placeClause
	placeOther
)

// inside is the place of the statements of a compound command at the given place.
func inside(at place) place {
	if at == placeOther {
		return placeOther
	}
	return placeClause
}

// walker appends the simple commands of one parsed source to the shared state.
type walker struct {
	state       *state
	source      string
	parent      int
	substituted bool
}

func (w *walker) statements(statements []*syntax.Stmt, at place, substituted bool) {
	for _, statement := range statements {
		w.statement(statement, at, substituted)
	}
}

func (w *walker) statement(statement *syntax.Stmt, at place, substituted bool) {
	if statement.Cmd != nil {
		w.command(statement.Cmd, at, substituted)
	}
	for _, redirect := range statement.Redirs {
		w.substitutions(redirect)
	}
}

func (w *walker) command(command syntax.Command, at place, substituted bool) {
	switch node := command.(type) {
	case *syntax.CallExpr:
		w.call(node, at, substituted)
	case *syntax.DeclClause:
		w.declaration(node, at, substituted)
	case *syntax.BinaryCmd:
		w.statement(node.X, at, substituted)
		w.statement(node.Y, at, substituted)
	case *syntax.TimeClause:
		if node.Stmt != nil {
			w.statement(node.Stmt, at, substituted)
		}
	case *syntax.Block:
		w.statements(node.Stmts, inside(at), substituted)
	case *syntax.IfClause:
		for clause := node; clause != nil; clause = clause.Else {
			w.statements(clause.Cond, inside(at), substituted)
			w.statements(clause.Then, inside(at), substituted)
		}
	case *syntax.WhileClause:
		w.statements(node.Cond, inside(at), substituted)
		w.statements(node.Do, inside(at), substituted)
	case *syntax.ForClause:
		w.substitutions(node.Loop)
		w.statements(node.Do, inside(at), substituted)
	case *syntax.Subshell:
		w.statements(node.Stmts, inside(at), substituted)
	case *syntax.CaseClause:
		w.substitutions(node.Word)
		for _, item := range node.Items {
			for _, pattern := range item.Patterns {
				w.substitutions(pattern)
			}
			w.statements(item.Stmts, placeOther, substituted)
		}
	case *syntax.FuncDecl:
		w.statement(node.Body, placeOther, substituted)
	case *syntax.CoprocClause:
		w.statement(node.Stmt, placeOther, substituted)
	default:
		// [[ ]], (( )), let: no simple command of their own, but substitutions inside.
		w.substitutions(node)
	}
}

// call appends a simple command, then the commands of its shell string, then those of
// the substitutions in its words.
func (w *walker) call(call *syntax.CallExpr, at place, substituted bool) {
	var raw, arguments []string
	for _, assign := range call.Assigns {
		raw = append(raw, w.assignment(assign))
	}
	for _, word := range call.Args {
		arguments = append(arguments, w.word(word))
	}
	raw = append(raw, arguments...)
	if len(call.Args) > 0 {
		w.add(raw, Normalize(arguments), int(call.Args[0].Pos().Offset()), at, substituted)
	}
	for _, assign := range call.Assigns {
		w.substitutions(assign)
	}
	for _, word := range call.Args {
		w.substitutions(word)
	}
}

// declaration appends export, local, declare and the like as a command of their own, as 0.6 split them.
func (w *walker) declaration(declaration *syntax.DeclClause, at place, substituted bool) {
	raw := []string{declaration.Variant.Value}
	for _, assign := range declaration.Args {
		raw = append(raw, w.assignment(assign))
	}
	w.add(raw, Normalize(raw), int(declaration.Variant.Pos().Offset()), at, substituted)
	for _, assign := range declaration.Args {
		w.substitutions(assign)
	}
}

func (w *walker) add(raw, argv []string, offset int, at place, substituted bool) {
	if len(argv) == 0 {
		return
	}
	if w.parent >= 0 {
		offset = -1
		at = placeOther
	}
	index := len(w.state.commands)
	w.state.commands = append(w.state.commands, Command{
		Argv:        argv,
		Raw:         raw,
		Offset:      offset,
		Substituted: substituted || w.substituted,
		TopLevel:    at == placeList,
		Clause:      at == placeClause,
		Parent:      w.parent,
	})
	for _, text := range shellStrings(argv) {
		parseShellString(w.state, text, index, substituted || w.substituted)
	}
}

// substitutions appends the commands of every $( ), backtick and <( ) inside node.
func (w *walker) substitutions(node syntax.Node) {
	syntax.Walk(node, func(child syntax.Node) bool {
		switch inner := child.(type) {
		case *syntax.CmdSubst:
			w.statements(inner.Stmts, placeOther, true)
			return false
		case *syntax.ProcSubst:
			w.statements(inner.Stmts, placeOther, true)
			return false
		}
		return true
	})
}
