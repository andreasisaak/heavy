package procs_test

import (
	"errors"
	"fmt"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/helper"
	"github.com/andreasisaak/heavy/internal/procs"
)

func TestPauseSignalsHostVMAndCoStartedContainersInOrder(t *testing.T) {
	f := newFixture()
	f.docker.containers = []procs.Container{dbContainer, oldContainer, otherContainer}
	if err := f.tracker.Pause(standardRun()); err != nil {
		t.Fatalf("Pause returned error: %v", err)
	}
	assertEvents(t, f, []string{"host 4242 STOP", "vm run-1 STOP [10 11]", "docker pause db-test-1"})
}

func TestResumeContinuesHostVMAndUnpausesContainers(t *testing.T) {
	f := newFixture()
	paused := dbContainer
	paused.Paused = true
	f.docker.containers = []procs.Container{paused, oldContainer, otherContainer}
	if err := f.tracker.Resume(standardRun()); err != nil {
		t.Fatalf("Resume returned error: %v", err)
	}
	assertEvents(t, f, []string{"host 4242 CONT", "vm run-1 CONT [10 11]", "docker unpause db-test-1"})
}

func TestPauseWithoutVMProcessesSendsNoHelperRequest(t *testing.T) {
	f := newFixture()
	f.vm.processes = nil
	f.docker.containers = []procs.Container{dbContainer}
	if err := f.tracker.Pause(standardRun()); err != nil {
		t.Fatalf("Pause returned error: %v", err)
	}
	assertEvents(t, f, []string{"host 4242 STOP", "docker pause db-test-1"})
}

func TestPauseWithUnreachableHelperStillStopsHost(t *testing.T) {
	f := newFixture()
	f.vm.findErr = errors.New("docker not reachable")
	err := f.tracker.Pause(standardRun())
	assertErrorText(t, err, "heavy: VM processes of run run-1 not paused: docker not reachable")
	assertEvents(t, f, []string{"host 4242 STOP"})
}

func TestPauseHostFailureStillReachesVMAndDocker(t *testing.T) {
	f := newFixture()
	hostFailure := errors.New("operation not permitted")
	f.host.signalErr = hostFailure
	f.docker.containers = []procs.Container{dbContainer}
	err := f.tracker.Pause(standardRun())
	if !errors.Is(err, hostFailure) {
		t.Fatalf("error %v does not wrap %v", err, hostFailure)
	}
	assertEvents(t, f, []string{"host 4242 STOP", "vm run-1 STOP [10 11]", "docker pause db-test-1"})
}

func TestPauseHelperSignalFailureIsReportedAndDockerStillPauses(t *testing.T) {
	f := newFixture()
	helperFailure := errors.New("helper write failed")
	f.vm.signalErr = helperFailure
	f.docker.containers = []procs.Container{dbContainer}
	err := f.tracker.Pause(standardRun())
	if !errors.Is(err, helperFailure) {
		t.Fatalf("error %v does not wrap %v", err, helperFailure)
	}
	assertEvents(t, f, []string{"host 4242 STOP", "vm run-1 STOP [10 11]", "docker pause db-test-1"})
}

func TestPauseDockerFailureIsReported(t *testing.T) {
	f := newFixture()
	dockerFailure := errors.New("container is not running")
	f.docker.pauseErr = dockerFailure
	f.docker.containers = []procs.Container{dbContainer}
	err := f.tracker.Pause(standardRun())
	if !errors.Is(err, dockerFailure) {
		t.Fatalf("error %v does not wrap %v", err, dockerFailure)
	}
	assertEvents(t, f, []string{"host 4242 STOP", "vm run-1 STOP [10 11]", "docker pause db-test-1"})
}

