package sampler

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/andreasisaak/heavy/internal/helper"
)

// ReadProcHost measures a Linux host from the /proc root: MemTotal, MemAvailable and the swap of meminfo, pswpout
// of vmstat, the load of one minute and the processors. The share of free memory is MemAvailable in MemTotal.
func ReadProcHost(root string, pageSize int64) (HostSample, error) {
	machine, err := helper.ReadSample(helper.Options{Root: root, PageSize: pageSize})
	if err != nil {
		return HostSample{}, err
	}
	if machine.Cores < 1 {
		return HostSample{}, fmt.Errorf("heavy: %s: expected processor lines, found none", filepath.Join(root, "cpuinfo"))
	}
	meminfo := filepath.Join(root, "meminfo")
	total, err := helper.Field(meminfo, "MemTotal:", "MemTotal: <n> kB")
	if err != nil {
		return HostSample{}, err
	}
	if total < 1 {
		return HostSample{}, fmt.Errorf("heavy: %s: expected MemTotal above 0, got %d kB", meminfo, total)
	}
	swapTotal, err := helper.Field(meminfo, "SwapTotal:", "SwapTotal: <n> kB")
	if err != nil {
		return HostSample{}, err
	}
	swapFree, err := helper.Field(meminfo, "SwapFree:", "SwapFree: <n> kB")
	if err != nil {
		return HostSample{}, err
	}
	return HostSample{
		Time: time.Now(), FreePercent: int(machine.MemAvailableBytes / 1024 * 100 / total), MemoryBytes: total * 1024,
		Cores: machine.Cores, Load1: machine.Load1, SwapUsedBytes: (swapTotal - swapFree) * 1024,
		PageSize: pageSize, SwapOutPages: uint64(machine.SwapOutBytes / pageSize),
	}, nil
}
