// Package foreign tells the sessions of a checkout, while memory pressure lasts, when a container of that checkout
// that belongs to no heavy run uses a lot of memory, so the session can stop it (design G10, FR-28). heavy never
// stops such a container itself.
package foreign

import (
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/andreasisaak/heavy/internal/helper"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// Threshold is the resident memory from which a container outside the runs gets a notice.
const Threshold = int64(1) << 30

// Interval is the least time between two notices about one container.
const Interval = 15 * time.Minute

// Notifier remembers when it last considered each container, so a container gets at most one notice per Interval.
type Notifier struct {
	checkout func(directory string) (string, error)
	// considered holds, by container id, when a container last got a notice or a look for its checkout that found
	// none or failed; an entry leaves after Interval, whether its container still runs or not.
	considered map[string]time.Time
}

// NewNotifier takes the function that gives the main checkout of a compose working directory: "" for a directory in
// no checkout, an error when it cannot tell (MainCheckout in production).
func NewNotifier(checkout func(directory string) (string, error)) *Notifier {
	return &Notifier{checkout: checkout, considered: map[string]time.Time{}}
}

// Notices are the events for the containers that use at least Threshold of the VM's memory (usage, by id), belong to
// no run (owned, by id), carry a compose working directory in a checkout, and got no notice within the last Interval;
// ordered by container name, nil when there is none. The daemon calls it while memory pressure lasts.
func (n *Notifier) Notices(now time.Time, vmAvailableBytes int64, usage []helper.Container, containers []Container, owned map[string]bool, logger *slog.Logger) []protocol.Event {
	n.forget(now)
	resident := make(map[string]int64, len(usage))
	for _, container := range usage {
		resident[container.ID] = container.RSSBytes
	}
	var events []protocol.Event
	for _, container := range byName(containers) {
		rssBytes := resident[container.ID]
		if rssBytes < Threshold || owned[container.ID] || container.WorkingDirectory == "" {
			continue
		}
		if _, recent := n.considered[container.ID]; recent {
			continue
		}
		n.considered[container.ID] = now
		main, err := n.checkout(container.WorkingDirectory)
		if err != nil {
			logger.Warn("finding the checkout of a foreign container failed", "container", container.Name,
				"working_directory", container.WorkingDirectory, "error", err.Error())
			continue
		}
		if main == "" {
			logger.Info("a foreign container lies in no checkout, so no session hears of it", "container", container.Name,
				"working_directory", container.WorkingDirectory)
			continue
		}
		events = append(events, protocol.Event{Checkout: main, Line: noticeLine(vmAvailableBytes, container, rssBytes)})
	}
	return events
}

// forget drops the containers considered at least Interval ago: they may get a notice again, and those that are
// gone no longer take room.
func (n *Notifier) forget(now time.Time) {
	for id, considered := range n.considered {
		if now.Sub(considered) >= Interval {
			delete(n.considered, id)
		}
	}
}

// byName orders a copy of the containers by name.
func byName(containers []Container) []Container {
	ordered := slices.Clone(containers)
	slices.SortFunc(ordered, func(left, right Container) int { return strings.Compare(left.Name, right.Name) })
	return ordered
}
