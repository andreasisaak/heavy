package procs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andreasisaak/heavy/internal/procs"
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

// stat is /proc/<pid>/stat up to the process group: pid, name, state, parent, group.
func stat(pid, name, group string) string {
	return pid + " (" + name + ") S 1 " + group + " " + group + " 0 -1 4194560 100\n"
}

func TestGroupRSSSumsTheProcessesOfTheGroup(t *testing.T) {
	root := procTree(t, map[string]string{
		"400/stat": stat("400", "sh", "400"), "400/status": "Name:\tsh\nVmRSS:\t    1024 kB\n",
		"401/stat": stat("401", "php) S 1 999 (x", "400"), "401/status": "Name:\tphp\nVmRSS:\t    2048 kB\n",
		"402/stat": stat("402", "sleep", "999"), "402/status": "Name:\tsleep\nVmRSS:\t    4096 kB\n",
		"403/stat": stat("403", "kworker", "400"), "403/status": "Name:\tkworker\n",
		"meminfo": "MemTotal: 1 kB\n", "self/stat": stat("7", "heavy", "400"),
	})

	bytes, err := procs.GroupRSS(root, 400)

	if err != nil || bytes != (1024+2048)*1024 {
		t.Fatalf("GroupRSS = %d, %v, want %d", bytes, err, (1024+2048)*1024)
	}
}

func TestGroupRSSOfAGroupWithoutProcessesIsZero(t *testing.T) {
	root := procTree(t, map[string]string{"402/stat": stat("402", "sleep", "999"), "402/status": "VmRSS:\t 4096 kB\n"})

	bytes, err := procs.GroupRSS(root, 400)

	if err != nil || bytes != 0 {
		t.Fatalf("GroupRSS = %d, %v, want 0", bytes, err)
	}
}

func TestGroupRSSSkipsAProcessThatEndedMeanwhile(t *testing.T) {
	root := procTree(t, map[string]string{
		"400/stat": stat("400", "sh", "400"), "400/status": "VmRSS:\t 1024 kB\n",
		"401/gone": "",
		"402/stat": stat("402", "php", "400"),
	})

	bytes, err := procs.GroupRSS(root, 400)

	if err != nil || bytes != 1024*1024 {
		t.Fatalf("GroupRSS = %d, %v, want %d", bytes, err, 1024*1024)
	}
}

func TestGroupRSSOfABrokenStatIsAnError(t *testing.T) {
	root := procTree(t, map[string]string{"400/stat": "400 sh S 1\n"})

	_, err := procs.GroupRSS(root, 400)

	want := "heavy: " + root + "/400/stat: expected <pid> (<name>) <state> <parent> <group> …, got \"400 sh S 1\\n\""
	if err == nil || err.Error() != want {
		t.Fatalf("GroupRSS error = %v, want %q", err, want)
	}
}

func TestGroupRSSWithoutProcIsAnError(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")

	_, err := procs.GroupRSS(root, 400)

	want := "heavy: reading " + root + ": open " + root + ": no such file or directory"
	if err == nil || err.Error() != want {
		t.Fatalf("GroupRSS error = %v, want %q", err, want)
	}
}

// With hidepid=noaccess the files of another user's process cannot be read; such a process is in no group of
// this user.
func TestGroupRSSSkipsAProcessItMayNotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file")
	}
	root := procTree(t, map[string]string{
		"400/stat": stat("400", "sh", "400"), "400/status": "VmRSS:\t 1024 kB\n",
		"1/stat": stat("1", "systemd", "1"),
	})
	if err := os.Chmod(filepath.Join(root, "1", "stat"), 0); err != nil {
		t.Fatal(err)
	}

	bytes, err := procs.GroupRSS(root, 400)

	if err != nil || bytes != 1024*1024 {
		t.Fatalf("GroupRSS = %d, %v, want %d", bytes, err, 1024*1024)
	}
}