func TestResumeDockerFailureIsReported(t *testing.T) {
	f := newFixture()
	dockerFailure := errors.New("container is not paused")
	f.docker.unpauseErr = dockerFailure
	paused := dbContainer
	paused.Paused = true
	f.docker.containers = []procs.Container{paused}
	err := f.tracker.Resume(standardRun())
	if !errors.Is(err, dockerFailure) {
		t.Fatalf("error %v does not wrap %v", err, dockerFailure)
	}
	assertEvents(t, f, []string{"host 4242 CONT", "vm run-1 CONT [10 11]", "docker unpause db-test-1"})
}

func TestRefusedGroupsSendNoSignalForAnyAction(t *testing.T) {
	cases := []struct {
		name     string
		group    int
		expected string
	}{
		{"own group of the daemon", 4242, "heavy: refusing to signal process group 4242: it is the daemon's own group"},
		{"group 0", 0, "heavy: refusing to signal process group 0"},
		{"group 1", 1, "heavy: refusing to signal process group 1"},
	}
	actions := map[string]func(procs.Tracker, procs.Run) error{
		"Pause":  func(tracker procs.Tracker, run procs.Run) error { return tracker.Pause(run) },
		"Resume": func(tracker procs.Tracker, run procs.Run) error { return tracker.Resume(run) },
		"Terminate": func(tracker procs.Tracker, run procs.Run) error {
			_, err := tracker.Terminate(run)
			return err
		},
	}
	for _, c := range cases {
		for actionName, action := range actions {
			t.Run(c.name+"/"+actionName, func(t *testing.T) {
				f := newFixture()
				f.host.own = 4242
				paused := dbContainer
				paused.Paused = true
				f.docker.containers = []procs.Container{paused}
				run := standardRun()
				run.ProcessGroup = c.group
				assertErrorText(t, action(f.tracker, run), c.expected)
				assertEvents(t, f, nil)
			})
		}
	}
}

func TestCoStartedExcludesContainersStartedBeforeOrOfOtherWorktrees(t *testing.T) {
	f := newFixture()
	f.docker.containers = []procs.Container{dbContainer, oldContainer, otherContainer, siblingContainer, nestedContainer}

	containers, err := f.tracker.CoStarted(standardRun())

	if want := []procs.Container{dbContainer, nestedContainer}; err != nil || !reflect.DeepEqual(containers, want) {
		t.Fatalf("CoStarted = %#v, %v; want %#v", containers, err, want)
	}
}

func TestTerminateSendsTermThenContThenKillsSurvivorsAfterTenSeconds(t *testing.T) {
	cases := []struct {
		name         string
		hostSurvives bool
		vmSurvivors  []int
		wantKills    []string
	}{
		{"host and PID 11 survive", true, []int{11}, []string{"host 4242 KILL", "vm run-1 KILL [11]"}},
		{"only the host survives", true, nil, []string{"host 4242 KILL"}},
		{"only PID 10 and 11 survive", false, []int{10, 11}, []string{"vm run-1 KILL [10 11]"}},
		{"everything ended after TERM", false, nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture()
			if !c.hostSurvives {
				f.host.diesOn = syscall.SIGTERM
			} else {
				f.host.diesOn = syscall.SIGKILL
			}
			for _, pid := range []int{10, 11} {
				if !contains(c.vmSurvivors, pid) {
					f.vm.diesOnTerm = append(f.vm.diesOnTerm, pid)
				}
			}
			paused := dbContainer
			paused.Paused = true
			running := nestedContainer
			f.docker.containers = []procs.Container{paused, running, oldContainer}

			done, err := f.tracker.Terminate(standardRun())
			if err != nil {
				t.Fatalf("Terminate returned error: %v", err)
			}
			select {
			case duration := <-f.clock.calls:
				if duration != 10*time.Second {
					t.Fatalf("kill phase waits %v, want 10s", duration)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Terminate never asked the clock for the kill delay")
			}
			assertEvents(t, f, []string{
				"host 4242 TERM", "vm run-1 TERM [10 11]",
				"host 4242 CONT", "vm run-1 CONT [10 11]",
				"docker unpause db-test-1",
			})
			select {
			case <-done:
				t.Fatal("channel closed before the kill delay elapsed")
			default:
			}

			f.clock.fire <- time.Now()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("channel did not close after the kill phase")
			}
			expected := []string{
				"host 4242 TERM", "vm run-1 TERM [10 11]",
				"host 4242 CONT", "vm run-1 CONT [10 11]",
				"docker unpause db-test-1",
			}
			assertEvents(t, f, append(expected, c.wantKills...))
		})
	}
}

