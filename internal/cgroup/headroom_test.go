package cgroup_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/andreasisaak/heavy/internal/cgroup"
)

const gibibyte = int64(1) << 30

// tree writes files below a new root: every key is a path, every value the content.
func tree(t *testing.T, files map[string]string) string {
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

func bytes(gibibytes int64) string {
	return strconv.FormatInt(gibibytes*gibibyte, 10) + "\n"
}

const userPath = "user.slice/user-1000.slice/user@1000.service/app.slice"

func procOf(t *testing.T, line string) string {
	return tree(t, map[string]string{"self/cgroup": line})
}

func TestHeadroomIsTheTightestLimitAboveTheProcess(t *testing.T) {
	proc := procOf(t, "0::/"+userPath+"\n")
	groups := tree(t, map[string]string{
		"memory.stat":           "anon 1\n",
		"user.slice/memory.max": "max\n", "user.slice/memory.high": "max\n", "user.slice/memory.current": bytes(90),
		"user.slice/user-1000.slice/memory.high": bytes(80), "user.slice/user-1000.slice/memory.max": bytes(96),
		"user.slice/user-1000.slice/memory.current":                   bytes(70),
		"user.slice/user-1000.slice/user@1000.service/memory.max":     bytes(100),
		"user.slice/user-1000.slice/user@1000.service/memory.high":    "max\n",
		"user.slice/user-1000.slice/user@1000.service/memory.current": bytes(95),
		userPath + "/cgroup.procs":                                    "1\n",
	})

	headroom, limited, err := cgroup.Headroom(proc, groups)

	if err != nil || !limited || headroom != 5*gibibyte {
		t.Fatalf("Headroom = %d, %v, %v, want %d, true", headroom, limited, err, 5*gibibyte)
	}
}

func TestHeadroomOfAGroupOverItsLimitIsZero(t *testing.T) {
	proc := procOf(t, "0::/user.slice\n")
	groups := tree(t, map[string]string{"user.slice/memory.high": bytes(8), "user.slice/memory.max": "max\n", "user.slice/memory.current": bytes(9)})

	headroom, limited, err := cgroup.Headroom(proc, groups)

	if err != nil || !limited || headroom != 0 {
		t.Fatalf("Headroom = %d, %v, %v, want 0, true", headroom, limited, err)
	}
}

func TestHeadroomWithoutLimitsIsNotLimited(t *testing.T) {
	proc := procOf(t, "0::/user.slice\n")
	groups := tree(t, map[string]string{"user.slice/memory.high": "max\n", "user.slice/memory.max": "max\n", "user.slice/memory.current": bytes(9)})

	headroom, limited, err := cgroup.Headroom(proc, groups)

	if err != nil || limited || headroom != 0 {
		t.Fatalf("Headroom = %d, %v, %v, want 0, false", headroom, limited, err)
	}
}

func TestHeadroomOfTheRootGroupIsNotLimited(t *testing.T) {
	proc := procOf(t, "0::/\n")
	groups := tree(t, map[string]string{"cgroup.procs": "1\n"})

	headroom, limited, err := cgroup.Headroom(proc, groups)

	if err != nil || limited || headroom != 0 {
		t.Fatalf("Headroom = %d, %v, %v, want 0, false", headroom, limited, err)
	}
}

func TestHeadroomWithoutAVersion2GroupIsNotLimited(t *testing.T) {
	proc := procOf(t, "12:memory:/user.slice\n11:cpu:/\n")
	groups := tree(t, map[string]string{"user.slice/memory.max": bytes(1), "user.slice/memory.current": bytes(1)})

	headroom, limited, err := cgroup.Headroom(proc, groups)

	if err != nil || limited || headroom != 0 {
		t.Fatalf("Headroom = %d, %v, %v, want 0, false", headroom, limited, err)
	}
}

func TestHeadroomWithABrokenLimitIsAnError(t *testing.T) {
	proc := procOf(t, "0::/user.slice\n")
	groups := tree(t, map[string]string{"user.slice/memory.max": "lots\n", "user.slice/memory.current": bytes(1)})

	_, _, err := cgroup.Headroom(proc, groups)

	want := "heavy: " + groups + "/user.slice/memory.max: expected bytes or max, got \"lots\\n\""
	if err == nil || err.Error() != want {
		t.Fatalf("Headroom error = %v, want %q", err, want)
	}
}

func TestHeadroomWithALimitButNoUsageIsAnError(t *testing.T) {
	proc := procOf(t, "0::/user.slice\n")
	groups := tree(t, map[string]string{"user.slice/memory.max": bytes(4)})

	_, _, err := cgroup.Headroom(proc, groups)

	want := "heavy: reading " + groups + "/user.slice/memory.current: open " + groups + "/user.slice/memory.current: no such file or directory"
	if err == nil || err.Error() != want {
		t.Fatalf("Headroom error = %v, want %q", err, want)
	}
}

func TestHeadroomWithoutTheCgroupFileIsAnError(t *testing.T) {
	proc := t.TempDir()

	_, _, err := cgroup.Headroom(proc, t.TempDir())

	want := "heavy: reading " + proc + "/self/cgroup: open " + proc + "/self/cgroup: no such file or directory"
	if err == nil || err.Error() != want {
		t.Fatalf("Headroom error = %v, want %q", err, want)
	}
}

// File cache counts as free: the kernel gives it back before the limit holds anything up, and MemAvailable of
// the machine counts it as available too.
func TestHeadroomCountsTheFileCacheOfAGroupAsFree(t *testing.T) {
	proc := procOf(t, "0::/user.slice\n")
	groups := tree(t, map[string]string{
		"user.slice/memory.high": bytes(8), "user.slice/memory.max": "max\n", "user.slice/memory.current": bytes(8),
		"user.slice/memory.stat": "anon 1073741824\nfile 7516192768\ninactive_file 3221225472\nactive_file 2147483648\nshmem 1073741824\n",
	})

	headroom, limited, err := cgroup.Headroom(proc, groups)

	if err != nil || !limited || headroom != 5*gibibyte {
		t.Fatalf("Headroom = %d, %v, %v, want %d, true: the limit less the usage without 5 GiB of file cache", headroom, limited, err, 5*gibibyte)
	}
}

func TestHeadroomWithABrokenStatLineIsAnError(t *testing.T) {
	proc := procOf(t, "0::/user.slice\n")
	groups := tree(t, map[string]string{
		"user.slice/memory.max": bytes(8), "user.slice/memory.current": bytes(8), "user.slice/memory.stat": "inactive_file lots\n",
	})

	_, _, err := cgroup.Headroom(proc, groups)

	want := "heavy: " + groups + "/user.slice/memory.stat: expected inactive_file <bytes>, got \"inactive_file lots\""
	if err == nil || err.Error() != want {
		t.Fatalf("Headroom error = %v, want %q", err, want)
	}
}
