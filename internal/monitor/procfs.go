package monitor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/andreasisaak/heavy/internal/procfs"
)

// ticksPerSecond is USER_HZ, the unit of the times in /proc/<pid>/stat: 100 on every Linux architecture.
const ticksPerSecond = 100

// ReadProcProcess reads a process below a Linux /proc root: the parent and the start from its stat (fields 4 and
// 22 of proc(5), the start in ticks since the boot time of /proc/stat), and as its name the file name of argv[0],
// the program it was started as: the native Claude Code runs as ~/.local/bin/claude, a link to a file named after
// its version, which the kernel's name shows. A process without arguments, a kernel thread, keeps the kernel's name.
func ReadProcProcess(root string, pid int) (Process, error) {
	stat, err := procfs.ReadStat(root, pid)
	if errors.Is(err, fs.ErrNotExist) {
		return Process{}, fmt.Errorf("heavy: found no process with pid %d", pid)
	}
	if err != nil {
		return Process{}, fmt.Errorf("heavy: reading process %d: %w", pid, err)
	}
	form := fmt.Errorf("heavy: %s: expected <pid> (<name>) and 20 fields with the parent and the start, got %q", stat.Path, stat.Raw)
	if len(stat.Fields) < 20 {
		return Process{}, form
	}
	parent, err := strconv.Atoi(stat.Fields[1])
	if err != nil {
		return Process{}, form
	}
	ticks, err := strconv.ParseInt(stat.Fields[19], 10, 64)
	if err != nil {
		return Process{}, form
	}
	boot, err := bootTime(filepath.Join(root, "stat"))
	if err != nil {
		return Process{}, err
	}
	name := stat.Name
	if arguments, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "cmdline")); err == nil {
		if program, _, _ := strings.Cut(string(arguments), "\x00"); program != "" {
			name = filepath.Base(program)
		}
	}
	started := time.Unix(boot+ticks/ticksPerSecond, ticks%ticksPerSecond*int64(time.Second/ticksPerSecond))
	return Process{Parent: parent, Name: name, Started: started}, nil
}

// bootTime is the btime line of /proc/stat: the boot of the machine in seconds since the epoch.
func bootTime(path string) (int64, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("heavy: reading %s: %w", path, err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		words := strings.Fields(line)
		if len(words) == 2 && words[0] == "btime" {
			seconds, err := strconv.ParseInt(words[1], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("heavy: %s: expected a line btime <seconds>, got %q", path, line)
			}
			return seconds, nil
		}
	}
	return 0, fmt.Errorf("heavy: %s: expected a line btime <seconds>, found none", path)
}