func TestTerminateWithoutVMProcessesSendsNoHelperRequest(t *testing.T) {
	f := newFixture()
	f.vm.processes = nil
	f.host.diesOn = syscall.SIGTERM
	done, err := f.tracker.Terminate(standardRun())
	if err != nil {
		t.Fatalf("Terminate returned error: %v", err)
	}
	select {
	case <-f.clock.calls:
	case <-time.After(2 * time.Second):
		t.Fatal("Terminate never asked the clock for the kill delay")
	}
	f.clock.fire <- time.Now()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("channel did not close after the kill phase")
	}
	assertEvents(t, f, []string{"host 4242 TERM", "host 4242 CONT"})
}

func TestTerminateHostFailureStillTermsVM(t *testing.T) {
	f := newFixture()
	hostFailure := errors.New("operation not permitted")
	f.host.signalErr = hostFailure
	_, err := f.tracker.Terminate(standardRun())
	if !errors.Is(err, hostFailure) {
		t.Fatalf("error %v does not wrap %v", err, hostFailure)
	}
	events := f.recorder.snapshot()
	if len(events) == 0 || events[0] != "host 4242 TERM" {
		t.Fatalf("first call = %#v, want host TERM first", events)
	}
}

func TestRSSAddsHostVMOutsideContainersAndCoStartedContainers(t *testing.T) {
	f := newFixture()
	f.host.rss = 100 * mebibyte
	f.vm.processes = []helper.Process{
		{PID: 10, RSSBytes: 120 * mebibyte, Container: ""},
		{PID: 13, RSSBytes: 80 * mebibyte, Container: "c-foreign"},
		{PID: 12, RSSBytes: 50 * mebibyte, Container: "c-db1"},
	}
	f.vm.containers = []helper.Container{
		{ID: "c-db1", RSSBytes: 300 * mebibyte},
		{ID: "c-other", RSSBytes: 999 * mebibyte},
	}
	f.docker.containers = []procs.Container{dbContainer, otherContainer}
	rss, err := f.tracker.RSS(standardRun())
	if err != nil {
		t.Fatalf("RSS returned error: %v", err)
	}
	if rss != 600*mebibyte {
		t.Fatalf("RSS = %d bytes, want %d", rss, 600*mebibyte)
	}
}

func TestRSSReturnsHostPartWithErrorWhenVMFails(t *testing.T) {
	failures := map[string]func(*fakeVM){
		"find":       func(vm *fakeVM) { vm.findErr = errors.New("docker not reachable") },
		"containers": func(vm *fakeVM) { vm.containersErr = errors.New("docker not reachable") },
	}
	for name, fail := range failures {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			f.host.rss = 100 * mebibyte
			f.docker.containers = []procs.Container{dbContainer}
			fail(f.vm)
			rss, err := f.tracker.RSS(standardRun())
			assertErrorText(t, err, "heavy: VM processes of run run-1 not measured: docker not reachable")
			if rss != 100*mebibyte {
				t.Fatalf("RSS = %d bytes, want the host part %d", rss, 100*mebibyte)
			}
		})
	}
}

func TestRSSReportsHostFailure(t *testing.T) {
	f := newFixture()
	hostFailure := fmt.Errorf("ps failed")
	f.host.rssErr = hostFailure
	_, err := f.tracker.RSS(standardRun())
	if !errors.Is(err, hostFailure) {
		t.Fatalf("error %v does not wrap %v", err, hostFailure)
	}
}
