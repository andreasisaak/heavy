package scheduler

import (
	"math"
	"sort"
	"time"
)

// admit starts the first waiting run in queue order that may start. A run whose checkout has an active run waits
// for it, and the runs behind may pass. A run that does not fit beside the active ones lets the runs behind it pass
// that do fit, until it has waited BackfillFor; from then on it keeps every run behind it waiting, so a big run
// cannot wait forever behind a stream of small ones (V52).
func admit(state State, m Measurements, cfg Config, now time.Time) (Action, bool) {
	var active, waiting []Run
	checkouts := map[string]bool{}
	for _, run := range state.Runs {
		switch run.Status {
		case Waiting:
			waiting = append(waiting, run)
		default:
			active = append(active, run)
			checkouts[run.Checkout] = true
		}
	}
	if len(active) >= cfg.Load.MaxParallel {
		return Action{}, false
	}
	for _, run := range active {
		if run.Exclusive || pausedForMemory(run) {
			return Action{}, false
		}
	}
	sort.SliceStable(waiting, func(left, right int) bool {
		if waiting[left].Order != waiting[right].Order {
			return waiting[left].Order < waiting[right].Order
		}
		return waiting[left].Arrived.Before(waiting[right].Arrived)
	})
	for _, run := range waiting {
		if checkouts[run.Checkout] {
			continue
		}
		if fits(run, active, m, cfg) {
			return Action{Kind: Start, ID: run.ID, Cores: cores(len(active) == 0 && m.OtherActive == 0, m)}, true
		}
		if now.Sub(run.Arrived) >= cfg.BackfillFor {
			return Action{}, false
		}
	}
	return Action{}, false
}

// fits checks the memory, load and swap conditions of design G4 for a run beside the active ones. A run beside which
// nothing is active on the whole machine always fits: waiting frees nothing when heavy runs nothing (V2 of the
// plan). Beside own runs a run needs an expected peak: that of its history, or unpredicted_peak_bytes when the
// personal list names it. Beside the runs of other users alone it may start without one, since waiting for a
// machine without runs could last forever, and then needs the reserve.
func fits(run Run, active []Run, m Measurements, cfg Config) bool {
	if len(active) == 0 && m.OtherActive == 0 {
		return true
	}
	expected, known := expectedPeak(run, cfg)
	if run.Exclusive || (len(active) > 0 && !known) {
		return false
	}
	return available(m)-claimed(active, cfg)-expected >= cfg.Load.ReserveBytes &&
		m.HostFreePercent >= cfg.Load.HostRecoveredPercent &&
		m.Load1/float64(m.Cores) < cfg.Load.LoadPerCore &&
		m.SwapOutBytesPer2s < cfg.Load.SwapBytesPer2s
}

// expectedPeak is the peak memory to expect of a run: that of its history, else the estimate of the personal list
// for a run without history. known is false when neither is there; the peak is 0 then.
func expectedPeak(run Run, cfg Config) (bytes int64, known bool) {
	if run.Predicted {
		return run.PredictedBytes, true
	}
	return cfg.Load.UnpredictedPeakBytes, cfg.Load.UnpredictedPeakBytes > 0
}

// claimed is the memory the active runs without history may still take: for each the part of the estimate its
// measured peak has not reached. The free memory does not show it yet, since a run takes its memory only as it
// goes; without the estimate nothing is claimed.
func claimed(active []Run, cfg Config) int64 {
	var total int64
	for _, run := range active {
		if !run.Predicted {
			total += max(0, cfg.Load.UnpredictedPeakBytes-run.PeakBytes)
		}
	}
	return total
}

// available is the free memory of the VM; without a VM measurement, the free part of the host's memory.
func available(m Measurements) int64 {
	if m.VMMeasured {
		return m.VMAvailableBytes
	}
	return m.HostMemoryBytes * int64(m.HostFreePercent) / 100
}

// cores is HEAVY_CPUS of a run that starts: all cores alone on the machine, else the cores the load leaves, at least one.
func cores(alone bool, m Measurements) int {
	if alone {
		return m.Cores
	}
	return max(1, int(math.Floor(float64(m.Cores)-m.Load1)))
}
