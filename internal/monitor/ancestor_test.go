package monitor_test

import (
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/monitor"
	"github.com/andreasisaak/heavy/internal/protocol"
)

const noRegisteredAncestor = "heavy: found no registered Claude Code session among the ancestors of pid 100"

// noConnectionYet gives the monitor a few searches and checks that it did not connect to the daemon.
func noConnectionYet(t *testing.T, fake *fakeDaemon) {
	t.Helper()
	time.Sleep(150 * time.Millisecond)
	if got := fake.connections(); got != 0 {
		t.Fatalf("the monitor connected %d times before its Claude Code process was registered, want 0", got)
	}
}

// registrations are never deleted: the PID of a long dead Claude Code may now belong to a shell above the monitor;
// the monitor skips it for the nearest process running claude, or node for an npm install
func TestMonitorSkipsARegisteredShellForTheNearestClaudeCode(t *testing.T) {
	for _, name := range []string{"claude", "node"} {
		t.Run(name, func(t *testing.T) {
			fixture := newFixture(t)
			processes := processTree()
			processes[claudePID] = monitor.Process{Parent: 1, Name: name, Started: claudeStart}
			fixture.deps.Process = processes.process
			fixture.registerAt(t, 50, "s-dead", "/w/old", claudeStart.Add(-time.Hour))
			fixture.register(t, claudePID, "s-1", "/w/app")
			fake := startFakeDaemon(t, fixture.state)
			run := start(t, fixture.deps)

			fake.subscribed(t, protocol.Subscribe{ClaudePID: claudePID})

			run.stop(t)
		})
	}
}

// a Claude Code started from the Bash tool of another one: the monitor waits for the registration of the nearest
// and never takes the outer one
func TestMonitorInsideAnotherClaudeCodeWaitsForTheNearest(t *testing.T) {
	fixture := newFixture(t)
	fixture.deps.RetryPause = 20 * time.Millisecond
	processes := processTree()
	processes[claudePID] = monitor.Process{Parent: 60, Name: "claude", Started: claudeStart}
	processes[60] = monitor.Process{Parent: 10, Name: "zsh", Started: claudeStart.Add(-time.Minute)}
	processes[10] = monitor.Process{Parent: 1, Name: "claude", Started: claudeStart.Add(-time.Hour)}
	fixture.deps.Process = processes.process
	fixture.register(t, 10, "s-outer", "/w/outer")
	fake := startFakeDaemon(t, fixture.state)
	run := start(t, fixture.deps)
	noConnectionYet(t, fake)

	fixture.register(t, claudePID, "s-inner", "/w/inner")

	fake.subscribed(t, protocol.Subscribe{ClaudePID: claudePID})
	run.stop(t)
}

// a registration older than the start of its Claude Code process belongs to an earlier process of that PID: the
// monitor waits until the SessionStart hook writes it again
func TestMonitorWaitsForARegistrationNewerThanItsClaudeCode(t *testing.T) {
	fixture := newFixture(t)
	fixture.deps.RetryPause = 20 * time.Millisecond
	fixture.registerAt(t, claudePID, "s-dead", "/w/old", claudeStart.Add(-time.Hour))
	fake := startFakeDaemon(t, fixture.state)
	run := start(t, fixture.deps)
	noConnectionYet(t, fake)

	fixture.register(t, claudePID, "s-1", "/w/app")
	link := fake.subscribed(t, protocol.Subscribe{ClaudePID: claudePID})
	link.event(t, eventFor("s-dead", "old session"))
	link.event(t, eventFor("s-1", "my run"))

	fixture.stdout.waitFor(t, "stdout", "my run\n")
	run.stop(t)
}

// the registration holds whole seconds and the start microseconds: one second before the start still counts,
// two seconds do not
func TestMonitorTakesARegistrationWithinASecondOfTheStart(t *testing.T) {
	started := claudeStart.Add(500 * time.Millisecond)
	t.Run("one second before", func(t *testing.T) {
		fixture := newFixture(t)
		processes := processTree()
		processes[claudePID] = monitor.Process{Parent: 1, Name: "claude", Started: started}
		fixture.deps.Process = processes.process
		fixture.registerAt(t, claudePID, "s-1", "/w/app", claudeStart.Add(-time.Second))
		fake := startFakeDaemon(t, fixture.state)
		run := start(t, fixture.deps)

		fake.subscribed(t, protocol.Subscribe{ClaudePID: claudePID})

		run.stop(t)
	})
	t.Run("two seconds before", func(t *testing.T) {
		fixture := newFixture(t)
		fixture.deps.FindFor = 100 * time.Millisecond
		fixture.deps.RetryPause = 20 * time.Millisecond
		processes := processTree()
		processes[claudePID] = monitor.Process{Parent: 1, Name: "claude", Started: started}
		fixture.deps.Process = processes.process
		fixture.registerAt(t, claudePID, "s-1", "/w/app", claudeStart.Add(-2*time.Second))

		err := start(t, fixture.deps).result(t)

		if err == nil || err.Error() != noRegisteredAncestor {
			t.Fatalf("Run = %v, want %q", err, noRegisteredAncestor)
		}
	})
}

// without a process running claude or node among the ancestors, registrations of other ancestors do not count
func TestMonitorWithoutAClaudeCodeAncestorEndsWithTheError(t *testing.T) {
	fixture := newFixture(t)
	fixture.deps.FindFor = 100 * time.Millisecond
	fixture.deps.RetryPause = 20 * time.Millisecond
	processes := processTree()
	processes[claudePID] = monitor.Process{Parent: 1, Name: "zsh", Started: claudeStart}
	fixture.deps.Process = processes.process
	fixture.register(t, 50, "s-shell", "/w/shell")
	fixture.register(t, claudePID, "s-zsh", "/w/zsh")

	err := start(t, fixture.deps).result(t)

	if err == nil || err.Error() != noRegisteredAncestor {
		t.Fatalf("Run = %v, want %q", err, noRegisteredAncestor)
	}
}
