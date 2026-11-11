package guard

import (
	"fmt"
	"strings"
	"time"

	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/shell"
	"github.com/andreasisaak/heavy/internal/statusview"
)

// queueStatus names who runs and how many callers wait ahead, from the daemon's queue.
func queueStatus(status protocol.Status, now time.Time) string {
	runs := status.Runs
	parts := runningNow(runs, now)
	waiting := 0
	for _, run := range runs {
		if run.State == "waiting" {
			waiting++
		}
	}
	if waiting > 0 {
		parts = append(parts, fmt.Sprintf("Waiting ahead of you: %d.", waiting))
	}
	if len(parts) == 0 {
		return freeText
	}
	return strings.Join(parts, " ")
}

func runningNow(runs []protocol.RunStatus, now time.Time) []string {
	var lines []string
	for _, run := range runs {
		if run.State == "running" {
			lines = append(lines, fmt.Sprintf("Running now: `%s` in %s for %d min.", run.Command, run.Directory, statusview.Minutes(run, now)))
		}
	}
	return lines
}

// poll denies a call that reads the queue (`heavy --status …`) while a run of the session waits or runs, with the
// state of that run: the session is woken when the run ends and needs no poll.
func poll(script shell.Script, input hookInput, deps Dependencies) (*answer, error) {
	if !readsQueue(script) {
		return nil, nil
	}
	session, err := input.session()
	if err != nil {
		return nil, err
	}
	status, err := deps.Queue()
	if err != nil {
		return nil, err
	}
	runs, now := status.Runs, deps.Now()
	var own []string
	holdsLock := false
	for _, run := range statusview.Own(runs, session) {
		own = append(own, statusview.Describe(run, now))
		holdsLock = holdsLock || run.State == "running"
	}
	if len(own) == 0 {
		return nil, nil
	}
	running := ""
	if !holdsLock {
		for _, line := range runningNow(runs, now) {
			running += line + " "
		}
	}
	return denial(fmt.Sprintf(pollReason, strings.Join(own, "; "), running)), nil
}

// readsQueue reports a simple command `heavy --status …`.
func readsQueue(script shell.Script) bool {
	for _, command := range script.Commands {
		if len(command.Argv) > 1 && isHeavy(command.Argv[0]) && command.Argv[1] == "--status" {
			return true
		}
	}
	return false
}
