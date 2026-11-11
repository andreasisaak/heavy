package shell_test

import (
	"strings"
	"testing"

	"github.com/andreasisaak/heavy/internal/shell"
)

// place says where an expected command sits in the script.
type place int

const (
	placeList place = iota
	placeClause
	placeOther
)

// expected describes one simple command Parse should find.
type expected struct {
	at          string // the program token starts at the first occurrence of at in the input; "" for a command of a shell string
	raw         []string
	argv        []string // nil: the same as raw
	place       place
	substituted bool
	parent      int
}

type parseCase struct {
	origin string
	input  string
	want   []expected
}

// listed is a statement, pipeline stage or &&/|| operand of the top-level list.
func listed(at string, raw ...string) expected {
	return expected{at: at, raw: raw, place: placeList, parent: -1}
}

// clause is a command inside if, while, until, for, { } or ( ) of the top-level list.
func clause(at string, raw ...string) expected {
	return expected{at: at, raw: raw, place: placeClause, parent: -1}
}

// other is a command in a case or function body.
func other(at string, raw ...string) expected {
	return expected{at: at, raw: raw, place: placeOther, parent: -1}
}

// substitution is a command inside $( ), backticks or <( ).
func substitution(at string, raw ...string) expected {
	return expected{at: at, raw: raw, place: placeOther, substituted: true, parent: -1}
}

// nested is a command of the shell string (sh -c, eval, ssh) of the command at index parent.
func nested(parent int, raw ...string) expected {
	return expected{raw: raw, place: placeOther, parent: parent}
}

// normalized sets the program-first words when they differ from the raw ones.
func (e expected) normalized(argv ...string) expected {
	e.argv = argv
	return e
}

func (e expected) command(t *testing.T, input string) shell.Command {
	t.Helper()
	offset := -1
	if e.at != "" {
		offset = strings.Index(input, e.at)
		if offset < 0 {
			t.Fatalf("case error: %q is not in the input %q", e.at, input)
		}
	}
	argv := e.argv
	if argv == nil {
		argv = e.raw
	}
	return shell.Command{
		Argv:        argv,
		Raw:         e.raw,
		Offset:      offset,
		Substituted: e.substituted,
		TopLevel:    e.place == placeList,
		Clause:      e.place == placeClause,
		Parent:      e.parent,
	}
}
