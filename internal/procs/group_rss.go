package procs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/andreasisaak/heavy/internal/procfs"
)

// unreadable reports an error of a process that ended while it was read, or that this user may not read (the
// process of another user under hidepid): neither is in a group of this user's run.
func unreadable(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH) || errors.Is(err, fs.ErrPermission)
}

// GroupRSS sums the resident memory of the processes of the group below a Linux /proc root: every process whose
// stat names the group, with the VmRSS of its status. A process that ended between the listing and the reading, or
// that this user may not read, is skipped; a kernel thread has no VmRSS and counts nothing.
func GroupRSS(root string, group int) (int64, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, fmt.Errorf("heavy: reading %s: %w", root, err)
	}
	var total int64
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		stat, err := procfs.ReadStat(root, pid)
		if unreadable(err) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("heavy: reading the stat of process %d: %w", pid, err)
		}
		if len(stat.Fields) < 3 {
			return 0, fmt.Errorf("heavy: %s: expected <pid> (<name>) <state> <parent> <group> …, got %q", stat.Path, stat.Raw)
		}
		if stat.Fields[2] != strconv.Itoa(group) {
			continue
		}
		bytes, err := residentBytes(filepath.Join(root, entry.Name(), "status"))
		if unreadable(err) {
			continue
		}
		if err != nil {
			return 0, err
		}
		total += bytes
	}
	return total, nil
}

// residentBytes is the VmRSS line of a status file, 0 without one.
func residentBytes(path string) (int64, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(content), "\n") {
		words := strings.Fields(line)
		if len(words) == 3 && words[0] == "VmRSS:" && words[2] == "kB" {
			kilobytes, err := strconv.ParseInt(words[1], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("heavy: %s: expected VmRSS: <n> kB, got %q", path, line)
			}
			return kilobytes * 1024, nil
		}
	}
	return 0, nil
}
