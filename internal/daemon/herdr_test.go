package daemon_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// V34 (as notify_herdr in 0.6): the daemon asks the herdr plugin to redraw the sidebar when the queue changes and
// every 30 s, so the minutes keep counting; without a change and within 30 s it asks nothing
func TestDaemonAsksHerdrToRedrawOnChangesAndEveryThirtySeconds(t *testing.T) {
	var calls atomic.Int32
	current := startDaemonWith(t, protocol.Identity{}, func(options *daemon.Options) {
		options.RedrawHerdr = func() error { calls.Add(1); return nil }
	})
	current.clock.advance(t, time.Second)
	waitFor(t, func() bool { return calls.Load() == 1 }, "the first redraw")

	current.clock.advance(t, time.Second)
	current.clock.advance(t, time.Second)
	afterQuiet := calls.Load()
	register(t, current.state)
	current.clock.advance(t, time.Second)
	waitFor(t, func() bool { return calls.Load() == 2 }, "the redraw after a new run")
	for step := 0; step < 28; step++ {
		current.clock.advance(t, time.Second)
	}
	beforeThirty := calls.Load()
	current.clock.advance(t, time.Second)
	current.clock.advance(t, time.Second)
	waitFor(t, func() bool { return calls.Load() == 3 }, "the redraw after 30 s")

	if afterQuiet != 1 || beforeThirty != 2 {
		t.Fatalf("redraws after quiet seconds %d, before 30 s %d; want 1 and 2", afterQuiet, beforeThirty)
	}
}
