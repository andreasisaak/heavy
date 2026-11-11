package testkit_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/andreasisaak/heavy/internal/testkit"
)

func TestFakeProgramPrintsItsArgumentsAsLines(t *testing.T) {
	directory := t.TempDir()
	path := testkit.FakeProgram(t, directory, "fake", `for argument in "$@"; do printf '%s\n' "$argument"; done`)

	if path != filepath.Join(directory, "fake") {
		t.Fatalf("path = %q, want %q", path, filepath.Join(directory, "fake"))
	}
	output, err := exec.Command(path, "one", "two words", "").Output()
	if err != nil {
		t.Fatalf("running %s: %v", path, err)
	}
	got := strings.Split(string(output), "\n")
	want := []string{"one", "two words", "", ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %q, want %q", got, want)
	}
}

func TestBuildBinaryBuildsHeavyOnce(t *testing.T) {
	first := testkit.BuildBinary(t)
	second := testkit.BuildBinary(t)

	if first != second {
		t.Fatalf("second build at %q, want the first one at %q", second, first)
	}
	if filepath.Base(first) != "heavy" {
		t.Fatalf("binary name = %q, want heavy", filepath.Base(first))
	}
	output, err := exec.Command(first, "--help").Output()
	if err != nil {
		t.Fatalf("running %s --help: %v", first, err)
	}
	if !strings.HasPrefix(string(output), "Usage: heavy ") {
		t.Fatalf("--help printed %q, want the usage line", output)
	}
}

func TestStateDirSetsTheStateDirectory(t *testing.T) {
	directory := testkit.StateDir(t)

	if got := os.Getenv("HEAVY_STATE_DIRECTORY"); got != directory {
		t.Fatalf("HEAVY_STATE_DIRECTORY = %q, want %q", got, directory)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("reading %s: %v", directory, err)
	}
	if len(entries) != 0 {
		t.Fatalf("state directory holds %d entries, want an empty one", len(entries))
	}
}

func TestHomeSetsHome(t *testing.T) {
	directory := testkit.Home(t)

	if got := os.Getenv("HOME"); got != directory {
		t.Fatalf("HOME = %q, want %q", got, directory)
	}
	information, err := os.Stat(directory)
	if err != nil {
		t.Fatalf("stat %s: %v", directory, err)
	}
	if !information.IsDir() {
		t.Fatalf("%s is no directory", directory)
	}
}
