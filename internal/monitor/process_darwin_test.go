package monitor_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/monitor"
)

// a child started by the test has the test as its parent, the name of its program and a start between the times
// around its start
func TestReadProcessNamesParentProgramAndStart(t *testing.T) {
	before := time.Now()
	child := exec.Command("/bin/sleep", "5")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	after := time.Now()
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})

	got, err := monitor.ReadProcess(child.Process.Pid)

	if err != nil || got.Parent != os.Getpid() || got.Name != "sleep" {
		t.Fatalf("ReadProcess(%d) = %+v, %v; want parent %d and name sleep", child.Process.Pid, got, err, os.Getpid())
	}
	if got.Started.Before(before.Truncate(time.Microsecond)) || got.Started.After(after) {
		t.Fatalf("ReadProcess(%d) started at %s, want between %s and %s", child.Process.Pid, got.Started, before, after)
	}
}

func TestReadProcessOfAMissingProcessIsAnError(t *testing.T) {
	_, err := monitor.ReadProcess(999999)

	if want := "heavy: found no process with pid 999999"; err == nil || err.Error() != want {
		t.Fatalf("ReadProcess(999999) = %v, want %q", err, want)
	}
}

// gate probe 5: the native Claude Code runs as ~/.local/bin/claude, a link to a file named after its version, so
// the kernel names it 2.1.319; the name of a process is the program it was started as (argv[0])
func TestReadProcessNamesTheProgramItWasStartedAs(t *testing.T) {
	link := filepath.Join(t.TempDir(), "claude")
	if err := os.Symlink("/bin/sleep", link); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(link, "5")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})

	got, err := monitor.ReadProcess(child.Process.Pid)

	if err != nil || got.Name != "claude" {
		t.Fatalf("ReadProcess(%d) = %+v, %v; want the name claude", child.Process.Pid, got, err)
	}
}
