//go:build !darwin && !linux

package monitor

import "fmt"

// ReadProcess exists for macOS and Linux only.
func ReadProcess(pid int) (Process, error) {
	return Process{}, fmt.Errorf("heavy: reading process %d needs macOS or Linux; start the monitor with --session <id>", pid)
}
