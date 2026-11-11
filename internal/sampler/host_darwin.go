//go:build darwin

package sampler

import (
	"encoding/binary"
	"fmt"
	"os/exec"
	"time"

	"golang.org/x/sys/unix"
)

// ReadHost measures the host through sysctl and vm_stat.
func ReadHost() (HostSample, error) {
	sample := HostSample{Time: time.Now()}
	level, err := unix.SysctlUint32("kern.memorystatus_level")
	if err != nil {
		return HostSample{}, fmt.Errorf("heavy: sysctl kern.memorystatus_level failed: %w", err)
	}
	memory, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return HostSample{}, fmt.Errorf("heavy: sysctl hw.memsize failed: %w", err)
	}
	cores, err := unix.SysctlUint32("hw.ncpu")
	if err != nil {
		return HostSample{}, fmt.Errorf("heavy: sysctl hw.ncpu failed: %w", err)
	}
	sample.FreePercent, sample.MemoryBytes, sample.Cores = int(level), int64(memory), int(cores)
	if sample.Load1, err = load1(); err != nil {
		return HostSample{}, err
	}
	if sample.SwapUsedBytes, err = swapUsed(); err != nil {
		return HostSample{}, err
	}
	output, err := exec.Command("vm_stat").Output()
	if err != nil {
		return HostSample{}, fmt.Errorf("heavy: vm_stat failed: %w", err)
	}
	if sample.PageSize, sample.SwapOutPages, err = ParseVMStat(string(output)); err != nil {
		return HostSample{}, err
	}
	return sample, nil
}

// load1 reads struct loadavg {fixpt_t ldavg[3]; long fscale}: three uint32 values, padding, an int64 scale.
func load1() (float64, error) {
	raw, err := unix.SysctlRaw("vm.loadavg")
	if err != nil {
		return 0, fmt.Errorf("heavy: sysctl vm.loadavg failed: %w", err)
	}
	if len(raw) != 24 {
		return 0, fmt.Errorf("heavy: sysctl vm.loadavg gave %d bytes, expected the 24 of struct loadavg", len(raw))
	}
	scale := binary.LittleEndian.Uint64(raw[16:24])
	return float64(binary.LittleEndian.Uint32(raw[0:4])) / float64(scale), nil
}

// swapUsed reads struct xsw_usage {uint64 total, avail, used; uint32 pagesize; boolean_t encrypted}.
func swapUsed() (int64, error) {
	raw, err := unix.SysctlRaw("vm.swapusage")
	if err != nil {
		return 0, fmt.Errorf("heavy: sysctl vm.swapusage failed: %w", err)
	}
	if len(raw) != 32 {
		return 0, fmt.Errorf("heavy: sysctl vm.swapusage gave %d bytes, expected the 32 of struct xsw_usage", len(raw))
	}
	return int64(binary.LittleEndian.Uint64(raw[16:24])), nil
}
