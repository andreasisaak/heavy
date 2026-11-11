package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/monitor"
)

const (
	// monitorFindFor is how long the monitor looks for the registration the SessionStart hook writes (design G9).
	monitorFindFor = 30 * time.Second
	// monitorRetryPause is the pause between two searches and before the monitor subscribes again.
	monitorRetryPause = time.Second
	// monitorOutageNotice is how long the daemon may stay out of reach before the session hears of it.
	monitorOutageNotice = time.Minute
)

// runMonitor is `heavy --monitor [--session <id>]`, the plugin monitor of monitors/monitors.json, started through
// hooks/build.sh; it runs until SIGTERM or SIGINT. Connecting starts a daemon when none answers: a subscriber keeps
// the daemon alive (design G3).
func runMonitor(arguments []string, stdout, stderr io.Writer) int {
	session := ""
	switch {
	case len(arguments) == 0:
	case arguments[0] == "--session" && (len(arguments) < 2 || arguments[1] == ""):
		fmt.Fprintf(stderr, "heavy: --session needs a session id. %s\n", usage)
		return exitUsage
	case arguments[0] == "--session" && len(arguments) == 2:
		session = arguments[1]
	default:
		unexpected := arguments[0]
		if unexpected == "--session" {
			unexpected = arguments[2]
		}
		fmt.Fprintf(stderr, "heavy: --monitor takes only --session <id>, got %s. %s\n", unexpected, usage)
		return exitUsage
	}
	connect, err := connectOptions()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	err = monitor.Run(ctx, monitor.Dependencies{
		StateDirectory: connect.StateDirectory,
		Connect:        func() (*daemon.Connection, error) { return daemon.Connect(connect) },
		Process:        monitor.ReadProcess, PID: os.Getpid(), Session: session, Stdout: stdout, Stderr: stderr,
		FindFor: monitorFindFor, RetryPause: monitorRetryPause, Now: time.Now, OutageNotice: monitorOutageNotice,
	})
	if err != nil {
		// stderr of a plugin monitor reaches nobody, so the session hears the reason as a notice (V29).
		fmt.Fprintln(stdout, "heavy: the session monitor stopped: "+err.Error())
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
