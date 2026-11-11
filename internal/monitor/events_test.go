package monitor_test

import (
	"os"
	"testing"

	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/sessions"
)

// plan 5.1: the pause of a run of the session gives exactly the daemon's line, and a run of another session none
func TestMonitorPrintsThePauseOfItsRunAndNothingOfOthers(t *testing.T) {
	fixture := newFixture(t)
	fixture.register(t, claudePID, "s-1", "/w/app")
	fake := startFakeDaemon(t, fixture.state)
	run := start(t, fixture.deps)
	link := fake.subscribed(t, protocol.Subscribe{ClaudePID: claudePID})

	link.event(t, eventFor("s-other", "heavy: your run test (r-2) was paused at 20:41:05: VM 1.2 GiB free, host 8 % free"))
	link.event(t, eventFor("s-1", "heavy: your run ci (r-1) was paused at 20:41:07: VM 1.2 GiB free, host 8 % free"))

	fixture.stdout.waitFor(t, "stdout", "heavy: your run ci (r-1) was paused at 20:41:07: VM 1.2 GiB free, host 8 % free\n")
	run.stop(t)
	if got := fixture.stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want nothing", got)
	}
}

// plan 5.1: after /clear the registration names a new session; the next event of the new session is printed, one
// of the old session no longer
func TestMonitorFollowsTheSessionAfterClear(t *testing.T) {
	fixture := newFixture(t)
	fixture.register(t, claudePID, "s-old", "/w/app")
	fake := startFakeDaemon(t, fixture.state)
	run := start(t, fixture.deps)
	link := fake.subscribed(t, protocol.Subscribe{ClaudePID: claudePID})
	link.event(t, eventFor("s-old", "before clear"))
	fixture.stdout.waitFor(t, "stdout", "before clear\n")

	fixture.register(t, claudePID, "s-new", "/w/app")
	link.event(t, eventFor("s-old", "old session after clear"))
	link.event(t, eventFor("s-new", "new session after clear"))

	fixture.stdout.waitFor(t, "stdout", "before clear\nnew session after clear\n")
	run.stop(t)
}

// G10: a notice about a container names a checkout instead of a session: printed for the session's checkout only;
// a notice without session and checkout is for nobody
func TestMonitorPrintsForeignNoticesOfItsCheckoutOnly(t *testing.T) {
	fixture := newFixture(t)
	fixture.register(t, claudePID, "s-1", "/w/app")
	fake := startFakeDaemon(t, fixture.state)
	run := start(t, fixture.deps)
	link := fake.subscribed(t, protocol.Subscribe{ClaudePID: claudePID})

	link.event(t, protocol.Event{Checkout: "/w/other", Line: "other checkout"})
	link.event(t, protocol.Event{Line: "nobody"})
	link.event(t, protocol.Event{Checkout: "/w/app", Line: "heavy: memory is low (VM 1.4 GiB free); app-1 of this repository uses 1.5 GiB"})

	fixture.stdout.waitFor(t, "stdout", "heavy: memory is low (VM 1.4 GiB free); app-1 of this repository uses 1.5 GiB\n")
	run.stop(t)
}

// a session without a checkout gets no notice that names no checkout either
func TestMonitorWithoutACheckoutPrintsNoNoticeWithoutOne(t *testing.T) {
	fixture := newFixture(t)
	fixture.register(t, claudePID, "s-1", "")
	fake := startFakeDaemon(t, fixture.state)
	run := start(t, fixture.deps)
	link := fake.subscribed(t, protocol.Subscribe{ClaudePID: claudePID})

	link.event(t, protocol.Event{Line: "nobody"})
	link.event(t, eventFor("s-1", "mine"))

	fixture.stdout.waitFor(t, "stdout", "mine\n")
	run.stop(t)
}

