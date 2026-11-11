package sampler

import (
	"syscall"

	"github.com/andreasisaak/heavy/internal/cgroup"
	"github.com/andreasisaak/heavy/internal/helper"
)

// LocalOptions say where the daemon of a Linux user reads the machine it runs on.
type LocalOptions struct {
	// ProcRoot is /proc, CgroupRoot /sys/fs/cgroup.
	ProcRoot   string
	CgroupRoot string
	PageSize   int64
	Kill       func(pid int, signal syscall.Signal) error
}

// Local measures the machine the daemon runs on, where docker needs no VM: native Linux. It answers what the helper
// in the Docker VM answers on a Mac, inside the daemon and with the rights of its user.
type Local struct {
	helper     helper.Options
	cgroupRoot string
}

// NewLocal returns the helper for the machine of the daemon.
func NewLocal(options LocalOptions) *Local {
	return &Local{
		helper:     helper.Options{Root: options.ProcRoot, PageSize: options.PageSize, Kill: options.Kill, Unprivileged: true},
		cgroupRoot: options.CgroupRoot,
	}
}

// Sample is the memory, swap, load and pressure of the machine. The available memory is at most what the cgroup
// limits of the daemon's user leave (memory.high and memory.max up to the root).
func (l *Local) Sample() (helper.Sample, error) {
	sample, err := helper.ReadSample(l.helper)
	if err != nil {
		return helper.Sample{}, err
	}
	headroom, limited, err := cgroup.Headroom(l.helper.Root, l.cgroupRoot)
	if err != nil {
		return helper.Sample{}, err
	}
	if limited {
		sample.MemAvailableBytes = min(sample.MemAvailableBytes, headroom)
	}
	return sample, nil
}

// Find lists the processes in containers that carry HEAVY_RUN=<tag>.
func (l *Local) Find(tag string) ([]helper.Process, error) {
	return helper.Find(l.helper, tag)
}

// Containers lists the containers of the machine with the resident memory of their processes.
func (l *Local) Containers() ([]helper.Container, error) {
	return helper.ContainerUsage(l.helper)
}

// Signal sends the signal (STOP, CONT, TERM or KILL) to the PIDs that still carry HEAVY_RUN=<tag>.
func (l *Local) Signal(tag, signal string, pids []int) (helper.Signaled, error) {
	return helper.SignalTagged(l.helper, helper.Request{Type: "signal", Tag: tag, Signal: signal, PIDs: pids})
}

// Close has nothing to end: the local helper starts no process.
func (l *Local) Close() error {
	return nil
}
