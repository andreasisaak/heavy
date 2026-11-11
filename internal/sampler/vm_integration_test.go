//go:build integration

package sampler_test

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/detect"
	"github.com/andreasisaak/heavy/internal/platform"
	"github.com/andreasisaak/heavy/internal/sampler"
)

// realDocker is the docker program of the machine; the tests use only containers and images named heavy-probe-*.
func realDocker(t *testing.T) string {
	t.Helper()
	docker, found := detect.RealProgram("docker", os.Getenv("PATH"))
	if !found {
		t.Fatal("no docker on PATH")
	}
	return docker
}

// linuxBuild builds heavy for the VM, as bin/heavy-linux.
func linuxBuild(t *testing.T) string {
	t.Helper()
	output := filepath.Join(t.TempDir(), "heavy-linux")
	command := exec.Command("go", "build", "-o", output, "../../cmd/heavy")
	command.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+runtime.GOARCH, "CGO_ENABLED=0")
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("building heavy for linux: %v\n%s", err, result)
	}
	return output
}

func dockerRun(t *testing.T, docker string, arguments ...string) string {
	t.Helper()
	output, err := exec.Command(docker, arguments...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %v: %v\n%s", arguments, err, output)
	}
	return strings.TrimSpace(string(output))
}

// probeVM is a VM sampler with a probe image and container; both go when the test ends, and no heavy-probe-*
// container may stay behind.
func probeVM(t *testing.T, docker string) *sampler.VM {
	t.Helper()
	tag := fmt.Sprintf("%d", time.Now().UnixNano())
	vm := sampler.NewVM(sampler.VMOptions{
		Docker: docker, LinuxBinary: linuxBuild(t), Image: "heavy-probe-helper:" + tag, Container: "heavy-probe-helper-" + tag,
		Timeout: 5 * time.Second, Now: time.Now, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	t.Cleanup(func() {
		if err := vm.Close(); err != nil {
			t.Errorf("closing the VM sampler: %v", err)
		}
		_ = exec.Command(docker, "rmi", "-f", "heavy-probe-helper:"+tag).Run()
	})
	return vm
}

// noProbeLeft checks, after every other cleanup of the test, that no heavy-probe-* container stayed behind.
func noProbeLeft(t *testing.T, docker string) {
	t.Helper()
	t.Cleanup(func() {
		if left := dockerRun(t, docker, "ps", "-a", "--filter", "name=heavy-probe-", "--format", "{{.Names}}"); left != "" {
			t.Errorf("containers left behind: %s", left)
		}
	})
}

// plan 3.1: in a probe container, `docker exec -e HEAVY_RUN=probe sh -c 'sleep 30 & sleep 30'` gives two tagged
// processes; the helper finds both with their memory and the container they run in
func TestVMFindsTheTaggedProcessesOfAProbeContainer(t *testing.T) {
	docker := realDocker(t)
	noProbeLeft(t, docker)
	name := fmt.Sprintf("heavy-probe-find-%d", os.Getpid())
	dockerRun(t, docker, "run", "-d", "--rm", "--name", name, "alpine:3", "sleep", "120")
	t.Cleanup(func() { _ = exec.Command(docker, "rm", "-f", name).Run() })
	id := dockerRun(t, docker, "inspect", "--format", "{{.Id}}", name)
	dockerRun(t, docker, "exec", "-d", "-e", "HEAVY_RUN=probe", name, "sh", "-c", "sleep 30 & sleep 30")
	vm := probeVM(t, docker)

	deadline := time.Now().Add(20 * time.Second)
	var pids []int
	for {
		processes, err := vm.Find("probe")
		if err == nil && len(processes) == 2 {
			for _, process := range processes {
				if process.RSSBytes <= 0 || process.Container != id {
					t.Fatalf("process %+v, want resident memory and the container %s", process, id)
				}
				pids = append(pids, process.PID)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Find = %+v, %v; want the two tagged sleeps", processes, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	sort.Ints(pids)
	for _, signal := range []string{"STOP", "CONT"} {
		signaled, err := vm.Signal("probe", signal, append(pids, 1))
		if want := (helperSignaled{sent: pids, skipped: []int{1}}); err != nil || !reflect.DeepEqual(helperSignaled{signaled.Sent, signaled.Skipped}, want) {
			t.Fatalf("Signal %s = %+v, %v; want %+v", signal, signaled, err, want)
		}
	}
	containers, err := vm.Containers()
	found := false
	for _, container := range containers {
		found = found || (container.ID == id && container.RSSBytes > 0)
	}
	if err != nil || !found {
		t.Fatalf("Containers = %+v, %v; want the probe container %s with memory", containers, err, id)
	}
	sample, err := vm.Sample()
	if err != nil || sample.MemAvailableBytes <= 0 || sample.Cores < 1 {
		t.Fatalf("Sample = %+v, %v", sample, err)
	}
}

// plan 3.1 and EC-2: docker that cannot be reached leaves the VM unmeasured with the reason
func TestVMWithoutAReachableDockerIsNotMeasured(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///nonexistent")
	vm := unreachableVM(t, realDocker(t))
	machine := sampler.NewMachine()

	_, err := vm.Sample()
	machine.VMFailed(err)

	if !errors.Is(err, sampler.ErrNotReachable) || machine.VMStatus() != platform.Docker+": not measured (docker not reachable)" {
		t.Fatalf("Sample error %v, status %q", err, machine.VMStatus())
	}
}

// unreachableVM is a VM sampler that never gets to start a container.
func unreachableVM(t *testing.T, docker string) *sampler.VM {
	t.Helper()
	return sampler.NewVM(sampler.VMOptions{
		Docker: docker, LinuxBinary: filepath.Join(t.TempDir(), "heavy-linux"), Image: "heavy-probe-helper:none",
		Container: "heavy-probe-helper-none", Timeout: 5 * time.Second, Now: time.Now, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
}

type helperSignaled struct {
	sent    []int
	skipped []int
}
