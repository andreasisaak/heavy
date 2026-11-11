package monitor

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// ReadProcess reads a process from the process table of the kernel (sysctl kern.proc.pid). For a PID without a
// process the kernel answers with no entry, which x/sys reports as EIO.
func ReadProcess(pid int) (Process, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if errors.Is(err, unix.EIO) {
		return Process{}, fmt.Errorf("heavy: found no process with pid %d", pid)
	}
	if err != nil {
		return Process{}, fmt.Errorf("heavy: reading process %d: %w", pid, err)
	}
	start := info.Proc.P_starttime
	return Process{
		Parent:  int(info.Eproc.Ppid),
		Name:    programName(pid, unix.ByteSliceToString(info.Proc.P_comm[:])),
		Started: time.Unix(start.Sec, int64(start.Usec)*int64(time.Microsecond)),
	}, nil
}

// programName is the file name of argv[0], the program a process was started as: the native Claude Code runs as
// ~/.local/bin/claude, a link to a file named after its version, which the kernel's name (P_comm) shows. The
// arguments of a process of another user cannot be read; its kernel name stands in, since such a process is never
// the user's Claude Code. kern.procargs2 holds argc, the executable path, padding and then argv.
func programName(pid int, kernelName string) string {
	data, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil || len(data) < 4 {
		return kernelName
	}
	rest := data[4:]
	end := bytes.IndexByte(rest, 0)
	if end < 0 {
		return kernelName
	}
	rest = bytes.TrimLeft(rest[end:], "\x00")
	if end = bytes.IndexByte(rest, 0); end > 0 {
		return filepath.Base(string(rest[:end]))
	}
	return kernelName
}
