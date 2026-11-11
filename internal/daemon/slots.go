package daemon

import (
	"fmt"

	"github.com/andreasisaak/heavy/internal/scheduler"
	"github.com/andreasisaak/heavy/internal/slots"
)

// readMachine reads the machine configuration, outside the state lock; without a path there is none.
func (s *server) readMachine() (slots.Config, bool, error) {
	if s.machineConfig == "" {
		return slots.Config{}, false, nil
	}
	return slots.Load(s.machineConfig)
}

// limitLocked takes what readMachine read and sets the thresholds in force: under a machine configuration its
// limit stands in for the default of the personal list, and caps a max_parallel the list names. A broken
// configuration keeps the last good one and is logged once. The caller holds state.
func (s *server) limitLocked(limit slots.Config, limited bool, err error) {
	switch {
	case err == nil:
		if limit != s.limit || limited != s.limited {
			s.logger.Info("the machine limit changed", "limited", limited, "max_parallel", limit.MaxParallel, "slots", limit.Folder)
		}
		s.limit, s.limited, s.limitError = limit, limited, ""
	case err.Error() != s.limitError:
		s.limitError = err.Error()
		s.logger.Error("reading the machine configuration failed, the last one stays", "error", s.limitError)
	}
	s.load = s.personalLoad
	if !s.limited {
		return
	}
	if !s.load.MaxParallelSet || s.load.MaxParallel > s.limit.MaxParallel {
		s.load.MaxParallel = s.limit.MaxParallel
	}
}

// otherActive counts the runs the daemons of other users have active, by the machine slots they hold; a failure
// is logged and counts none. The caller holds state.
func (s *server) otherActive() int {
	if !s.limited {
		return 0
	}
	count, err := s.slots.Others(s.limit)
	if err != nil {
		s.logger.Warn("counting the machine slots of other users failed", "error", err.Error())
		return 0
	}
	return count
}

// takeSlot takes a machine slot for a run about to start and reports whether the run may start. While the daemons
// of the machine hold every slot the run keeps waiting, and its client hears so once. When the slots cannot be
// used, such as without their folder, the run starts without one and its client hears why: a machine that starts
// nothing helps nobody, and the load thresholds still hold. The caller holds state.
func (s *server) takeSlot(current *run) bool {
	if !s.limited {
		return true
	}
	slot, taken, err := s.slots.Acquire(s.limit)
	switch {
	case err != nil:
		s.logger.Error("taking a machine slot failed, the run starts without one", "id", current.id, "error", err.Error())
		s.tellSlot(current, err.Error())
		return true
	case !taken:
		s.tellSlot(current, fmt.Sprintf("heavy: waits for one of the %d machine slots (%s)", s.limit.MaxParallel, s.limit.Folder))
		return false
	}
	current.slot = slot
	return true
}

// tellSlot tells the client of a run about its machine slot, once until the run has run.
func (s *server) tellSlot(current *run, line string) {
	if current.slotTold {
		return
	}
	current.slotTold = true
	s.tell(current, line)
}

// adoptSlot takes a machine slot for a run that runs already, announced again after a daemon ended; without a free
// one it keeps running beyond the limit, which the log says. The caller holds state.
func (s *server) adoptSlot(current *run) {
	if !s.limited {
		return
	}
	slot, taken, err := s.slots.Acquire(s.limit)
	if err != nil || !taken {
		s.logger.Warn("a run announced again runs without a machine slot", "id", current.id, "taken", taken, "error", fmt.Sprint(err))
		return
	}
	current.slot = slot
}

// claimSlots takes a machine slot for each active run that has none: a run that started while the slots could
// not be used, or before the machine had a configuration. So the daemons of other users count it from then on.
// A failure stays silent here; the client of the run heard of it at the start. The caller holds state.
func (s *server) claimSlots() {
	if !s.limited {
		return
	}
	for _, current := range s.queue.runs {
		if current.status == scheduler.Waiting || current.slot != 0 {
			continue
		}
		slot, taken, err := s.slots.Acquire(s.limit)
		if err != nil || !taken {
			return
		}
		current.slot = slot
	}
}

// releaseSlot gives the machine slot of a run back, when its command ended. The caller holds state.
func (s *server) releaseSlot(current *run) {
	if current.slot != 0 {
		s.slots.Release(current.slot)
	}
	current.slot, current.slotTold = 0, false
}
