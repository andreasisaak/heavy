package shell_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"

	"github.com/andreasisaak/heavy/internal/shell"
)

func TestParseFindsEverySimpleCommand(t *testing.T) {
	for _, cases := range [][]parseCase{detectCases, guardCases, correctedCases, nestedCases} {
		for _, current := range cases {
			t.Run(current.origin+" "+current.input, func(t *testing.T) {
				var want []shell.Command
				for _, command := range current.want {
					want = append(want, command.command(t, current.input))
				}

				script, err := shell.Parse(current.input)

				if err != nil {
					t.Fatalf("Parse(%q) failed: %v", current.input, err)
				}
				if !reflect.DeepEqual(script.Commands, want) {
					t.Fatalf("Parse(%q).Commands\n got %#v\nwant %#v", current.input, script.Commands, want)
				}
			})
		}
	}
}

func TestParseReportsInvalidBashWithItsPosition(t *testing.T) {
	_, err := shell.Parse(`make ci "`)

	var parseError syntax.ParseError
	if !errors.As(err, &parseError) {
		t.Fatalf("Parse error = %#v, want a syntax.ParseError", err)
	}
	got := [3]any{parseError.Pos.Line(), parseError.Pos.Col(), parseError.Text}
	want := [3]any{uint(1), uint(9), "reached EOF without closing quote `\"`"}
	if got != want {
		t.Fatalf("parse error at line, column, text %v, want %v", got, want)
	}
}

func TestShellStringThatIsNoValidBashAddsNoCommand(t *testing.T) {
	input := `ssh host 'echo "hi' && make ci`

	script, err := shell.Parse(input)

	if err != nil {
		t.Fatalf("Parse(%q) failed: %v", input, err)
	}
	want := []shell.Command{
		{Argv: []string{"ssh", "host", `echo "hi`}, Raw: []string{"ssh", "host", `echo "hi`}, Offset: 0, TopLevel: true, Parent: -1},
		{Argv: []string{"make", "ci"}, Raw: []string{"make", "ci"}, Offset: strings.Index(input, "make"), TopLevel: true, Parent: -1},
	}
	if !reflect.DeepEqual(script.Commands, want) {
		t.Fatalf("Parse(%q).Commands\n got %#v\nwant %#v", input, script.Commands, want)
	}
}

func TestQuoteFollowsShlex(t *testing.T) {
	cases := map[string]string{
		"a b":                   "'a b'",
		"it's":                  `'it'"'"'s'`,
		"":                      "''",
		"make":                  "make",
		"/opt/heavy/bin/heavy":  "/opt/heavy/bin/heavy",
		"a@b%c+d=e:f,g.h/i_j-k": "a@b%c+d=e:f,g.h/i_j-k",
		"$HOME":                 "'$HOME'",
		"ü":                     "'ü'",
	}
	for word, want := range cases {
		if got := shell.Quote(word); got != want {
			t.Errorf("Quote(%q) = %q, want %q", word, got, want)
		}
	}
}
