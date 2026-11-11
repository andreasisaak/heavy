package daemon

import (
	"sort"
	"time"

	"github.com/andreasisaak/heavy/internal/foreign"
	"github.com/andreasisaak/heavy/internal/helper"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// foreignEvery is how often the daemon looks for foreign load while memory pressure lasts (design G7).
const foreignEvery = 10 * time.Second

// runContainers are the containers the runs started and the tagged processes of the runs, which are no foreign load.
type runContainers struct {
	owned  map[string]bool
	tagged []helper.Process
	err    error
}

// foreignLoad is what one sample under pressure learned about the containers outside the runs.
type foreignLoad struct {
	containers []foreign.Container
	usage      []helper.Container
	owned      map[string]bool
	err        error
}

// listForeign lists the containers with their compose labels and their memory in the VM without the runs' tagged
// processes, outside the state lock, since docker and the VM can take seconds.
func (s *server) listForeign(inRuns runContainers) foreignLoad {
	if inRuns.err != nil {
		return foreignLoad{err: inRuns.err}
	}
	containers, err := foreign.List(s.dockerProgram, s.dockerTimeout)
	if err != nil {
		return foreignLoad{err: err}
	}
	usage, err := s.vm.Containers()
	return foreignLoad{containers: containers, usage: foreign.Untagged(usage, inRuns.tagged), owned: inRuns.owned, err: err}
}

// lookAtForeign keeps the biggest containers outside the runs for the dashboard and, under pressure, tells the
// monitors of a checkout about its big ones (design G10, FR-28). git finds the checkouts, so it runs outside the
// state lock; only sample calls it. A failed listing is logged and changes nothing.
func (s *server) lookAtForeign(now time.Time, available int64, pressure bool, load foreignLoad) {
	if load.err != nil {
		foreign.LogUnattributed(s.logger, load.err)
		return
	}
	biggest := s.biggestOutside(load)
	var events []protocol.Event
	if pressure {
		events = s.foreign.Notices(now, available, load.usage, load.containers, load.owned, s.logger)
	}
	s.state.Lock()
	defer s.state.Unlock()
	s.biggest, s.biggestAt = biggest, now
	for _, event := range events {
		s.publish(event)
	}
}

// biggestShown is how many containers outside the runs the dashboard shows.
const biggestShown = 3

// biggestOutside are the biggest containers outside the runs, biggest first, with the main checkout of their
// compose project; a directory outside any checkout names none.
func (s *server) biggestOutside(load foreignLoad) []protocol.ForeignContainer {
	bytes := map[string]int64{}
	for _, container := range load.usage {
		bytes[container.ID] = container.RSSBytes
	}
	found := []protocol.ForeignContainer{}
	for _, container := range load.containers {
		if load.owned[container.ID] || bytes[container.ID] == 0 {
			continue
		}
		found = append(found, protocol.ForeignContainer{Name: container.Name, Checkout: s.checkoutOf(container.WorkingDirectory), Bytes: bytes[container.ID]})
	}
	sort.Slice(found, func(left, right int) bool { return found[left].Bytes > found[right].Bytes })
	return found[:min(len(found), biggestShown)]
}

// checkoutOf is the main checkout of a compose working directory, asked of git once per directory.
func (s *server) checkoutOf(directory string) string {
	if directory == "" {
		return ""
	}
	if checkout, known := s.checkouts[directory]; known {
		return checkout
	}
	checkout, err := foreign.MainCheckout(directory)
	if err != nil {
		s.logger.Warn("finding the checkout of a container failed", "working_directory", directory, "error", err.Error())
		return ""
	}
	s.checkouts[directory] = checkout
	return checkout
}
