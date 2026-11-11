//go:build !darwin && !linux

package procs

import "fmt"

// RSS exists for macOS and Linux only.
func (SystemHost) RSS(group int) (int64, error) {
	return 0, fmt.Errorf("heavy: measuring process group %d needs macOS or Linux", group)
}
