package procs

import (
	"fmt"
	"syscall"
)

// signalNames name the signals procs sends in its messages.
var signalNames = map[syscall.Signal]string{syscall.SIGSTOP: "SIGSTOP", syscall.SIGCONT: "SIGCONT", syscall.SIGTERM: "SIGTERM", syscall.SIGKILL: "SIGKILL"}

// SystemHost is the host of the daemon: signals through kill(2), resident memory through ps.
type SystemHost struct{}

// Signal sends the signal to every process of the group.
func (SystemHost) Signal(group int, signal syscall.Signal) error {
	if err := syscall.Kill(-group, signal); err != nil {
		return fmt.Errorf("heavy: sending %s to process group %d: %w", signalNames[signal], group, err)
	}
	return nil
}

// Alive reports whether a process of the group is left that a signal reaches; macOS answers EPERM for a group of
// zombies, which no signal reaches either.
func (SystemHost) Alive(group int) bool {
	return syscall.Kill(-group, 0) == nil
}

// OwnGroup is the process group of the daemon, which procs never signals.
func (SystemHost) OwnGroup() int {
	return syscall.Getpgrp()
}
