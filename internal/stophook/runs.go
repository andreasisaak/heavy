package stophook

import (
	"strings"
	"time"

	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/statusview"
)

const (
	costWaiting = "Stopping it gives up its place in the queue, and a new start waits at the end."
	costRunning = "Stopping it throws away the minutes it has run and makes room for the next run."
)

// cost is what stopping the runs throws away: the minutes of one that has started, else the places in the queue.
func cost(runs []protocol.RunStatus) string {
	for _, run := range runs {
		if run.State != "waiting" {
			return costRunning
		}
	}
	return costWaiting
}

func describeAll(runs []protocol.RunStatus, now time.Time) string {
	descriptions := make([]string, len(runs))
	for index, run := range runs {
		descriptions[index] = statusview.Describe(run, now)
	}
	return strings.Join(descriptions, "; ")
}
