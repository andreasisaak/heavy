package sampler_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/helper"
	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/platform"
	"github.com/andreasisaak/heavy/internal/sampler"
	"github.com/andreasisaak/heavy/internal/scheduler"
)

var machineStart = time.Date(2026, 11, 8, 12, 0, 0, 0, time.Local)

// hostSample is a host with 40 % free memory, load 3 on 18 cores and the given swap-out counter.
func hostSample(at time.Duration, swapouts uint64) sampler.HostSample {
	return sampler.HostSample{Time: machineStart.Add(at), FreePercent: 40, MemoryBytes: 48 << 30, Cores: 18, Load1: 3, SwapOutPages: swapouts, PageSize: 16384}
}

func vmSample(free int64, swapped int64) helper.Sample {
	return helper.Sample{MemAvailableBytes: free, SwapOutBytes: swapped, Load1: 2.5, Cores: 8}
}

// plan 3.3 and G5: the VM gives cores, load and memory; pressure holds while the VM or the host is under its
// threshold, recovery while both are above theirs; swap is the larger of the two
func TestMachineCombinesTheVMAndTheHost(t *testing.T) {
	machine := sampler.NewMachine()
	for step := time.Duration(0); step <= 12*time.Second; step += 2 * time.Second {
		machine.AddHost(hostSample(step, 100))
		machine.AddVM(machineStart.Add(step), vmSample(1<<30, int64(step/time.Second)*mebibyte))
	}

	got := machine.Measurements(lists.DefaultLoad())

	want := scheduler.Measurements{
		Cores: 8, Load1: 2.5, VMMeasured: true, VMAvailableBytes: 1 << 30, HostFreePercent: 40, HostMemoryBytes: 48 << 30,
		SwapOutBytesPer2s: 2 * mebibyte, PressureFor: 12 * time.Second, RecoveredFor: 0,
	}
	if !reflect.DeepEqual(got, want) || machine.VMStatus() != "" {
		t.Fatalf("Measurements = %+v, status %q; want %+v and no status", got, machine.VMStatus(), want)
	}
}

func TestMachineRecoversOnlyWhenTheVMAndTheHostDo(t *testing.T) {
	machine := sampler.NewMachine()
	for step := time.Duration(0); step <= 40*time.Second; step += 2 * time.Second {
		machine.AddHost(hostSample(step, 100))
		free := int64(1 << 30)
		if step >= 8*time.Second {
			free = 3 << 30
		}
		machine.AddVM(machineStart.Add(step), vmSample(free, 0))
	}
	load := lists.DefaultLoad()
	load.HostRecoveredPercent = 30

	got := machine.Measurements(load)

	if got.RecoveredFor != 32*time.Second || got.PressureFor != 0 {
		t.Fatalf("RecoveredFor %s, PressureFor %s; want 32s and 0", got.RecoveredFor, got.PressureFor)
	}
}

// EC-2: without the VM the host stands alone, and the status says why
func TestMachineWithoutTheVMMeasuresTheHost(t *testing.T) {
	machine := sampler.NewMachine()
	machine.AddHost(hostSample(0, 100))
	machine.AddVM(machineStart, vmSample(1<<30, 0))
	machine.VMFailed(sampler.ErrNotReachable)
	machine.AddHost(hostSample(2*time.Second, 100))

	got := machine.Measurements(lists.DefaultLoad())

	want := scheduler.Measurements{Cores: 18, Load1: 3, HostFreePercent: 40, HostMemoryBytes: 48 << 30, RecoveredFor: 2 * time.Second}
	if !reflect.DeepEqual(got, want) || machine.VMStatus() != platform.Docker+": not measured (docker not reachable)" {
		t.Fatalf("Measurements = %+v, status %q; want %+v", got, machine.VMStatus(), want)
	}
}

func TestMachineBeforeItsFirstVMSampleSaysSo(t *testing.T) {
	machine := sampler.NewMachine()
	machine.AddHost(hostSample(0, 100))

	if got := machine.VMStatus(); got != platform.Docker+": not measured yet" || machine.Measurements(lists.DefaultLoad()).VMMeasured {
		t.Fatalf("VMStatus = %q", got)
	}
}

// a VM that restarted counts its swap-outs from 0 again; the fall is a new start, not a swap-out
func TestMachineTakesAFallingVMSwapCounterAsANewStart(t *testing.T) {
	machine := sampler.NewMachine()
	machine.AddHost(hostSample(0, 100))
	machine.AddVM(machineStart, vmSample(4<<30, 500*mebibyte))
	machine.AddVM(machineStart.Add(2*time.Second), vmSample(4<<30, 4*mebibyte))
	machine.AddVM(machineStart.Add(4*time.Second), vmSample(4<<30, 6*mebibyte))

	if got := machine.Measurements(lists.DefaultLoad()).SwapOutBytesPer2s; got != 2*mebibyte {
		t.Fatalf("SwapOutBytesPer2s = %d, want %d", got, 2*mebibyte)
	}
}

func TestMachineVMFailureOtherThanDockerNamesTheError(t *testing.T) {
	machine := sampler.NewMachine()
	machine.VMFailed(errors.New("heavy: the helper in the VM ended"))

	if got := machine.VMStatus(); got != platform.Docker+": not measured (heavy: the helper in the VM ended)" {
		t.Fatalf("VMStatus = %q", got)
	}
}
