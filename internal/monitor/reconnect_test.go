package monitor_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// plan 5.1: after a daemon restart the monitor subscribes again at the new daemon and prints its next event
func TestMonitorSubscribesAgainAfterADaemonRestart(t *testing.T) {
	fixture := newFixture(t)
	fixture.register(t, claudePID, "s-1", "/w/app")
	first := startFakeDaemon(t, fixture.state)
	run := start(t, fixture.deps)
	first.subscribed(t, protocol.Subscribe{ClaudePID: claudePID})

	first.close()
	second := startFakeDaemon(t, fixture.state)
	link := second.subscribed(t, protocol.Subscribe{ClaudePID: claudePID})
	link.event(t, eventFor("s-1", "heavy: your run ci (r-1) was restarted at 20:45:00"))

	fixture.stdout.waitFor(t, "stdout", "heavy: your run ci (r-1) was restarted at 20:45:00\n")
	run.stop(t)
	if got, want := fixture.stderr.String(), "heavy: lost the daemon (EOF), subscribing again\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

// plan 5.1: an error message of the daemon is a line on stderr, and the monitor subscribes again
func TestMonitorRefusedByTheDaemonSubscribesAgain(t *testing.T) {
	fixture := newFixture(t)
	fixture.register(t, claudePID, "s-1", "/w/app")
	fake := startFakeDaemon(t, fixture.state)
	run := start(t, fixture.deps)
	refused := fake.next(t)
	refused.send(t, protocol.Message{Type: protocol.TypeError, Error: &protocol.Failure{Text: "fake: no subscriptions now"}})

	link := fake.subscribed(t, protocol.Subscribe{ClaudePID: claudePID})
	link.event(t, eventFor("s-1", "my run"))

	fixture.stdout.waitFor(t, "stdout", "my run\n")
	run.stop(t)
	want := "heavy: the daemon refused the subscription (fake: no subscriptions now), subscribing again\n"
	if got := fixture.stderr.String(); got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

// an error message after the subscription held is reported the same way
func TestMonitorGivenAnErrorAfterTheSubscriptionSubscribesAgain(t *testing.T) {
	fixture := newFixture(t)
	fixture.register(t, claudePID, "s-1", "/w/app")
	fake := startFakeDaemon(t, fixture.state)
	run := start(t, fixture.deps)
	first := fake.subscribed(t, protocol.Subscribe{ClaudePID: claudePID})
	first.send(t, protocol.Message{Type: protocol.TypeError, Error: &protocol.Failure{Text: "fake: going away"}})

	link := fake.subscribed(t, protocol.Subscribe{ClaudePID: claudePID})
	link.event(t, eventFor("s-1", "my run"))

	fixture.stdout.waitFor(t, "stdout", "my run\n")
	run.stop(t)
	want := "heavy: the daemon refused the subscription (fake: going away), subscribing again\n"
	if got := fixture.stderr.String(); got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

// a message of another type in place of the answer or an event is a line on stderr, and the monitor subscribes
// again
func TestMonitorGivenAnotherMessageSubscribesAgain(t *testing.T) {
	for name, wrong := range map[string]struct {
		subscribed bool
		want       string
	}{
		"in place of the answer": {want: "heavy: expected a subscribe message from the daemon, got a status message, subscribing again\n"},
		"in place of an event":   {subscribed: true, want: "heavy: expected an event message from the daemon, got a status message, subscribing again\n"},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newFixture(t)
			fixture.register(t, claudePID, "s-1", "/w/app")
			fake := startFakeDaemon(t, fixture.state)
			run := start(t, fixture.deps)
			first := fake.next(t)
			if wrong.subscribed {
				first.send(t, protocol.Message{Type: protocol.TypeSubscribe, Subscribe: &protocol.Subscribe{ClaudePID: claudePID}})
			}
			first.send(t, protocol.Message{Type: protocol.TypeStatus, Status: &protocol.Status{}})

			link := fake.subscribed(t, protocol.Subscribe{ClaudePID: claudePID})
			link.event(t, eventFor("s-1", "my run"))

			fixture.stdout.waitFor(t, "stdout", "my run\n")
			run.stop(t)
			if got := fixture.stderr.String(); got != wrong.want {
				t.Fatalf("stderr = %q, want %q", got, wrong.want)
			}
		})
	}
}

// a failed connect is a line on stderr, and the monitor tries again after its pause
func TestMonitorWhoseConnectFailsTriesAgain(t *testing.T) {
	fixture := newFixture(t)
	fixture.register(t, claudePID, "s-1", "/w/app")
	fake := startFakeDaemon(t, fixture.state)
	reach := fixture.deps.Connect
	calls := 0
	var mutex sync.Mutex
	fixture.deps.Connect = func() (*daemon.Connection, error) {
		mutex.Lock()
		calls++
		first := calls == 1
		mutex.Unlock()
		if first {
			return nil, errors.New("fake: no daemon answered")
		}
		return reach()
	}
	run := start(t, fixture.deps)

	link := fake.subscribed(t, protocol.Subscribe{ClaudePID: claudePID})
	link.event(t, eventFor("s-1", "my run"))

	fixture.stdout.waitFor(t, "stdout", "my run\n")
	run.stop(t)
	if got, want := fixture.stderr.String(), "heavy: reaching the daemon failed (fake: no daemon answered), subscribing again\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if calls != 2 {
		t.Fatalf("the monitor connected %d times, want 2", calls)
	}
}

// a daemon that refuses this heavy in its hello gives the same answer to every try: Run ends with the refusal
// instead of asking every second for the whole session
func TestMonitorRefusedInTheHelloEndsWithTheRefusal(t *testing.T) {
	for name, refusal := range map[string]struct {
		hello protocol.Hello
		want  string
	}{
		"error":          {protocol.Hello{Protocol: protocol.Version, Error: "fake: this daemon takes no monitors"}, "fake: this daemon takes no monitors"},
		"other protocol": {protocol.Hello{Protocol: protocol.Version + 1}, "heavy: the running daemon speaks protocol 2, this heavy 1; restart the session"},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newFixture(t)
			fixture.register(t, claudePID, "s-1", "/w/app")
			fake := startFakeDaemonWith(t, fixture.state, refusal.hello)

			err := start(t, fixture.deps).result(t)

			if err == nil || err.Error() != refusal.want {
				t.Fatalf("Run = %v, want %q", err, refusal.want)
			}
			if got := fixture.stderr.String() + fixture.stdout.String(); got != "" || fake.connections() != 1 {
				t.Fatalf("output %q after %d connections, want nothing after one", got, fake.connections())
			}
		})
	}
}
