package sampler_test

import (
	"reflect"
	"syscall"
	"testing"

	"github.com/andreasisaak/heavy/internal/helper"
	"github.com/andreasisaak/heavy/internal/sampler"
)

const localContainer = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

// localFiles are a box with 87.5 GiB available and a run r1 with one process in a container and one outside.
func localFiles() map[string]string {
	files := boxFiles()
	files["self/cgroup"] = "0::/user.slice/user-1000.slice/session-3.scope\n"
	files["101/environ"] = "HEAVY_RUN=r1\x00"
	files["101/status"] = "Name:\tphp\nVmRSS:\t    2048 kB\n"
	files["101/cgroup"] = "0::/user.slice/user-1000.slice/user@1000.service/docker-" + localContainer + ".scope\n"
	files["57/environ"] = "HEAVY_RUN=r1\x00"
	files["57/status"] = "Name:\tmake\nVmRSS:\t    1024 kB\n"
	files["57/cgroup"] = "0::/user.slice/user-1000.slice/session-3.scope\n"
	return files
}

func TestLocalSampleIsTheMachineWithoutCgroupLimits(t *testing.T) {
	local := sampler.NewLocal(sampler.LocalOptions{ProcRoot: procRoot(t, localFiles()), CgroupRoot: t.TempDir(), PageSize: 4096})

	sample, err := local.Sample()

	want := helper.Sample{MemAvailableBytes: 91750400 * 1024, SwapOutBytes: 2500 * 4096, Load1: 14.8, Cores: 4}
	if err != nil || !reflect.DeepEqual(sample, want) {
		t.Fatalf("Sample = %#v, %v, want %#v", sample, err, want)
	}
}

func TestLocalSampleIsCappedByTheHeadroomOfTheUserSlice(t *testing.T) {
	groups := procRoot(t, map[string]string{
		"user.slice/user-1000.slice/memory.high": "85899345920\n", "user.slice/user-1000.slice/memory.max": "max\n",
		"user.slice/user-1000.slice/memory.current": "75161927680\n",
	})
	local := sampler.NewLocal(sampler.LocalOptions{ProcRoot: procRoot(t, localFiles()), CgroupRoot: groups, PageSize: 4096})

	sample, err := local.Sample()

	if err != nil || sample.MemAvailableBytes != 10<<30 {
		t.Fatalf("Sample = %#v, %v, want 10 GiB available, the 80 GiB of memory.high less the 70 GiB in use", sample, err)
	}
}

func TestLocalSampleWithABrokenCgroupLimitIsAnError(t *testing.T) {
	groups := procRoot(t, map[string]string{"user.slice/memory.max": "lots\n"})
	local := sampler.NewLocal(sampler.LocalOptions{ProcRoot: procRoot(t, localFiles()), CgroupRoot: groups, PageSize: 4096})

	_, err := local.Sample()

	want := "heavy: " + groups + "/user.slice/memory.max: expected bytes or max, got \"lots\\n\""
	if err == nil || err.Error() != want {
		t.Fatalf("Sample error = %v, want %q", err, want)
	}
}

func TestLocalFindsTheTaggedProcessesInContainersOnly(t *testing.T) {
	local := sampler.NewLocal(sampler.LocalOptions{ProcRoot: procRoot(t, localFiles()), CgroupRoot: t.TempDir(), PageSize: 4096})

	processes, err := local.Find("r1")

	want := []helper.Process{{PID: 101, RSSBytes: 2048 * 1024, Container: localContainer}}
	if err != nil || !reflect.DeepEqual(processes, want) {
		t.Fatalf("Find = %#v, %v, want %#v", processes, err, want)
	}
}

func TestLocalListsTheMemoryOfTheContainers(t *testing.T) {
	local := sampler.NewLocal(sampler.LocalOptions{ProcRoot: procRoot(t, localFiles()), CgroupRoot: t.TempDir(), PageSize: 4096})

	containers, err := local.Containers()

	want := []helper.Container{{ID: localContainer, RSSBytes: 2048 * 1024}}
	if err != nil || !reflect.DeepEqual(containers, want) {
		t.Fatalf("Containers = %#v, %v, want %#v", containers, err, want)
	}
}

func TestLocalSignalsTheProcessesThatCarryTheTag(t *testing.T) {
	type sent struct {
		pid    int
		signal syscall.Signal
	}
	var signals []sent
	kill := func(pid int, signal syscall.Signal) error {
		signals = append(signals, sent{pid, signal})
		return nil
	}
	local := sampler.NewLocal(sampler.LocalOptions{ProcRoot: procRoot(t, localFiles()), CgroupRoot: t.TempDir(), PageSize: 4096, Kill: kill})

	signaled, err := local.Signal("r1", "STOP", []int{101, 4000})

	want := helper.Signaled{Sent: []int{101}, Skipped: []int{4000}}
	if err != nil || !reflect.DeepEqual(signaled, want) || !reflect.DeepEqual(signals, []sent{{101, syscall.SIGSTOP}}) {
		t.Fatalf("Signal = %#v, %v, sent %v, want %#v and SIGSTOP to 101 alone", signaled, err, signals, want)
	}
}

func TestLocalSignalFailsWhenKillFails(t *testing.T) {
	kill := func(int, syscall.Signal) error { return syscall.EPERM }
	local := sampler.NewLocal(sampler.LocalOptions{ProcRoot: procRoot(t, localFiles()), CgroupRoot: t.TempDir(), PageSize: 4096, Kill: kill})

	_, err := local.Signal("r1", "TERM", []int{101})

	want := "heavy: sending TERM to process 101: operation not permitted"
	if err == nil || err.Error() != want {
		t.Fatalf("Signal error = %v, want %q", err, want)
	}
}
