package helper

import (
	"errors"
	"fmt"
	"syscall"
)

// signals maps the names of the protocol to the signals of the platform the helper runs on; the numbers of STOP
// and CONT differ between darwin and linux.
var signals = map[string]syscall.Signal{"STOP": syscall.SIGSTOP, "CONT": syscall.SIGCONT, "TERM": syscall.SIGTERM, "KILL": syscall.SIGKILL}

// SignalTagged sends the signal to each PID that still carries the tag right before the signal (EC-3); a PID without
// the tag or that ended meanwhile is skipped, and so are init and the PIDs kill(2) reads as groups (1 and below).
// Unprivileged, the process of another user, which reused the PID, is skipped too.
func SignalTagged(options Options, request Request) (Signaled, error) {
	if request.Tag == "" {
		return Signaled{}, errors.New("heavy: signal needs a tag")
	}
	number, known := signals[request.Signal]
	if !known {
		return Signaled{}, fmt.Errorf("heavy: unknown signal %q, expected STOP, CONT, TERM or KILL", request.Signal)
	}
	signaled := Signaled{Sent: []int{}, Skipped: []int{}}
	for _, pid := range request.PIDs {
		if pid <= 1 {
			signaled.Skipped = append(signaled.Skipped, pid)
			continue
		}
		carries, err := tagged(options.Root, pid, request.Tag)
		if err != nil && !vanished(err) && !(options.Unprivileged && foreign(err)) {
			return Signaled{}, fmt.Errorf("heavy: reading process %d: %w", pid, err)
		}
		if !carries {
			signaled.Skipped = append(signaled.Skipped, pid)
			continue
		}
		if err := options.Kill(pid, number); errors.Is(err, syscall.ESRCH) {
			signaled.Skipped = append(signaled.Skipped, pid)
			continue
		} else if err != nil {
			return Signaled{}, fmt.Errorf("heavy: sending %s to process %d: %w", request.Signal, pid, err)
		}
		signaled.Sent = append(signaled.Sent, pid)
	}
	return signaled, nil
}
