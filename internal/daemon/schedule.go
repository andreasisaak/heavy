package daemon

import (
	"fmt"

	"github.com/andreasisaak/heavy/internal/platform"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/scheduler"
)

// decideLocked asks the scheduler what to do now and carries it out: the state changes at once, the work on the
// processes follows in order outside the lock. While runs of a gone daemon may still announce themselves again,
// nothing happens. The caller holds state.
func (s *server) decideLocked() {
	if s.clock.Now().Before(s.adoptUntil) {
		return
	}
	measurements := s.machine.Measurements(s.load)
	if measurements.Cores < 1 {
		return
	}
	measurements.OtherActive = s.otherActive()
	state := s.queue.schedulerState(s.clock.Now())
	state.LastResume, state.LastRestart = s.lastResume, s.lastRestart
	for _, action := range scheduler.Decide(state, measurements, scheduler.DefaultConfig(s.load), s.clock.Now()) {
		current := s.queue.find(action.ID)
		switch action.Kind {
		case scheduler.Start:
			if s.takeSlot(current) {
				s.start(action)
			}
		case scheduler.Pause:
			s.pauseForMemory(current, measurements)
		case scheduler.Resume:
			s.resumeFromMemory(current, measurements)
		case scheduler.Terminate:
			s.restart(current, measurements)
		case scheduler.Requeue:
			current.requeueOrder = action.Order
		}
	}
}

func (s *server) start(action scheduler.Action) {
	current := s.queue.find(action.ID)
	now := s.clock.Now()
	current.status, current.started, current.cores = scheduler.Running, now, action.Cores
	start := protocol.Start{ID: current.id, Cores: action.Cores, Since: now.Unix()}
	if err := current.link.send(protocol.Message{Type: protocol.TypeStart, Start: &start}); err != nil {
		s.logger.Warn("starting a run failed, its client went away", "id", current.id, "error", err.Error())
		current.link.connection.Close()
		return
	}
	s.logger.Info("run started", "id", current.id, "label", current.register.Label, "cores", action.Cores)
	s.announce(current, fmt.Sprintf("started at %s with %d cores", now.Format("15:04:05"), action.Cores))
}

// pauseForMemory pauses the run under memory pressure (design G5).
func (s *server) pauseForMemory(current *run, measurements scheduler.Measurements) {
	now := s.clock.Now()
	current.status, current.pausedReason, current.pausedAt = scheduler.Paused, scheduler.PausedForMemory, now
	at, memory := now.Format("15:04:05"), memoryNote(measurements)
	s.tell(current, fmt.Sprintf("heavy: paused at %s (%s)", at, memory))
	s.announce(current, fmt.Sprintf("was paused at %s: %s", at, memory))
	s.logger.Info("run paused for memory", "id", current.id, "vm_available_bytes", measurements.VMAvailableBytes,
		"host_free_percent", measurements.HostFreePercent, "pressure_for", measurements.PressureFor.String())
	tracker, target := s.tracker(), processRun(current)
	s.jobs <- func() { s.report(current, "pause", tracker.Pause(target)) }
}

// resumeFromMemory resumes a run paused for memory once memory has recovered.
func (s *server) resumeFromMemory(current *run, measurements scheduler.Measurements) {
	now := s.clock.Now()
	current.pausedTotal += now.Sub(current.pausedAt)
	current.status, current.pausedReason, current.pausedAt = scheduler.Running, scheduler.NotPaused, now
	s.lastResume = now
	s.tell(current, "heavy: resumed at "+now.Format("15:04:05"))
	s.announce(current, fmt.Sprintf("was resumed at %s: %s", now.Format("15:04:05"), memoryNote(measurements)))
	s.logger.Info("run resumed after memory recovered", "id", current.id)
	tracker, target := s.tracker(), processRun(current)
	s.jobs <- func() { s.report(current, "resume", tracker.Resume(target)) }
}

// restart ends the run's processes under lasting pressure; its client starts the command again once the run is
// admitted again, from #1 (design G5).
func (s *server) restart(current *run, measurements scheduler.Measurements) {
	now := s.clock.Now()
	current.restarting, s.lastRestart = true, now
	s.tell(current, fmt.Sprintf("heavy: restarting at %s (memory), back to #1", now.Format("15:04:05")))
	s.announce(current, fmt.Sprintf("was ended at %s to start again as the next run: %s", now.Format("15:04:05"), memoryNote(measurements)))
	if err := current.link.send(protocol.Message{Type: protocol.TypeRestart, Restart: &protocol.Target{ID: current.id}}); err != nil {
		s.logger.Warn("telling a run of its restart failed", "id", current.id, "error", err.Error())
	}
	s.logger.Info("run restarts under lasting memory pressure", "id", current.id, "restarts", current.restarts+1)
	tracker, target := s.tracker(), processRun(current)
	s.jobs <- func() {
		_, err := tracker.Terminate(target)
		s.report(current, "restart", err)
	}
}

// memoryNote is the free memory where docker runs and of the host, as the pause lines name it.
func memoryNote(measurements scheduler.Measurements) string {
	vm := platform.Docker + " not measured"
	if measurements.VMMeasured {
		vm = fmt.Sprintf("%s %.1f GiB free", platform.Docker, float64(measurements.VMAvailableBytes)/(1<<30))
	}
	return fmt.Sprintf("%s, host %d %% free", vm, measurements.HostFreePercent)
}

// announce tells the monitors of the run's session what happened to it (FR-27): its start, a pause, a resume, a
// restart, its first red line and its end, so the session needs no watch of its own on the run's output.
func (s *server) announce(current *run, what string) {
	s.publish(protocol.Event{Session: current.register.Session, Line: fmt.Sprintf("heavy: your run %s (%s) %s", current.register.Label, current.id, what)})
}
