package daemon_test

import (
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/platform"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/sampler"
)

// localProc writes the /proc of a Linux machine with eight processors and the given available memory.
func localProc(t *testing.T, availableKilobytes string) string {
	t.Helper()
	proc := t.TempDir()
	write(t, filepath.Join(proc, "meminfo"), "MemTotal:       131072000 kB\nMemAvailable:   "+availableKilobytes+" kB\n")
	write(t, filepath.Join(proc, "vmstat"), "pswpout 0\n")
	write(t, filepath.Join(proc, "loadavg"), "0.50 0.50 0.50 1/100 99\n")
	write(t, filepath.Join(proc, "cpuinfo"), "processor\t: 0\n\nprocessor\t: 1\n\nprocessor\t: 2\n\nprocessor\t: 3\n\nprocessor\t: 4\n\nprocessor\t: 5\n\nprocessor\t: 6\n\nprocessor\t: 7\n")
	write(t, filepath.Join(proc, "self", "cgroup"), "0::/user.slice/user-1000.slice\n")
	return proc
}

func localOptions(proc, groups string) *sampler.LocalOptions {
	return &sampler.LocalOptions{ProcRoot: proc, CgroupRoot: groups, PageSize: 4096, Kill: func(int, syscall.Signal) error { return nil }}
}

// On native Linux the daemon measures the machine itself, without docker and without a helper container.
func TestALocalDaemonMeasuresItsMachineWithoutDocker(t *testing.T) {
	proc := localProc(t, "91750400")
	current := startDaemonWith(t, protocol.Identity{}, func(options *daemon.Options) {
		options.Local, options.WithoutVM = localOptions(proc, t.TempDir()), ""
	})

	status := detail(t, current.state)

	want := protocol.Machine{
		Cores: 8, VMMeasured: true, VMAvailableBytes: 91750400 << 10, Load1: 0.5, HostFreePercent: 50,
		FreeSeries: []int64{91750400 << 10}, Foreign: []protocol.ForeignContainer{}, Recent: []protocol.Result{},
	}
	if status.VM != "" || status.Machine == nil || !reflect.DeepEqual(*status.Machine, want) {
		t.Fatalf("status = VM %q, machine %+v\nwant VM \"\", machine %+v", status.VM, status.Machine, want)
	}
}

func TestALocalDaemonCountsTheCgroupLimitOfItsUser(t *testing.T) {
	proc, groups := localProc(t, "91750400"), t.TempDir()
	write(t, filepath.Join(groups, "user.slice", "user-1000.slice", "memory.high"), "85899345920\n")
	write(t, filepath.Join(groups, "user.slice", "user-1000.slice", "memory.current"), "75161927680\n")
	current := startDaemonWith(t, protocol.Identity{}, func(options *daemon.Options) {
		options.Local, options.WithoutVM = localOptions(proc, groups), ""
	})

	machine := detail(t, current.state).Machine

	if machine == nil || !machine.VMMeasured || machine.VMAvailableBytes != 10<<30 {
		t.Fatalf("machine = %+v, want 10 GiB available under the limit of the user slice", machine)
	}
}

func TestALocalDaemonSaysWhyItsMachineIsNotMeasured(t *testing.T) {
	proc := localProc(t, "91750400")
	write(t, filepath.Join(proc, "meminfo"), "MemTotal:       131072000 kB\n")
	current := startDaemonWith(t, protocol.Identity{}, func(options *daemon.Options) {
		options.Local, options.WithoutVM = localOptions(proc, t.TempDir()), ""
	})

	status := detail(t, current.state)

	want := platform.Docker + ": not measured (heavy: " + proc + "/meminfo: expected a line MemAvailable: <n> kB, found none)"
	if status.VM != want || status.Machine == nil || status.Machine.VMMeasured {
		t.Fatalf("status = VM %q, machine %+v, want VM %q and the host alone", status.VM, status.Machine, want)
	}
}
