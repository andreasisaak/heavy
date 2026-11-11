package daemon

import (
	"fmt"

	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/scheduler"
)

// nameHoldsLocked writes into the status of each waiting run what keeps it from starting now: what the daemon
// itself waits for, else the reason of the scheduler, else the machine slots. The caller holds state.
func (s *server) nameHoldsLocked(runs []protocol.RunStatus) {
	now := s.clock.Now()
	state := s.queue.schedulerState(now)
	state.LastResume, state.LastRestart = s.lastResume, s.lastRestart
	scheduled := map[string]bool{}
	for _, run := range state.Runs {
		scheduled[run.ID] = true
	}
	measurements := s.machine.Measurements(s.load)
	measurements.OtherActive = s.otherActive()
	all, holds := "", map[string]string{}
	switch {
	case now.Before(s.adoptUntil):
		all = "the daemon just started and hears of the runs of the last one first"
	case measurements.Cores < 1:
		all = "the daemon has no measurement of the machine yet"
	default:
		holds = scheduler.Holds(state, measurements, scheduler.DefaultConfig(s.load), now)
	}
	for index, run := range runs {
		if run.State != "waiting" {
			continue
		}
		current := s.queue.find(run.ID)
		switch {
		case all != "":
			runs[index].Waits = all
		case !scheduled[run.ID]:
			runs[index].Waits = "restarted: starts once the processes of its last start are gone"
		case holds[run.ID] != "":
			runs[index].Waits = holds[run.ID]
		case s.limited && current != nil && current.slotTold:
			runs[index].Waits = fmt.Sprintf("machine slots: the runs of this machine hold all %d (%s)", s.limit.MaxParallel, s.limit.Folder)
		}
	}
}
