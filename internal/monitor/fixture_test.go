package monitor_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/monitor"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/sessions"
	"github.com/andreasisaak/heavy/internal/testkit"
)

// output collects what the monitor writes, safe for the monitor's goroutine and the test.
type output struct {
	mutex sync.Mutex
	text  strings.Builder
}

func (o *output) Write(data []byte) (int, error) {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	return o.text.Write(data)
}

func (o *output) String() string {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	return o.text.String()
}

// waitFor waits until the output is exactly want.
func (o *output) waitFor(t *testing.T, name, want string) {
	t.Helper()
	deadline := time.Now().Add(patience)
	for o.String() != want {
		if time.Now().After(deadline) {
			t.Fatalf("%s = %q after %s, want %q", name, o.String(), patience, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// failingWriter refuses every write and counts the attempts.
type failingWriter struct {
	mutex    sync.Mutex
	attempts int
}

func (w *failingWriter) Write([]byte) (int, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.attempts++
	return 0, fmt.Errorf("fake: stdout is closed")
}

// tree is a fake process table.
type tree map[int]monitor.Process

func (t tree) process(pid int) (monitor.Process, error) {
	process, found := t[pid]
	if !found {
		return monitor.Process{}, fmt.Errorf("fake: no process %d", pid)
	}
	return process, nil
}

// monitorPID is the PID of the monitor in the fake process tree: its shell is 50, Claude Code is 20.
const (
	monitorPID = 100
	claudePID  = 20
)

var (
	// claudeStart is the start of Claude Code in the fake process tree.
	claudeStart = time.Unix(1794139000, 0)
	// registered is the time of the fixture's registrations, after the start of Claude Code.
	registered = claudeStart.Add(100 * time.Second)
)

func processTree() tree {
	return tree{
		monitorPID: {Parent: 50, Name: "heavy", Started: registered},
		50:         {Parent: claudePID, Name: "sh", Started: registered},
		claudePID:  {Parent: 1, Name: "claude", Started: claudeStart},
	}
}

// fixture is a state directory with the monitor's stdout and stderr, and dependencies that reach the fake daemon
// of that directory.
type fixture struct {
	state  string
	stdout *output
	stderr *output
	deps   monitor.Dependencies
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	state := testkit.StateDir(t)
	stdout, stderr := &output{}, &output{}
	connect := func() (*daemon.Connection, error) {
		return daemon.Connect(daemon.ConnectOptions{StateDirectory: state, Binary: "/nonexistent/heavy"})
	}
	return &fixture{state: state, stdout: stdout, stderr: stderr, deps: monitor.Dependencies{
		StateDirectory: state, Connect: connect, Process: processTree().process, PID: monitorPID,
		Stdout: stdout, Stderr: stderr, FindFor: patience, RetryPause: 200 * time.Millisecond,
		Now: time.Now, OutageNotice: time.Minute,
	}}
}

func (f *fixture) register(t *testing.T, pid int, session, checkout string) {
	t.Helper()
	f.registerAt(t, pid, session, checkout, registered)
}

func (f *fixture) registerAt(t *testing.T, pid int, session, checkout string, at time.Time) {
	t.Helper()
	registration := sessions.Registration{SessionID: session, Cwd: checkout, Checkout: checkout, Time: at.Unix()}
	if err := sessions.Write(f.state, pid, registration); err != nil {
		t.Fatal(err)
	}
}

// running is a monitor started in the background.
type running struct {
	cancel context.CancelFunc
	done   chan error
}

func start(t *testing.T, deps monitor.Dependencies) *running {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	run := &running{cancel: cancel, done: make(chan error, 1)}
	go func() { run.done <- monitor.Run(ctx, deps) }()
	t.Cleanup(cancel)
	return run
}

// result waits for Run to return on its own.
func (r *running) result(t *testing.T) error {
	t.Helper()
	select {
	case err := <-r.done:
		return err
	case <-time.After(patience):
		t.Fatalf("Run did not return within %s", patience)
		return nil
	}
}

// stop ends the monitor's context; Run must return nil.
func (r *running) stop(t *testing.T) {
	t.Helper()
	r.cancel()
	if err := r.result(t); err != nil {
		t.Fatalf("Run after the end of its context = %v, want nil", err)
	}
}

// eventFor is an event about a run of the session.
func eventFor(session, line string) protocol.Event {
	return protocol.Event{Session: session, Line: line}
}
