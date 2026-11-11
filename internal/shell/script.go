// Package shell splits a Bash call into the simple commands it runs, on the parser of
// mvdan.cc/sh/v3. The guard and the detection read them: the words of each command, where
// its program starts in the call, and whether heavy can go right in front of it.
//
// Shell strings that a shell parses again (sh -c, bash -c, eval, ssh) are parsed as well,
// and their commands follow the command that holds them. Heredoc bodies with a quoted
// delimiter, quoted text and comments are no commands.
package shell

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Command is one simple command of a script.
type Command struct {
	// Argv starts at the real program: environment assignments, wrappers such as nice and
	// their options dropped, as hooks/shell_tokens.py normalize did.
	Argv []string
	// Raw holds every word with the assignments and wrappers in front of the program.
	Raw []string
	// Offset is the byte offset in the parsed call of the first word after the assignments,
	// the word heavy goes in front of; -1 for a command of a shell string.
	Offset int
	// Substituted is true for a command inside $( ), backticks, <( ) or >( ).
	Substituted bool
	// TopLevel is true for a statement, pipeline stage or &&/|| operand of the top-level
	// list of a call.
	TopLevel bool
	// Clause is true for a command inside if, while, until, for, { } or ( ) that sit in the
	// top-level list of a call.
	Clause bool
	// Parent is the index of the command whose shell string holds this one, -1 for none.
	Parent int
}

// Script holds the simple commands of a call in the order they appear.
type Script struct {
	Commands []Command
}

// Parse returns the simple commands of a Bash call, or the syntax.ParseError of a call
// that is no valid Bash. A shell string that is no valid Bash adds no command: the shell
// that gets it reports the error and runs none of it, and the rest of the call still counts.
func Parse(command string) (Script, error) {
	file, err := parse(command)
	if err != nil {
		return Script{}, err
	}
	found := &state{}
	walkFile(found, file, command, -1, false)
	return Script{Commands: found.commands}, nil
}

// state collects the commands of a call and of its shell strings.
type state struct {
	commands []Command
}

func parse(source string) (*syntax.File, error) {
	return syntax.NewParser(syntax.KeepComments(true), syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(source), "")
}

// walkFile appends the commands of a parsed source; parent is the command whose shell
// string source is, -1 for the call itself.
func walkFile(found *state, file *syntax.File, source string, parent int, substituted bool) {
	current := &walker{state: found, source: source, parent: parent, substituted: substituted}
	current.statements(file.Stmts, placeList, false)
}

// parseShellString appends the commands of the string a shell runs again behind the
// command at index parent.
func parseShellString(found *state, text string, parent int, substituted bool) {
	file, err := parse(text)
	if err != nil {
		return
	}
	walkFile(found, file, text, parent, substituted)
}
