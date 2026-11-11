package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/sessions"
	"github.com/andreasisaak/heavy/internal/testkit"
)

func TestMonitorWithWrongArgumentsExitsWithUsageError(t *testing.T) {
	cases := map[string]struct {
		arguments []string
		want      string
	}{
		"session without id": {[]string{"--monitor", "--session"}, "heavy: --session needs a session id. "},
		"empty session":      {[]string{"--monitor", "--session", ""}, "heavy: --session needs a session id. "},
		"unknown option":     {[]string{"--monitor", "--nope"}, "heavy: --monitor takes only --session <id>, got --nope. "},
		"extra word":         {[]string{"--monitor", "--session", "s-1", "extra"}, "heavy: --monitor takes only --session <id>, got extra. "},
	}
	for name, example := range cases {
		t.Run(name, func(t *testing.T) {
			got := runHeavy(t, example.arguments...)

			if want := (outcome{exitStatus: 64, stderr: example.want + usage + "\n"}); got != want {
				t.Fatalf("heavy %q = %+v, want %+v", example.arguments, got, want)
			}
		})
	}
}

// G9: started by a process named claude, the monitor finds it as its Claude Code, subscribes for it once it is
// registered, prints the event of the session and ends with exit 0 on SIGTERM
func TestMonitorFindsItsClaudeCodeAndPrintsItsEvent(t *testing.T) {
	state := testkit.StateDir(t)
	listener := listenAsDaemon(t, state)
	claude := startUnderClaude(t)
	registration := sessions.Registration{SessionID: "s-1", Checkout: "/w/app", Time: time.Now().Unix()}
	if err := sessions.Write(state, claude.process.Process.Pid, registration); err != nil {
		t.Fatal(err)
	}

	link, subscription := acceptSubscription(t, listener)
	if want := (protocol.Subscribe{ClaudePID: claude.process.Process.Pid}); subscription != want {
		t.Fatalf("the monitor subscribed with %+v, want %+v", subscription, want)
	}
	answer(t, link, protocol.Message{Type: protocol.TypeSubscribe, Subscribe: &subscription})
	answer(t, link, protocol.Message{Type: protocol.TypeEvent, Event: &protocol.Event{Session: "s-1", Line: "heavy: your run ci (r-1) was paused"}})
	waitForOutput(t, claude.stdout, "heavy: your run ci (r-1) was paused\n")

	if err := syscall.Kill(claude.monitorPID(t), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if status := exitWithin(t, claude.process, 5*time.Second); status != 0 || claude.stderr.String() != "" {
		t.Fatalf("the monitor ended with exit %d and stderr %q, want exit 0 and nothing", status, claude.stderr.String())
	}
}

// with --session the monitor subscribes for that session
func TestMonitorWithASessionSubscribesForIt(t *testing.T) {
	state := testkit.StateDir(t)
	listener := listenAsDaemon(t, state)
	monitor, _, _ := startMonitor(t, "--session", "s-9")

	_, subscription := acceptSubscription(t, listener)

	if want := (protocol.Subscribe{Session: "s-9"}); subscription != want {
		t.Fatalf("the monitor subscribed with %+v, want %+v", subscription, want)
	}
	if err := monitor.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if status := exitWithin(t, monitor, 5*time.Second); status != 0 {
		t.Fatalf("the monitor ended with exit %d, want 0", status)
	}
}

// a monitor that cannot go on ends with exit 1 and its error on stderr
func TestMonitorWithABrokenRegistrationExitsWithItsError(t *testing.T) {
	state := testkit.StateDir(t)
	claude := startUnderClaude(t)
	path := sessions.Path(state, claude.process.Process.Pid)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"session_id":`), 0o600); err != nil {
		t.Fatal(err)
	}

	status := exitWithin(t, claude.process, 10*time.Second)

	// V29: the session hears why its monitor stopped, since stderr of a plugin monitor reaches nobody
	want := "heavy: reading the session registration " + path + ": unexpected EOF\n"
	if status != 1 || claude.stderr.String() != want || claude.stdout.String() != "heavy: the session monitor stopped: "+want {
		t.Fatalf("the monitor ended with exit %d, stdout %q, stderr %q; want exit 1 and %q on both", status, claude.stdout.String(), claude.stderr.String(), want)
	}
}

// a daemon that refuses this heavy in its hello ends the monitor with exit 1 and the refusal on stderr
func TestMonitorRefusedByTheDaemonExitsWithTheRefusal(t *testing.T) {
	state := testkit.StateDir(t)
	listener := listenAsDaemon(t, state)
	monitor, stdout, stderr := startMonitor(t, "--session", "s-9")

	link, _ := acceptHello(t, listener)
	answer(t, link, protocol.Hello{Protocol: protocol.Version, Error: "fake: this daemon takes no monitors"})
	link.Close()

	status := exitWithin(t, monitor, 5*time.Second)
	if status != 1 || stderr.String() != "fake: this daemon takes no monitors\n" || stdout.String() != "heavy: the session monitor stopped: fake: this daemon takes no monitors\n" {
		t.Fatalf("the monitor ended with exit %d, stdout %q, stderr %q; want exit 1 and the refusal on stderr", status, stdout.String(), stderr.String())
	}
}
