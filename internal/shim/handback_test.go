package shim_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andreasisaak/heavy/internal/testkit"
)

// handBackScript stands in for the shim of a version manager that holds no version of the program, as the mise
// shim of yarn under a Node without yarn: it becomes the first program of its name on PATH outside its own folder,
// and that is the heavy shim again.
const handBackScript = `own=${0%/*}
name=${0##*/}
IFS=:
for directory in $PATH; do
  [ "$directory" = "$own" ] && continue
  [ -x "$directory/$name" ] && exec "$directory/$name" "$@"
done
exit 127`

// manager puts the hand-back fake of program into a folder of its own and returns the folder.
func (f *fixture) manager(t *testing.T, program string) string {
	t.Helper()
	directory := filepath.Join(f.scratch, "manager")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	testkit.FakeProgram(t, directory, program, handBackScript)
	return directory
}

func TestProgramThatHandsTheCallBackIsPassedOverForTheNextOne(t *testing.T) {
	f := newFixture(t)
	manager := f.manager(t, "yarn")
	environment := []string{
		"PATH=" + f.shims + ":" + manager + ":" + f.fakes + ":/usr/bin:/bin", "HOME=" + f.home, "HEAVY_STATE_DIRECTORY=" + f.state,
	}
	arguments := []string{"--silent", "node", "-p", "process.version"}

	got := f.call(t, f.scratch, environment, "", "yarn", arguments...)

	want := outcome{stdout: printed("yarn", arguments...)}
	if got != want {
		t.Fatalf("yarn %q = %+v, want %+v", arguments, got, want)
	}
}

func TestCallThatEveryProgramHandsBackFailsWithTheirPaths(t *testing.T) {
	f := newFixture(t)
	manager := f.manager(t, "yarn")
	environment := []string{"PATH=" + f.shims + ":" + manager + ":/usr/bin:/bin", "HOME=" + f.home, "HEAVY_STATE_DIRECTORY=" + f.state}

	got := f.call(t, f.scratch, environment, "", "yarn", "--version")

	want := outcome{exitStatus: 127, stderr: "heavy: every yarn on PATH outside the heavy shims (" + f.shims +
		") handed the call back to them: " + filepath.Join(manager, "yarn") + "\n"}
	if got != want {
		t.Fatalf("yarn --version = %+v, want %+v", got, want)
	}
}

// The note of a pass in another process, inherited through the environment, skips nothing: a program that starts
// the shim anew as its child gets the first real program again.
func TestPassOfAnotherProcessSkipsNoProgram(t *testing.T) {
	f := newFixture(t)
	passed := "HEAVY_SHIM_PASSED=1:yarn:" + filepath.Join(f.fakes, "yarn")

	got := f.call(t, f.scratch, f.environment(passed), "", "yarn", "--version")

	want := outcome{stdout: printed("yarn", "--version")}
	if got != want {
		t.Fatalf("yarn --version = %+v, want %+v", got, want)
	}
}
