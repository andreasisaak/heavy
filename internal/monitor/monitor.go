// Package monitor is `heavy --monitor [--session <id>]`, the plugin monitor of a Claude Code session (design G9).
// Claude Code runs it for the whole session and hands every line on its stdout to the model as a notification, so
// stdout carries exactly the events meant for the session, one line each: the daemon pausing, resuming or
// restarting one of its runs, foreign load in its checkout, and a daemon out of reach for a minute and back again. The monitor finds its session through the parent
// PIDs up to the registration of its Claude Code process, $STATE/sessions/<pid>.json, or takes it from --session;
// it subscribes to the daemon and subscribes again whenever it loses the daemon. Its few notes go to stderr.
package monitor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/andreasisaak/heavy/internal/daemon"
)

// Dependencies are what the monitor reaches outside itself.
type Dependencies struct {
	// StateDirectory holds the session registrations.
	StateDirectory string
	// Connect reaches the daemon, starting one when none answers.
	Connect func() (*daemon.Connection, error)
	// Process reads a process: its parent, its program and its start.
	Process func(pid int) (Process, error)
	// PID is the PID of the monitor; the search for its Claude Code process starts at its parent.
	PID int
	// Session is the session of --session; "" makes the monitor find its Claude Code process instead.
	Session string
	Stdout  io.Writer
	Stderr  io.Writer
	// FindFor is how long the monitor looks for the registration of its Claude Code process, which the SessionStart
	// hook may write a moment after the monitor started.
	FindFor time.Duration
	// RetryPause is the pause between two searches and before each new subscription.
	RetryPause time.Duration
	// Now is the time an outage of the daemon starts and ends at.
	Now func() time.Time
	// OutageNotice is how long the daemon may stay out of reach before the session hears of it; a shorter loss,
	// such as the handover to a newer heavy, stays a note on stderr.
	OutageNotice time.Duration
}

// Process is what the monitor reads of a process: the PID of its parent, the name of its program as the kernel
// keeps it (at most 16 bytes) and the time it started.
type Process struct {
	Parent  int
	Name    string
	Started time.Time
}

// Run follows the daemon until ctx ends and then returns nil. It returns an error when it finds no registered
// session, when a registration cannot be read, or when stdout takes no line.
func Run(ctx context.Context, deps Dependencies) error {
	whom, err := locate(ctx, deps)
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return err
	}
	var away time.Time
	told := false
	back := func() error {
		away = time.Time{}
		if !told {
			return nil
		}
		told = false
		return notify(deps.Stdout, "heavy: the queue daemon answers again at "+deps.Now().Format("15:04:05"))
	}
	for {
		err := follow(ctx, deps, whom, back)
		if ctx.Err() != nil {
			return nil
		}
		var reason lost
		if !errors.As(err, &reason) {
			return err
		}
		if away.IsZero() {
			away = deps.Now()
		}
		fmt.Fprintf(deps.Stderr, "heavy: %s, subscribing again\n", reason)
		if !told && deps.Now().Sub(away) >= deps.OutageNotice {
			line := fmt.Sprintf("heavy: the queue daemon is unreachable since %s (%s); your runs keep running and heavy keeps trying",
				away.Format("15:04:05"), reason)
			if err := notify(deps.Stdout, line); err != nil {
				return err
			}
			told = true
		}
		if !pause(ctx, deps.RetryPause) {
			return nil
		}
	}
}

// notify writes one line for the session to stdout.
func notify(stdout io.Writer, line string) error {
	if _, err := io.WriteString(stdout, line+"\n"); err != nil {
		return fmt.Errorf("heavy: writing a notice to stdout: %w", err)
	}
	return nil
}

// pause waits for the length of time or the end of ctx; false when ctx ended.
func pause(ctx context.Context, length time.Duration) bool {
	timer := time.NewTimer(length)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
