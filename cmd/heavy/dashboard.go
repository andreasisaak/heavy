package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/dashboard"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// runDashboard is `heavy --dashboard`, the live view of the queue in the terminal or a herdr pane (design G13). It
// only watches: it asks a running daemon and starts none, and asks again every 2 s while none answers.
func runDashboard(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) > 0 {
		fmt.Fprintf(stderr, "heavy: --dashboard takes no arguments, got %s. %s\n", arguments[0], usage)
		return exitUsage
	}
	connect, err := connectOptions()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	host, err := os.Hostname()
	if err != nil {
		fmt.Fprintf(stderr, "heavy: finding the host name for the dashboard: %v\n", err)
		return 1
	}
	host, _, _ = strings.Cut(host, ".")
	model := dashboard.New(dashboard.Options{Host: host, Fetch: dashboardFetch(connect), Send: dashboardSend(connect), Now: time.Now})
	if _, err := tea.NewProgram(model, tea.WithOutput(stdout)).Run(); err != nil {
		fmt.Fprintf(stderr, "heavy: the dashboard ended with an error: %v\n", err)
		return 1
	}
	return 0
}

// dashboardDeadline is how long the dashboard waits for the daemon's answer to one fetch or one action, so a daemon
// that hangs after its hello cannot freeze the view.
const dashboardDeadline = 5 * time.Second

// dashboardFetch asks a running daemon for the queue with the machine (Status with Detail); without a daemon it
// says so and starts none.
func dashboardFetch(connect daemon.ConnectOptions) func() (protocol.Status, error) {
	return func() (protocol.Status, error) {
		link, found, err := daemon.Reach(connect)
		if err != nil {
			return protocol.Status{}, err
		}
		if !found {
			return protocol.Status{}, fmt.Errorf("heavy: no daemon answers in %s; the next heavy run starts one", connect.StateDirectory)
		}
		defer link.Close()
		answer, err := askWithin(link, protocol.Message{Type: protocol.TypeStatus, Status: &protocol.Status{Detail: true}}, "dashboard")
		if err != nil {
			return protocol.Status{}, err
		}
		if answer.Type != protocol.TypeStatus {
			return protocol.Status{}, fmt.Errorf("heavy: the daemon answered the status with a %s message", answer.Type)
		}
		return *answer.Status, nil
	}
}

// dashboardSend sends the action of a key over the connection --forward, --pause, --resume and --stop use, which
// starts a daemon when none answers, and returns the daemon's line about it.
func dashboardSend(connect daemon.ConnectOptions) func(protocol.Message) (string, error) {
	return func(message protocol.Message) (string, error) {
		switch message.Type {
		case protocol.TypeForward, protocol.TypePause, protocol.TypeResume, protocol.TypeTerminate:
		default:
			return "", fmt.Errorf("heavy: the dashboard sends no %s message", message.Type)
		}
		link, err := daemon.Connect(connect)
		if err != nil {
			return "", err
		}
		defer link.Close()
		answer, err := askWithin(link, message, message.Type)
		if err != nil {
			return "", err
		}
		if answer.Type != protocol.TypePrint {
			return "", fmt.Errorf("heavy: the daemon answered the %s with a %s message", message.Type, answer.Type)
		}
		return answer.Print.Line, nil
	}
}

// askWithin sends the request and returns the daemon's answer within dashboardDeadline; what names the request in
// the error of a daemon that does not answer in time, and an error message of the daemon becomes the error.
func askWithin(link *daemon.Connection, request protocol.Message, what string) (protocol.Message, error) {
	if err := link.SetDeadline(time.Now().Add(dashboardDeadline)); err != nil {
		return protocol.Message{}, fmt.Errorf("heavy: setting the deadline of the %s: %w", what, err)
	}
	if err := link.Send(request); err != nil {
		return protocol.Message{}, fmt.Errorf("heavy: sending the %s request: %w", what, err)
	}
	answer, err := link.Receive()
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return protocol.Message{}, fmt.Errorf("heavy: the daemon did not answer the %s within %s", what, dashboardDeadline)
	}
	if err != nil {
		return protocol.Message{}, fmt.Errorf("heavy: reading the answer to the %s request: %w", what, err)
	}
	if answer.Type == protocol.TypeError {
		return protocol.Message{}, errors.New(answer.Error.Text)
	}
	return answer, nil
}
