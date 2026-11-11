package monitor_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/monitor"
)

// procTree writes a /proc tree: every key is a path below the root, every value the file's content.
func procTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// statLine is /proc/<pid>/stat with the parent in field 4 and the start in field 22, in ticks since boot.
func statLine(pid, name, parent, startTicks string) string {
	return pid + " (" + name + ") S " + parent + " " + strings.Repeat("0 ", 17) + startTicks + " 1000 200\n"
}

const bootFile = "cpu  1 2 3\nbtime 1794000000\nprocesses 5\n"

func TestReadProcProcessReadsParentProgramAndStart(t *testing.T) {
	root := procTree(t, map[string]string{
		"stat":         bootFile,
		"4711/stat":    statLine("4711", "2.1.319", "812", "123456"),
		"4711/cmdline": "/home/agents/.local/bin/claude\x00--resume\x00",
	})

	process, err := monitor.ReadProcProcess(root, 4711)

	want := monitor.Process{Parent: 812, Name: "claude", Started: time.Unix(1794000000+1234, 560*int64(time.Millisecond))}
	if err != nil || !reflect.DeepEqual(process, want) {
		t.Fatalf("ReadProcProcess = %#v, %v, want %#v", process, err, want)
	}
}

func TestReadProcProcessOfAKernelThreadTakesTheKernelName(t *testing.T) {
	root := procTree(t, map[string]string{
		"stat": bootFile, "2/stat": statLine("2", "kthreadd) R (x", "0", "5"), "2/cmdline": "",
	})

	process, err := monitor.ReadProcProcess(root, 2)

	want := monitor.Process{Parent: 0, Name: "kthreadd) R (x", Started: time.Unix(1794000000, 50*int64(time.Millisecond))}
	if err != nil || !reflect.DeepEqual(process, want) {
		t.Fatalf("ReadProcProcess = %#v, %v, want %#v", process, err, want)
	}
}

func TestReadProcProcessWithoutTheProcessIsAnError(t *testing.T) {
	root := procTree(t, map[string]string{"stat": bootFile})

	_, err := monitor.ReadProcProcess(root, 4711)

	if err == nil || err.Error() != "heavy: found no process with pid 4711" {
		t.Fatalf("ReadProcProcess error = %v, want %q", err, "heavy: found no process with pid 4711")
	}
}

func TestReadProcProcessWithABrokenStatIsAnError(t *testing.T) {
	root := procTree(t, map[string]string{"stat": bootFile, "4711/stat": "4711 (claude) S 812 1 2\n", "4711/cmdline": "claude\x00"})

	_, err := monitor.ReadProcProcess(root, 4711)

	want := "heavy: " + root + "/4711/stat: expected <pid> (<name>) and 20 fields with the parent and the start, got \"4711 (claude) S 812 1 2\\n\""
	if err == nil || err.Error() != want {
		t.Fatalf("ReadProcProcess error = %v, want %q", err, want)
	}
}

func TestReadProcProcessWithoutTheBootTimeIsAnError(t *testing.T) {
	root := procTree(t, map[string]string{"stat": "cpu  1 2 3\n", "4711/stat": statLine("4711", "claude", "812", "100"), "4711/cmdline": "claude\x00"})

	_, err := monitor.ReadProcProcess(root, 4711)

	want := "heavy: " + root + "/stat: expected a line btime <seconds>, found none"
	if err == nil || err.Error() != want {
		t.Fatalf("ReadProcProcess error = %v, want %q", err, want)
	}
}
