package sampler

import (
	"errors"
	"time"

	"github.com/andreasisaak/heavy/internal/helper"
	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/platform"
	"github.com/andreasisaak/heavy/internal/scheduler"
)

// errNoSampleYet is why the VM goes unmeasured before its first sample.
var errNoSampleYet = errors.New("yet")

// Machine keeps the samples of the host and the Docker VM and gives the scheduler its measurements (design G5, G7).
type Machine struct {
	host   *Host
	vmFree Series
	vmSwap Series
	vmLast helper.Sample
	vmAt   time.Time
	// vmError is why the VM is not measured now; nil after a good sample.
	vmError error
	vmFirst bool
}

// NewMachine returns a machine without samples.
func NewMachine() *Machine {
	return &Machine{host: NewHost(), vmError: errNoSampleYet, vmFirst: true}
}

// AddHost takes the next sample of the host.
func (m *Machine) AddHost(sample HostSample) {
	m.host.Add(sample)
}

// AddVM takes the next sample of the VM. A swap-out counter below the last one belongs to a restarted VM and starts
// the count again.
func (m *Machine) AddVM(at time.Time, sample helper.Sample) {
	if !m.vmFirst && at.After(m.vmAt) && sample.SwapOutBytes >= m.vmLast.SwapOutBytes {
		swapped := float64(sample.SwapOutBytes - m.vmLast.SwapOutBytes)
		m.vmSwap.Add(at, swapped*float64(2*time.Second)/float64(at.Sub(m.vmAt)))
	}
	m.vmFree.Add(at, float64(sample.MemAvailableBytes))
	m.vmLast, m.vmAt, m.vmError, m.vmFirst = sample, at, nil, false
}

// VMFailed notes why the VM could not be measured; the host stands alone until the next good sample.
func (m *Machine) VMFailed(err error) {
	m.vmError = err
}

// VMStatus is "" while the VM is measured, else the line `--status` shows, such as
// `VM: not measured (docker not reachable)` (EC-2).
func (m *Machine) VMStatus() string {
	if m.vmError == nil {
		return ""
	}
	if errors.Is(m.vmError, errNoSampleYet) {
		return platform.Docker + ": not measured yet"
	}
	return platform.Docker + ": not measured (" + m.vmError.Error() + ")"
}

// Measurements are the host's, and with a measured VM its cores, load and memory; pressure holds while the VM or
// the host is under its threshold, recovery while both are above theirs, swap-out is the larger of the two.
func (m *Machine) Measurements(load lists.Load) scheduler.Measurements {
	measurements := m.host.Measurements(load)
	if m.vmError != nil || measurements.Cores < 1 {
		return measurements
	}
	measurements.VMMeasured, measurements.VMAvailableBytes = true, m.vmLast.MemAvailableBytes
	measurements.Cores, measurements.Load1 = m.vmLast.Cores, m.vmLast.Load1
	measurements.SwapOutBytesPer2s = max(measurements.SwapOutBytesPer2s, int64(m.vmSwap.Mean(30*time.Second)))
	pressure := m.vmFree.HeldFor(func(free float64) bool { return free < float64(load.VMPressureBytes) })
	recovered := m.vmFree.HeldFor(func(free float64) bool { return free >= float64(load.VMRecoveredBytes) })
	measurements.PressureFor = max(measurements.PressureFor, pressure)
	measurements.RecoveredFor = min(measurements.RecoveredFor, recovered)
	return measurements
}

// FreeSeries is the lowest free memory of the VM in each 3 minutes of the last 30, oldest first, for the dashboard;
// empty while the VM is not measured (design G13).
func (m *Machine) FreeSeries(now time.Time) []int64 {
	series := []int64{}
	if m.vmError != nil {
		return series
	}
	for _, low := range m.vmFree.Lows(now, 3*time.Minute, 10) {
		series = append(series, int64(low))
	}
	return series
}
