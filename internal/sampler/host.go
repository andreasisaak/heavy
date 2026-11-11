// Package sampler measures the machine for the scheduler (design G7): the host every 2 s now, the docker VM
// through the helper container later. It keeps 30 minutes of each value for averages, for how long a condition
// held, and for the dashboard.
package sampler

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/scheduler"
)

// HostSample is one measurement of the host.
type HostSample struct {
	Time time.Time
	// FreePercent is kern.memorystatus_level, the share of memory macOS counts as free.
	FreePercent int
	// MemoryBytes is hw.memsize.
	MemoryBytes int64
	// Cores is hw.ncpu.
	Cores int
	// Load1 is the load average over one minute.
	Load1 float64
	// SwapUsedBytes is the used part of vm.swapusage.
	SwapUsedBytes int64
	// PageSize and SwapOutPages come from vm_stat: the pages swapped out since boot.
	PageSize     int64
	SwapOutPages uint64
}

// SwapOutPer2s is the memory swapped out between two samples, scaled to 2 s. The current sample must follow the
// previous one, and the swapouts counter only grows while the host runs.
func SwapOutPer2s(previous, current HostSample) int64 {
	if !current.Time.After(previous.Time) {
		panic(fmt.Sprintf("sampler: sample at %s does not follow the one at %s", current.Time, previous.Time))
	}
	if current.SwapOutPages < previous.SwapOutPages {
		panic(fmt.Sprintf("sampler: swapouts fell from %d to %d", previous.SwapOutPages, current.SwapOutPages))
	}
	swapped := float64(current.SwapOutPages-previous.SwapOutPages) * float64(current.PageSize)
	return int64(swapped * float64(2*time.Second) / float64(current.Time.Sub(previous.Time)))
}

var (
	vmStatPageSize = regexp.MustCompile(`page size of ([0-9]+) bytes`)
	vmStatSwapouts = regexp.MustCompile(`(?m)^Swapouts:\s+([0-9]+)\.$`)
)

// ParseVMStat reads the page size and the swapouts counter from the output of vm_stat.
func ParseVMStat(output string) (pageSize int64, swapouts uint64, err error) {
	size := vmStatPageSize.FindStringSubmatch(output)
	if size == nil {
		return 0, 0, fmt.Errorf("heavy: vm_stat printed no page size, got %q", output)
	}
	count := vmStatSwapouts.FindStringSubmatch(output)
	if count == nil {
		return 0, 0, fmt.Errorf("heavy: vm_stat printed no Swapouts line, got %q", output)
	}
	pageSize, err = strconv.ParseInt(size[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("heavy: vm_stat page size %s: %w", size[1], err)
	}
	swapouts, err = strconv.ParseUint(count[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("heavy: vm_stat swapouts %s: %w", count[1], err)
	}
	return pageSize, swapouts, nil
}

// Host keeps the samples of the host.
type Host struct {
	free    Series
	swap    Series
	last    HostSample
	sampled bool
}

// NewHost returns a host without samples.
func NewHost() *Host {
	return &Host{}
}

// Add takes the next sample.
func (h *Host) Add(sample HostSample) {
	if h.sampled {
		h.swap.Add(sample.Time, float64(SwapOutPer2s(h.last, sample)))
	}
	h.free.Add(sample.Time, float64(sample.FreePercent))
	h.last, h.sampled = sample, true
}

// Measurements gives the scheduler the newest host values, the swap-out average of 30 s, and how long the host
// has stayed under pressure or recovered by the thresholds; the VM is not measured here. Without samples it is zero.
func (h *Host) Measurements(load lists.Load) scheduler.Measurements {
	if !h.sampled {
		return scheduler.Measurements{}
	}
	return scheduler.Measurements{
		Cores:             h.last.Cores,
		Load1:             h.last.Load1,
		HostFreePercent:   h.last.FreePercent,
		HostMemoryBytes:   h.last.MemoryBytes,
		SwapOutBytesPer2s: int64(h.swap.Mean(30 * time.Second)),
		PressureFor:       h.free.HeldFor(func(level float64) bool { return level < float64(load.HostPressurePercent) }),
		RecoveredFor:      h.free.HeldFor(func(level float64) bool { return level >= float64(load.HostRecoveredPercent) }),
	}
}
