package shell_test

import (
	"reflect"
	"testing"

	"github.com/andreasisaak/heavy/internal/shell"
)

func TestFromArgvKeepsTheWordsAndParsesTheShellString(t *testing.T) {
	got := shell.FromArgv([]string{"nice", "/bin/zsh", "-c", "cd frontend && make ci"})

	want := shell.Script{Commands: []shell.Command{
		{Argv: []string{"/bin/zsh", "-c", "cd frontend && make ci"}, Raw: []string{"nice", "/bin/zsh", "-c", "cd frontend && make ci"}, Offset: -1, Parent: -1},
		{Argv: []string{"cd", "frontend"}, Raw: []string{"cd", "frontend"}, Offset: -1, Parent: 0},
		{Argv: []string{"make", "ci"}, Raw: []string{"make", "ci"}, Offset: -1, Parent: 0},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FromArgv\n got %#v\nwant %#v", got, want)
	}
}

func TestFromArgvOfOnlyWrappersHoldsNoCommand(t *testing.T) {
	if got := shell.FromArgv([]string{"nice", "-n", "5"}); !reflect.DeepEqual(got, shell.Script{}) {
		t.Fatalf("FromArgv = %#v, want no command", got)
	}
}
