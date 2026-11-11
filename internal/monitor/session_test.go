package monitor_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/monitor"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/sessions"
)

// plan 5.1: the registered Claude Code PID two levels above the monitor names the session it subscribes for
func TestMonitorFindsTheRegisteredSessionTwoLevelsUp(t *testing.T) {
	fixture := newFixture(t)
	fixture.register(t, claudePID, "s-1", "/w/app")
	fake := startFakeDaemon(t, fixture.state)
	run := start(t, fixture.deps)

	link := fake.subscribed(t, protocol.Subscribe{ClaudePID: claudePID})
	link.event(t, eventFor("s-1", "heavy: your run ci (r-1) was resumed at 20:42:00"))

	fixture.stdout.waitFor(t, "stdout", "heavy: your run ci (r-1) was resumed at 20:42:00\n")
	run.stop(t)
	if got := fixture.stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want nothing", got)
	}
}

// G9: the registration may appear a moment after the monitor starts, so the monitor looks again until it does
func TestMonitorWaitsForARegistrationThatAppearsLater(t *testing.T) {
	fixture := newFixture(t)
	fixture.deps.RetryPause = 20 * time.Millisecond
	fake := startFakeDaemon(t, fixture.state)
	run := start(t, fixture.deps)
	time.Sleep(100 * time.Millisecond)
	fixture.register(t, claudePID, "s-1", "/w/app")

	fake.subscribed(t, protocol.Subscribe{ClaudePID: claudePID})

	run.stop(t)
}

// plan 5.1: without a registration among the ancestors after FindFor, Run ends with the error that names the PID
func TestMonitorWithoutARegisteredAncestorEndsWithTheError(t *testing.T) {
	fixture := newFixture(t)
	fixture.deps.FindFor = 150 * time.Millisecond
	fixture.deps.RetryPause = 20 * time.Millisecond
	asked := 0
	var mutex sync.Mutex
	fixture.deps.Process = func(pid int) (monitor.Process, error) {
		mutex.Lock()
		asked++
		mutex.Unlock()
		return processTree().process(pid)
	}
	began := time.Now()

	err := start(t, fixture.deps).result(t)

	want := "heavy: found no registered Claude Code session among the ancestors of pid 100"
	if err == nil || err.Error() != want {
		t.Fatalf("Run = %v, want %q", err, want)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if elapsed := time.Since(began); elapsed < 150*time.Millisecond || asked < 6 {
		t.Fatalf("Run gave up after %s and %d process lookups, want at least 150ms and two walks of three lookups", elapsed, asked)
	}
}

// the walk stops at PID 1: it is never taken for Claude Code, even under that name and with a registration
func TestMonitorDoesNotTakePIDOneForClaudeCode(t *testing.T) {
	fixture := newFixture(t)
	fixture.deps.FindFor = 50 * time.Millisecond
	fixture.deps.RetryPause = 10 * time.Millisecond
	fixture.deps.Process = tree{monitorPID: {Parent: 50, Name: "heavy"}, 50: {Parent: 1, Name: "sh"}, 1: {Name: "claude"}}.process
	fixture.register(t, 1, "s-launchd", "/")

	err := start(t, fixture.deps).result(t)

	want := "heavy: found no registered Claude Code session among the ancestors of pid 100"
	if err == nil || err.Error() != want {
		t.Fatalf("Run = %v, want %q", err, want)
	}
}

// a failed process lookup ends Run with its error at once instead of hiding it behind the search
func TestMonitorWhoseProcessLookupFailsEndsWithThatError(t *testing.T) {
	fixture := newFixture(t)
	fixture.deps.Process = tree{monitorPID: {Parent: 50, Name: "heavy"}}.process

	err := start(t, fixture.deps).result(t)

	if err == nil || err.Error() != "fake: no process 50" {
		t.Fatalf("Run = %v, want the error of the lookup of pid 50", err)
	}
}

// a registration that is not valid JSON is a real error, not a missing one
func TestMonitorWithABrokenRegistrationEndsWithTheReadError(t *testing.T) {
	fixture := newFixture(t)
	path := sessions.Path(fixture.state, claudePID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"session_id":`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := start(t, fixture.deps).result(t)

	want := "heavy: reading the session registration " + path + ": unexpected EOF"
	if err == nil || err.Error() != want {
		t.Fatalf("Run = %v, want %q", err, want)
	}
}

// a registration that cannot be read is a real error, not a missing one
func TestMonitorWithAnUnreadableRegistrationEndsWithTheReadError(t *testing.T) {
	fixture := newFixture(t)
	path := sessions.Path(fixture.state, claudePID)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}

	err := start(t, fixture.deps).result(t)

	want := "heavy: reading the session registration " + path + ": read " + path + ": is a directory"
	if err == nil || err.Error() != want {
		t.Fatalf("Run = %v, want %q", err, want)
	}
}

// the end of the context while the monitor still looks for its session ends Run without an error
func TestMonitorStoppedWhileLookingEndsWithoutAnError(t *testing.T) {
	fixture := newFixture(t)
	fixture.deps.RetryPause = 20 * time.Millisecond
	run := start(t, fixture.deps)
	time.Sleep(60 * time.Millisecond)
	select {
	case err := <-run.done:
		t.Fatalf("Run returned %v before its context ended, want it still looking for its session", err)
	default:
	}

	run.stop(t)

	if got := fixture.stderr.String() + fixture.stdout.String(); got != "" {
		t.Fatalf("output = %q, want nothing", got)
	}
}

// with --session the monitor looks up no parents and subscribes for that session
func TestMonitorWithASessionSubscribesForItWithoutParents(t *testing.T) {
	fixture := newFixture(t)
	fixture.deps.Session = "s-9"
	fixture.deps.Process = func(pid int) (monitor.Process, error) {
		t.Errorf("the monitor read process %d despite --session", pid)
		return monitor.Process{}, errors.New("fake: no processes with --session")
	}
	fake := startFakeDaemon(t, fixture.state)
	run := start(t, fixture.deps)

	fake.subscribed(t, protocol.Subscribe{Session: "s-9"})

	run.stop(t)
}