// plan 5.1: with --session the monitor prints the events of that session and finds its checkout among the
// registrations
func TestMonitorWithASessionPrintsItsEventsAndItsCheckout(t *testing.T) {
	fixture := newFixture(t)
	fixture.deps.Session = "s-9"
	fixture.register(t, 30, "s-other", "/w/other")
	fixture.register(t, 31, "s-9", "/w/nine")
	fake := startFakeDaemon(t, fixture.state)
	run := start(t, fixture.deps)
	link := fake.subscribed(t, protocol.Subscribe{Session: "s-9"})

	link.event(t, eventFor("s-other", "other session"))
	link.event(t, protocol.Event{Checkout: "/w/other", Line: "other checkout"})
	link.event(t, eventFor("s-9", "my run"))
	link.event(t, protocol.Event{Checkout: "/w/nine", Line: "my checkout"})

	fixture.stdout.waitFor(t, "stdout", "my run\nmy checkout\n")
	run.stop(t)
}

// with --session and no registration of that session, only the events naming the session reach it
func TestMonitorWithAnUnregisteredSessionPrintsOnlyItsRuns(t *testing.T) {
	fixture := newFixture(t)
	fixture.deps.Session = "s-9"
	fake := startFakeDaemon(t, fixture.state)
	run := start(t, fixture.deps)
	link := fake.subscribed(t, protocol.Subscribe{Session: "s-9"})

	link.event(t, protocol.Event{Checkout: "/w/nine", Line: "a checkout"})
	link.event(t, eventFor("s-9", "my run"))

	fixture.stdout.waitFor(t, "stdout", "my run\n")
	run.stop(t)
}

// with --session a broken registration among the others is a real error
func TestMonitorWithASessionAndABrokenRegistrationEndsWithTheReadError(t *testing.T) {
	fixture := newFixture(t)
	fixture.deps.Session = "s-9"
	fixture.register(t, 31, "s-9", "/w/nine")
	path := sessions.Path(fixture.state, 32)
	if err := os.WriteFile(path, []byte(`{"session_id":`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := startFakeDaemon(t, fixture.state)
	run := start(t, fixture.deps)
	link := fake.subscribed(t, protocol.Subscribe{Session: "s-9"})

	link.event(t, eventFor("s-9", "my run"))

	want := "heavy: reading the session registration " + path + ": unexpected end of JSON input"
	if err := run.result(t); err == nil || err.Error() != want {
		t.Fatalf("Run = %v, want %q", err, want)
	}
	if got := fixture.stdout.String(); got != "" {
		t.Fatalf("stdout = %q, want nothing", got)
	}
}

// a registration that became unreadable after the subscription ends Run with the read error at the next event
func TestMonitorWhoseRegistrationBecameUnreadableEndsAtTheNextEvent(t *testing.T) {
	fixture := newFixture(t)
	fixture.register(t, claudePID, "s-1", "/w/app")
	fake := startFakeDaemon(t, fixture.state)
	run := start(t, fixture.deps)
	link := fake.subscribed(t, protocol.Subscribe{ClaudePID: claudePID})
	path := sessions.Path(fixture.state, claudePID)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	link.event(t, eventFor("s-1", "my run"))

	want := "heavy: reading the session registration " + path + ": read " + path + ": is a directory"
	if err := run.result(t); err == nil || err.Error() != want {
		t.Fatalf("Run = %v, want %q", err, want)
	}
	if got := fixture.stdout.String(); got != "" {
		t.Fatalf("stdout = %q, want nothing", got)
	}
}

// a failed write to stdout ends Run with that error: every stdout line is a notice for the model
func TestMonitorWhoseStdoutFailsEndsWithThatError(t *testing.T) {
	fixture := newFixture(t)
	fixture.register(t, claudePID, "s-1", "/w/app")
	stdout := &failingWriter{}
	fixture.deps.Stdout = stdout
	fake := startFakeDaemon(t, fixture.state)
	run := start(t, fixture.deps)
	link := fake.subscribed(t, protocol.Subscribe{ClaudePID: claudePID})

	link.event(t, eventFor("s-1", "my run"))

	want := "heavy: writing a notice to stdout: fake: stdout is closed"
	if err := run.result(t); err == nil || err.Error() != want {
		t.Fatalf("Run = %v, want %q", err, want)
	}
	stdout.mutex.Lock()
	defer stdout.mutex.Unlock()
	if stdout.attempts != 1 {
		t.Fatalf("the monitor wrote %d times to stdout, want 1", stdout.attempts)
	}
}
