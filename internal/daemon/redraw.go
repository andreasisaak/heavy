package daemon

import (
	"fmt"
	"strings"
	"time"
)

// redrawEvery is how often the sidebar is redrawn without a change, so its minutes keep counting (0.6: TICK_SECONDS).
const redrawEvery = 30 * time.Second

// redraw asks herdr to redraw the sidebar when the queue changed since the last redraw, or every 30 s, in the
// background and never twice at once, as notify_herdr of 0.6 did (V34). Called on every tick of serve.
func (s *server) redraw(now time.Time) {
	if s.redrawHerdr == nil || s.redrawing.Load() {
		return
	}
	s.state.Lock()
	var parts []string
	for _, run := range s.queue.snapshot(now) {
		parts = append(parts, fmt.Sprintf("%s %s %d", run.ID, run.State, run.Position))
	}
	s.state.Unlock()
	shown := strings.Join(parts, ",")
	if shown == s.redrawnFor && now.Sub(s.redrawnAt) < redrawEvery {
		return
	}
	s.redrawnFor, s.redrawnAt = shown, now
	s.redrawing.Store(true)
	go func() {
		defer s.redrawing.Store(false)
		err := s.redrawHerdr()
		text := ""
		if err != nil {
			text = err.Error()
		}
		if text != s.redrawError {
			s.redrawError = text
			if text != "" {
				s.logger.Warn("asking herdr to redraw the sidebar failed", "error", text)
			}
		}
	}()
}
