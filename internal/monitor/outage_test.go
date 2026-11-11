package monitor_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// stepClock is a clock that moves only when the test advances it.
type stepClock struct {
	mutex sync.Mutex
	now   time.Time
}

func (c *stepClock) Now() time.Time {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.now
}

func (c *stepClock) advance(length time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.now = c.now.Add(length)
}

// waitForPart waits until the output holds part.
func (o *output) waitForPart(t *testing.T, name, part string) {
	t.Helper()
	deadline := time.Now().Add(patience)
	for !strings.Contains(o.String(), part) {
		if time.Now().After(deadline) {
			t.Fatalf("%s = %q after %s, want it to hold %q", name, o.String(), patience, part)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// user 2026-11-09: the session hears when the daemon stays away for a minute and when it answers again; a short
// loss, such as a handover to a newer heavy, stays on stderr
func TestMonitorTellsTheSessionWhenTheDaemonStaysAwayAndWhenItAnswersAgain(t *testing.T) {
	fixture := newFixture(t)
	fixture.register(t, claudePID, "s-1", "/w/app")
	clock := &stepClock{now: time.Date(2026, 11, 9, 0, 3, 11, 0, time.Local)}
	fixture.deps.Now, fixture.deps.OutageNotice = clock.Now, time.Minute
	run := start(t, fixture.deps)
	fixture.stderr.waitForPart(t, "stderr", "heavy: reaching the daemon failed")
	if got := fixture.stdout.String(); got != "" {
		t.Fatalf("stdout before a minute = %q, want nothing", got)
	}

	clock.advance(time.Minute)
	away := "heavy: the queue daemon is unreachable since 00:03:11 (reaching the daemon failed (heavy: starting the daemon " +
		"through /nonexistent/heavy --daemon --detach failed: fork/exec /nonexistent/heavy: no such file or directory)); " +
		"your runs keep running and heavy keeps trying\n"
	fixture.stdout.waitFor(t, "stdout", away)

	clock.advance(30 * time.Second)
	fake := startFakeDaemon(t, fixture.state)
	link := fake.subscribed(t, protocol.Subscribe{ClaudePID: claudePID})
	link.event(t, eventFor("s-1", "my run"))

	fixture.stdout.waitFor(t, "stdout", away+"heavy: the queue daemon answers again at 00:04:41\nmy run\n")
	run.stop(t)
}
