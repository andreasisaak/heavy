package procs

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// RSS sums the resident memory of the processes of the group through ps -g, which on macOS selects the processes
// of a process group; a group without processes has none.
func (SystemHost) RSS(group int) (int64, error) {
	output, err := exec.Command("ps", "-o", "rss=", "-g", strconv.Itoa(group)).Output()
	var exitError *exec.ExitError
	if errors.As(err, &exitError) && strings.TrimSpace(string(output)) == "" {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("heavy: ps for process group %d: %w", group, err)
	}
	var total int64
	for _, field := range strings.Fields(string(output)) {
		kilobytes, err := strconv.ParseInt(field, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("heavy: ps for process group %d printed %q, expected kilobytes", group, output)
		}
		total += kilobytes * 1024
	}
	return total, nil
}
