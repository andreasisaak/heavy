package scheduler

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Holds names for each waiting run what keeps it from starting now, in the words `heavy --status` shows: the
// memory with its numbers, the load, the limit of runs, the same worktree. A run that Decide starts now has no
// entry. Holds follows the rules of Decide and admit step by step, and a test holds the two together.
func Holds(state State, m Measurements, cfg Config, now time.Time) map[string]string {
	var active, waiting []Run
	checkouts := map[string]string{}
	for _, run := range state.Runs {
		if run.Status == Waiting {
			waiting = append(waiting, run)
			continue
		}
		active = append(active, run)
		checkouts[run.Checkout] = run.ID
	}
	sort.SliceStable(waiting, func(left, right int) bool {
		if waiting[left].Order != waiting[right].Order {
			return waiting[left].Order < waiting[right].Order
		}
		return waiting[left].Arrived.Before(waiting[right].Arrived)
	})
	holds := map[string]string{}
	if all := holdOfAll(state, active, m, cfg, now); all != "" {
		for _, run := range waiting {
			holds[run.ID] = all
		}
		return holds
	}
	starts, blocks := "", ""
	for _, run := range waiting {
		switch {
		case blocks != "":
			holds[run.ID] = blocks
		case starts != "":
			holds[run.ID] = "after run " + starts + ", which starts now"
		case checkouts[run.Checkout] != "":
			holds[run.ID] = "same worktree as run " + checkouts[run.Checkout]
		default:
			short := shortfalls(run, active, m, cfg)
			if len(short) == 0 {
				starts = run.ID
				continue
			}
			holds[run.ID] = strings.Join(short, "; ")
			if now.Sub(run.Arrived) >= cfg.BackfillFor {
				blocks = fmt.Sprintf("behind run %s, which has waited %s for room", run.ID, cfg.BackfillFor)
			}
		}
	}
	return holds
}

// holdOfAll is what keeps every waiting run from starting, "" for nothing.
func holdOfAll(state State, active []Run, m Measurements, cfg Config, now time.Time) string {
	if m.PressureFor >= cfg.PressureHold {
		if len(active) > 0 {
			return "memory pressure: no run starts beside others until memory has recovered"
		}
	} else if _, resumes := resume(state, m, cfg, now); resumes {
		return "a run paused for memory resumes first"
	}
	if len(active) >= cfg.Load.MaxParallel {
		return fmt.Sprintf("max_parallel: %d of %d runs are active", len(active), cfg.Load.MaxParallel)
	}
	for _, run := range active {
		if run.Exclusive {
			return "run " + run.ID + " is exclusive"
		}
		if pausedForMemory(run) {
			return "run " + run.ID + " is paused for memory"
		}
	}
	return ""
}

// shortfalls are the conditions of design G4 a run misses beside the active ones, in words; none when it fits.
func shortfalls(run Run, active []Run, m Measurements, cfg Config) []string {
	if len(active) == 0 && m.OtherActive == 0 {
		return nil
	}
	expected, known := expectedPeak(run, cfg)
	switch {
	case run.Exclusive:
		return []string{"exclusive: starts when no run is active"}
	case len(active) > 0 && !known:
		return []string{"no history: starts when no run is active; unpredicted_peak_bytes in the personal list lets it start beside others"}
	}
	var short []string
	free, kept := available(m), claimed(active, cfg)
	if free-kept-expected < cfg.Load.ReserveBytes {
		needs := gibibytes(cfg.Load.ReserveBytes) + " reserve"
		switch {
		case known && run.Predicted:
			needs = gibibytes(expected) + " and " + needs
		case known:
			needs = gibibytes(expected) + " (estimate) and " + needs
		}
		note := fmt.Sprintf("memory: needs %s, %s are free", needs, gibibytes(free-kept))
		if kept > 0 {
			note += fmt.Sprintf(": %s less %s the running runs without history may still take", gibibytes(free), gibibytes(kept))
		}
		short = append(short, note)
	}
	if m.HostFreePercent < cfg.Load.HostRecoveredPercent {
		short = append(short, fmt.Sprintf("host memory: %d %% free, starts at %d %%", m.HostFreePercent, cfg.Load.HostRecoveredPercent))
	}
	if m.Load1/float64(m.Cores) >= cfg.Load.LoadPerCore {
		short = append(short, fmt.Sprintf("load: %.1f on %d cores, starts below %.1f", m.Load1, m.Cores, cfg.Load.LoadPerCore*float64(m.Cores)))
	}
	if m.SwapOutBytesPer2s >= cfg.Load.SwapBytesPer2s {
		short = append(short, fmt.Sprintf("swap: %.1f MiB written per 2 s, starts below %.1f MiB", float64(m.SwapOutBytesPer2s)/(1<<20), float64(cfg.Load.SwapBytesPer2s)/(1<<20)))
	}
	return short
}

func gibibytes(bytes int64) string {
	return fmt.Sprintf("%.1f GiB", float64(bytes)/(1<<30))
}
