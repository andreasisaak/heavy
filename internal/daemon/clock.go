package daemon

import "time"

// Clock gives the daemon its time and a tick every second, so tests can move both.
type Clock interface {
	Now() time.Time
	Ticks() <-chan time.Time
}

// SystemClock is the clock of the machine.
func SystemClock() Clock {
	return systemClock{ticker: time.NewTicker(time.Second)}
}

type systemClock struct {
	ticker *time.Ticker
}

func (c systemClock) Now() time.Time { return time.Now() }

func (c systemClock) Ticks() <-chan time.Time { return c.ticker.C }
