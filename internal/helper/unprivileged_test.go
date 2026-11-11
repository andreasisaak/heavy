package helper_test

import (
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"github.com/andreasisaak/heavy/internal/helper"
)

// foreignFixture holds a tagged process in a container, a tagged one outside containers, and the process of
// another user, whose environment this user may not read.
func foreignFixture(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root reads every file, so no environment is unreadable")
	}
	root := fixture(t, merge(
		process("101", []string{"HEAVY_RUN=r1"}, "2048", "0::/user.slice/user-1000.slice/user@1000.service/docker-"+containerA+".scope\n"),
		process("57", []string{"HEAVY_RUN=r1"}, "1024", "0::/user.slice/user-1000.slice/session-3.scope\n"),
		process("900", []string{"HEAVY_RUN=r1"}, "4096", "0::/docker/"+containerB+"\n"),
	))
	if err := os.Chmod(filepath.Join(root, "900", "environ"), 0); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestUnprivilegedFindSkipsForeignProcessesAndThoseOutsideContainers(t *testing.T) {
	root := foreignFixture(t)

	processes, err := helper.Find(helper.Options{Root: root, Unprivileged: true}, "r1")

	want := []helper.Process{{PID: 101, RSSBytes: 2048 * 1024, Container: containerA}}
	if err != nil || !reflect.DeepEqual(processes, want) {
		t.Fatalf("Find = %#v, %v, want %#v", processes, err, want)
	}
}

func TestPrivilegedFindFailsOnAnUnreadableEnvironment(t *testing.T) {
	root := foreignFixture(t)

	_, err := helper.Find(helper.Options{Root: root}, "r1")

	want := "heavy: reading process 900: open " + root + "/900/environ: permission denied"
	if err == nil || err.Error() != want {
		t.Fatalf("Find error = %v, want %q", err, want)
	}
}

func TestUnprivilegedSignalSkipsAForeignProcess(t *testing.T) {
	root := foreignFixture(t)
	var sent []int
	kill := func(pid int, signal syscall.Signal) error {
		sent = append(sent, pid)
		return nil
	}

	signaled, err := helper.SignalTagged(helper.Options{Root: root, Unprivileged: true, Kill: kill}, helper.Request{Type: "signal", Tag: "r1", Signal: "STOP", PIDs: []int{101, 900}})

	want := helper.Signaled{Sent: []int{101}, Skipped: []int{900}}
	if err != nil || !reflect.DeepEqual(signaled, want) || !reflect.DeepEqual(sent, []int{101}) {
		t.Fatalf("Signal = %#v, %v, killed %v, want %#v and kill of 101 alone", signaled, err, sent, want)
	}
}

func TestPrivilegedSignalFailsOnAnUnreadableEnvironment(t *testing.T) {
	root := foreignFixture(t)
	kill := func(int, syscall.Signal) error { return nil }

	_, err := helper.SignalTagged(helper.Options{Root: root, Kill: kill}, helper.Request{Type: "signal", Tag: "r1", Signal: "STOP", PIDs: []int{900}})

	want := "heavy: reading process 900: open " + root + "/900/environ: permission denied"
	if err == nil || err.Error() != want {
		t.Fatalf("Signal error = %v, want %q", err, want)
	}
}

func TestUsageSumsTheContainersOfASystemdCgroupPath(t *testing.T) {
	root := fixture(t, merge(
		process("101", nil, "2048", "0::/user.slice/user-1000.slice/user@1000.service/user.slice/docker-"+containerA+".scope\n"),
		process("102", nil, "1024", "0::/user.slice/user-1000.slice/user@1000.service/user.slice/docker-"+containerA+".scope\n"),
		process("57", nil, "512", "0::/user.slice/user-1000.slice/session-3.scope\n"),
	))

	containers, err := helper.ContainerUsage(helper.Options{Root: root})

	want := []helper.Container{{ID: containerA, RSSBytes: 3072 * 1024}}
	if err != nil || !reflect.DeepEqual(containers, want) {
		t.Fatalf("Usage = %#v, %v, want %#v", containers, err, want)
	}
}

func TestUnprivilegedUsageSkipsAProcessItMayNotRead(t *testing.T) {
	root := foreignFixture(t)
	if err := os.Chmod(filepath.Join(root, "900", "status"), 0); err != nil {
		t.Fatal(err)
	}

	containers, err := helper.ContainerUsage(helper.Options{Root: root, Unprivileged: true})
	_, privilegedError := helper.ContainerUsage(helper.Options{Root: root})

	want := []helper.Container{{ID: containerA, RSSBytes: 2048 * 1024}}
	wantError := "heavy: reading process 900: open " + root + "/900/status: permission denied"
	if err != nil || !reflect.DeepEqual(containers, want) || privilegedError == nil || privilegedError.Error() != wantError {
		t.Fatalf("ContainerUsage = %#v, %v; privileged error %v, want %#v and %q", containers, err, privilegedError, want, wantError)
	}
}
