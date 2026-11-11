package scheduler

import (
	"fmt"
	"time"
)

// Decide returns what the daemon does now. Under memory pressure it escalates one step: it ends a run with
// restart once pressure outlasts the last pause, else it pauses the youngest running run; it admits a run only when
// none is active, since waiting frees nothing then. Without
// pressure it resumes one run paused for memory once memory has recovered, else it starts at most one waiting run.
// One action per call lets the next measurement show its effect first.
func Decide(state State, m Measurements, cfg Config, now time.Time) []Action {
	if m.Cores < 1 {
		panic(fmt.Sprintf("scheduler: measurements with %d cores", m.Cores))
	}
	if m.PressureFor >= cfg.PressureHold {
		if actions := restart(state, m, cfg, now); actions != nil {
			return actions
		}
		if action, found := pause(state, cfg, now); found {
			return []Action{action}
		}
		if idle(state) {
			if action, found := admit(state, m, cfg, now); found {
				return []Action{action}
			}
		}
		return nil
	}
	if action, found := resume(state, m, cfg, now); found {
		return []Action{action}
	}
	if action, found := admit(state, m, cfg, now); found {
		return []Action{action}
	}
	return nil
}

// restart ends the youngest run with restart that runs or is paused for memory, and queues it as #1, ahead of the
// first waiting run (or with its own order less one when none waits), once pressure has held without a break since
// the last memory pause, for at least RestartAfter, and the last restart is RestartAfter ago. A run the user paused
// stays as it is.
func restart(state State, m Measurements, cfg Config, now time.Time) []Action {
	lastPause, paused := lastMemoryPause(state)
	if !paused || now.Sub(lastPause) < cfg.RestartAfter || m.PressureFor < now.Sub(lastPause) {
		return nil
	}
	if !state.LastRestart.IsZero() && now.Sub(state.LastRestart) < cfg.RestartAfter {
		return nil
	}
	candidate, found := youngest(state, func(run Run) bool {
		return run.Restart && (run.Status == Running || pausedForMemory(run))
	})
	if !found {
		return nil
	}
	order, waits := candidate.Order, false
	for _, run := range state.Runs {
		if run.Status == Waiting && (!waits || run.Order < order) {
			order, waits = run.Order, true
		}
	}
	return []Action{{Kind: Terminate, ID: candidate.ID}, {Kind: Requeue, ID: candidate.ID, Order: order - 1}}
}

// pause stops the youngest running run while another keeps running, at least PressureHold after the last memory pause.
func pause(state State, cfg Config, now time.Time) (Action, bool) {
	running := 0
	for _, run := range state.Runs {
		if run.Status == Running {
			running++
		}
	}
	if running < 2 {
		return Action{}, false
	}
	if lastPause, paused := lastMemoryPause(state); paused && now.Sub(lastPause) < cfg.PressureHold {
		return Action{}, false
	}
	candidate, _ := youngest(state, func(run Run) bool { return run.Status == Running })
	return Action{Kind: Pause, ID: candidate.ID, Reason: PausedForMemory}, true
}

// resume continues the run paused last for memory, once memory has recovered for RecoveryHold and the last resume
// is RecoveryHold ago. Runs the user paused stay paused.
func resume(state State, m Measurements, cfg Config, now time.Time) (Action, bool) {
	if m.RecoveredFor < cfg.RecoveryHold {
		return Action{}, false
	}
	if !state.LastResume.IsZero() && now.Sub(state.LastResume) < cfg.RecoveryHold {
		return Action{}, false
	}
	var candidate Run
	found := false
	for _, run := range state.Runs {
		if pausedForMemory(run) && (!found || run.PausedAt.After(candidate.PausedAt)) {
			candidate, found = run, true
		}
	}
	if !found {
		return Action{}, false
	}
	return Action{Kind: Resume, ID: candidate.ID}, true
}

func pausedForMemory(run Run) bool {
	return run.Status == Paused && run.PausedReason == PausedForMemory
}

// lastMemoryPause is the latest pause time of the runs paused for memory.
func lastMemoryPause(state State) (time.Time, bool) {
	var last time.Time
	found := false
	for _, run := range state.Runs {
		if pausedForMemory(run) && (!found || run.PausedAt.After(last)) {
			last, found = run.PausedAt, true
		}
	}
	return last, found
}

// youngest is the run with the latest start among those that match.
func youngest(state State, matches func(Run) bool) (Run, bool) {
	var candidate Run
	found := false
	for _, run := range state.Runs {
		if matches(run) && (!found || run.Started.After(candidate.Started)) {
			candidate, found = run, true
		}
	}
	return candidate, found
}

// idle reports that no run is active.
func idle(state State) bool {
	for _, run := range state.Runs {
		if run.Status != Waiting {
			return false
		}
	}
	return true
}
