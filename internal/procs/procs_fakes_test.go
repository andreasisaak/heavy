package procs_test

import (
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/helper"
	"github.com/andreasisaak/heavy/internal/procs"
)

const mebibyte = int64(1024 * 1024)

var runStart = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

func standardRun() procs.Run {
	return procs.Run{ID: "run-1", ProcessGroup: 4242, Worktree: "/work/app", Started: runStart}
}

var (
	dbContainer      = procs.Container{ID: "c-db1", Name: "db-test-1", WorkingDirectory: "/work/app", Started: runStart.Add(time.Minute)}
	nestedContainer  = procs.Container{ID: "c-nested", Name: "nested-1", WorkingDirectory: "/work/app/docker", Started: runStart.Add(2 * time.Minute)}
	oldContainer     = procs.Container{ID: "c-old", Name: "db-old", WorkingDirectory: "/work/app", Started: runStart.Add(-time.Minute)}
	otherContainer   = procs.Container{ID: "c-other", Name: "other-1", WorkingDirectory: "/work/other", Started: runStart.Add(time.Minute)}
	siblingContainer = procs.Container{ID: "c-sibling", Name: "app-two-1", WorkingDirectory: "/work/app-two", Started: runStart.Add(time.Minute)}
)

// recorder keeps every signal, pause and unpause in call order across the host, VM and docker fakes.
type recorder struct {
	mutex  sync.Mutex
	events []string
}

func (recorder *recorder) add(format string, arguments ...any) {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	recorder.events = append(recorder.events, fmt.Sprintf(format, arguments...))
}

func (recorder *recorder) snapshot() []string {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	return append([]string(nil), recorder.events...)
}

func signalName(signal syscall.Signal) string {
	switch signal {
	case syscall.SIGSTOP:
		return "STOP"
	case syscall.SIGCONT:
		return "CONT"
	case syscall.SIGTERM:
		return "TERM"
	case syscall.SIGKILL:
		return "KILL"
	}
	return fmt.Sprintf("signal-%d", int(signal))
}

type fakeHost struct {
	recorder  *recorder
	own       int
	rss       int64
	rssErr    error
	signalErr error
	// alive is the state of the group; a signal named in diesOn ends it.
	mutex  sync.Mutex
	alive  bool
	diesOn syscall.Signal
	// zombies makes the ended group answer as macOS does before the parent reaps it.
	zombies bool
}

func (host *fakeHost) Signal(group int, signal syscall.Signal) error {
	host.recorder.add("host %d %s", group, signalName(signal))
	if host.signalErr != nil {
		return host.signalErr
	}
	host.mutex.Lock()
	defer host.mutex.Unlock()
	// probe 2026-11-08: kill(2) on a group whose processes all ended fails with ESRCH, as SystemHost reports it;
	// macOS answers EPERM while they are zombies their parent has not reaped (CI run 43248801419)
	if !host.alive {
		ended := syscall.ESRCH
		if host.zombies {
			ended = syscall.EPERM
		}
		return fmt.Errorf("heavy: sending %s to process group %d: %w", signalName(signal), group, ended)
	}
	if signal == host.diesOn {
		host.alive = false
	}
	return nil
}

func (host *fakeHost) Alive(group int) bool {
	host.mutex.Lock()
	defer host.mutex.Unlock()
	return host.alive
}

func (host *fakeHost) RSS(group int) (int64, error) { return host.rss, host.rssErr }
func (host *fakeHost) OwnGroup() int                { return host.own }

type fakeVM struct {
	recorder      *recorder
	mutex         sync.Mutex
	processes     []helper.Process
	findErr       error
	containers    []helper.Container
	containersErr error
	signalErr     error
	// diesOnTerm lists the PIDs that end when they get TERM; every PID ends on KILL.
	diesOnTerm []int
}

func (vm *fakeVM) Find(tag string) ([]helper.Process, error) {
	vm.mutex.Lock()
	defer vm.mutex.Unlock()
	if vm.findErr != nil {
		return nil, vm.findErr
	}
	return append([]helper.Process(nil), vm.processes...), nil
}

func (vm *fakeVM) Containers() ([]helper.Container, error) {
	return vm.containers, vm.containersErr
}

func (vm *fakeVM) Signal(tag, signal string, pids []int) (helper.Signaled, error) {
	vm.recorder.add("vm %s %s %v", tag, signal, pids)
	if vm.signalErr != nil {
		return helper.Signaled{}, vm.signalErr
	}
	vm.mutex.Lock()
	defer vm.mutex.Unlock()
	for _, pid := range pids {
		if signal == "KILL" || (signal == "TERM" && contains(vm.diesOnTerm, pid)) {
			vm.remove(pid)
		}
	}
	return helper.Signaled{Sent: pids}, nil
}

func (vm *fakeVM) remove(pid int) {
	kept := vm.processes[:0:0]
	for _, process := range vm.processes {
		if process.PID != pid {
			kept = append(kept, process)
		}
	}
	vm.processes = kept
}

func contains(values []int, wanted int) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

type fakeDocker struct {
	recorder   *recorder
	containers []procs.Container
	listErr    error
	pauseErr   error
	unpauseErr error
}

func (docker *fakeDocker) Containers() ([]procs.Container, error) {
	return docker.containers, docker.listErr
}

func (docker *fakeDocker) Pause(name string) error {
	docker.recorder.add("docker pause %s", name)
	return docker.pauseErr
}

func (docker *fakeDocker) Unpause(name string) error {
	docker.recorder.add("docker unpause %s", name)
	return docker.unpauseErr
}

// fakeClock hands out one channel that the test fires; calls reports each requested duration.
type fakeClock struct {
	calls chan time.Duration
	fire  chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{calls: make(chan time.Duration, 8), fire: make(chan time.Time, 1)}
}

func (clock *fakeClock) After(duration time.Duration) <-chan time.Time {
	clock.calls <- duration
	return clock.fire
}

type fixture struct {
	recorder *recorder
	host     *fakeHost
	vm       *fakeVM
	docker   *fakeDocker
	clock    *fakeClock
	tracker  procs.Tracker
}

// newFixture builds a tracker over fakes: host group 4242 alive, VM PIDs 10 and 11 tagged, no containers.
func newFixture() *fixture {
	recorder := &recorder{}
	host := &fakeHost{recorder: recorder, own: 777, alive: true}
	vm := &fakeVM{recorder: recorder, processes: []helper.Process{{PID: 10}, {PID: 11}}}
	docker := &fakeDocker{recorder: recorder}
	clock := newFakeClock()
	return &fixture{
		recorder: recorder, host: host, vm: vm, docker: docker, clock: clock,
		tracker: procs.Tracker{
			Host: host, VM: vm, Docker: docker, Clock: clock,
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
	}
}

func assertEvents(t *testing.T, fixture *fixture, expected []string) {
	t.Helper()
	actual := fixture.recorder.snapshot()
	if len(actual) == 0 && len(expected) == 0 {
		return
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("recorded calls differ\n got: %#v\nwant: %#v", actual, expected)
	}
}

func assertErrorText(t *testing.T, err error, expected string) {
	t.Helper()
	if err == nil {
		t.Fatalf("got no error, want %q", expected)
	}
	if err.Error() != expected {
		t.Fatalf("error text differs\n got: %q\nwant: %q", err.Error(), expected)
	}
}
