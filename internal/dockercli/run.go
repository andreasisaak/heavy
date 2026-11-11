// Package dockercli runs short calls of the docker program within a deadline: docker hangs while OrbStack sleeps.
package dockercli

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Run runs docker with the arguments and input, ends it after the timeout, and returns its stdout; the error names
// the exit and stderr.
func Run(program string, timeout time.Duration, input string, arguments ...string) (string, error) {
	deadline, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(deadline, program, arguments...)
	command.WaitDelay = time.Second
	if input != "" {
		command.Stdin = strings.NewReader(input)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if deadline.Err() != nil {
		return "", fmt.Errorf("docker %s did not end within %s", arguments[0], timeout)
	}
	if err != nil && stderr.Len() > 0 {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if err != nil {
		return "", err
	}
	return stdout.String(), nil
}
