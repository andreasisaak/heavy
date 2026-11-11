package sampler_test

import (
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/helper"
	"github.com/andreasisaak/heavy/internal/sampler"
)

var fixtureSample = helper.Sample{MemAvailableBytes: 6291456 * 1024, SwapOutBytes: 25 * int64(os.Getpagesize()), Load1: 2.5, Cores: 8}

// plan 3.1: without the image the daemon builds it offline FROM scratch, then starts the helper once
func TestVMBuildsTheImageThenStartsTheHelperOnce(t *testing.T) {
	fake := newFakeDocker(t, dockerBehaviour{})
	vm := newVM(t, fake, &vmClock{now: time.Unix(1000, 0)}, 2*time.Second)

	first, firstError := vm.Sample()
	second, secondError := vm.Sample()

	if firstError != nil || secondError != nil || !reflect.DeepEqual(first, fixtureSample) || !reflect.DeepEqual(second, fixtureSample) {
		t.Fatalf("samples = %#v, %v and %#v, %v; want %#v twice", first, firstError, second, secondError, fixtureSample)
	}
	want := []string{started[0], started[1], "build -q -t heavy-probe-helper:1 -f - <context>", started[2], started[3]}
	if got := fake.calls(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("docker calls = %q, want %q", got, want)
	}
	dockerfile, err := os.ReadFile(fake.state + "/dockerfile")
	if want := "FROM scratch\nCOPY heavy-linux /heavy\nENTRYPOINT [\"/heavy\", \"--helper\"]\n"; err != nil || string(dockerfile) != want {
		t.Fatalf("Dockerfile = %q, %v; want %q", dockerfile, err, want)
	}
}

func TestVMWithItsImageStartsWithoutABuild(t *testing.T) {
	fake := newFakeDocker(t, dockerBehaviour{})
	fake.haveImage(t)
	vm := newVM(t, fake, &vmClock{now: time.Unix(1000, 0)}, 2*time.Second)

	processes, err := vm.Find("nobody")

	if err != nil || !reflect.DeepEqual(processes, []helper.Process{}) {
		t.Fatalf("Find = %#v, %v; want an empty list", processes, err)
	}
	if got := fake.calls(t); !reflect.DeepEqual(got, started) {
		t.Fatalf("docker calls = %q, want %q", got, started)
	}
}

// plan 3.1 and EC-2: docker that fails or hangs is not reachable; heavy tries again after 10 s at the earliest
func TestVMWithoutAnAnsweringDockerIsNotReachable(t *testing.T) {
	for name, version := range map[string]string{"failing": "echo 'Cannot connect to the Docker daemon' >&2; exit 1", "hanging": "exec sleep 30"} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeDocker(t, dockerBehaviour{version: version})
			clock := &vmClock{now: time.Unix(1000, 0)}
			vm := newVM(t, fake, clock, 300*time.Millisecond)

			_, first := vm.Sample()
			clock.advance(9 * time.Second)
			_, second := vm.Containers()
			clock.advance(time.Second)
			_, third := vm.Sample()

			for _, err := range []error{first, second, third} {
				if !errors.Is(err, sampler.ErrNotReachable) || err.Error() != "docker not reachable" {
					t.Fatalf("errors = %v, %v, %v; want docker not reachable", first, second, third)
				}
			}
			want := []string{started[0], started[0]}
			if got := fake.calls(t); !reflect.DeepEqual(got, want) {
				t.Fatalf("docker calls = %q, want %q", got, want)
			}
		})
	}
}

// plan 3.1: a helper that ended is started again, at most every 10 s, so a helper that keeps dying costs no docker
// run every 2 s
func TestVMStartsTheHelperAgainAfterItEnded(t *testing.T) {
	fake := newFakeDocker(t, dockerBehaviour{run: "head -n 1 | {helper}"})
	fake.haveImage(t)
	clock := &vmClock{now: time.Unix(1000, 0)}
	vm := newVM(t, fake, clock, 2*time.Second)

	_, first := vm.Sample()
	_, second := vm.Sample()
	clock.advance(9 * time.Second)
	_, early := vm.Sample()
	clock.advance(time.Second)
	_, third := vm.Sample()

	ended := "heavy: the helper in the VM ended"
	if first != nil || second == nil || second.Error() != ended || early == nil || early.Error() != ended || third != nil {
		t.Fatalf("errors = %v, %v, %v, %v; want the end of the helper twice, then a new one", first, second, early, third)
	}
	if got, want := fake.calls(t), append(append([]string{}, started...), started...); !reflect.DeepEqual(got, want) {
		t.Fatalf("docker calls = %q, want %q", got, want)
	}
}

// a helper that does not answer in time is removed, and the next request starts a new one
func TestVMRemovesAHelperThatDoesNotAnswer(t *testing.T) {
	fake := newFakeDocker(t, dockerBehaviour{run: "exec sleep 30"})
	fake.haveImage(t)
	vm := newVM(t, fake, &vmClock{now: time.Unix(1000, 0)}, 300*time.Millisecond)

	_, err := vm.Sample()

	if err == nil || err.Error() != "heavy: the helper in the VM did not answer within 300ms" {
		t.Fatalf("Sample error = %v", err)
	}
	if got, want := fake.calls(t), append(append([]string{}, started...), "rm -f heavy-probe-helper-1"); !reflect.DeepEqual(got, want) {
		t.Fatalf("docker calls = %q, want %q", got, want)
	}
}

func TestVMWithoutTheLinuxBinaryIsAnError(t *testing.T) {
	fake := newFakeDocker(t, dockerBehaviour{})
	if err := os.Remove(fake.linux); err != nil {
		t.Fatal(err)
	}
	vm := newVM(t, fake, &vmClock{now: time.Unix(1000, 0)}, 2*time.Second)

	_, err := vm.Sample()

	if want := "heavy: " + fake.linux + " is missing, so heavy measures no VM"; err == nil || err.Error() != want {
		t.Fatalf("Sample error = %v, want %s", err, want)
	}
}

func TestVMWithAFailingBuildNamesItsOutput(t *testing.T) {
	fake := newFakeDocker(t, dockerBehaviour{build: `echo "no space left on device" >&2; exit 1`})
	vm := newVM(t, fake, &vmClock{now: time.Unix(1000, 0)}, 2*time.Second)

	_, err := vm.Sample()

	if want := "heavy: building the image heavy-probe-helper:1 failed: exit status 1: no space left on device"; err == nil || err.Error() != want {
		t.Fatalf("Sample error = %v, want %s", err, want)
	}
}

// an error answer of the helper reaches the caller as it is
func TestVMPassesTheErrorOfTheHelperOn(t *testing.T) {
	fake := newFakeDocker(t, dockerBehaviour{})
	fake.haveImage(t)
	vm := newVM(t, fake, &vmClock{now: time.Unix(1000, 0)}, 2*time.Second)

	_, err := vm.Find("")

	if err == nil || err.Error() != "heavy: find needs a tag" {
		t.Fatalf("Find error = %v", err)
	}
}

// Close ends the helper through the end of its input; the container goes with it (--rm)
func TestVMCloseEndsTheHelper(t *testing.T) {
	fake := newFakeDocker(t, dockerBehaviour{})
	fake.haveImage(t)
	vm := newVM(t, fake, &vmClock{now: time.Unix(1000, 0)}, 2*time.Second)
	if _, err := vm.Sample(); err != nil {
		t.Fatal(err)
	}

	err := vm.Close()

	if err != nil || !reflect.DeepEqual(fake.calls(t), started) {
		t.Fatalf("Close = %v, docker calls %q", err, fake.calls(t))
	}
	if _, err := vm.Sample(); err == nil || err.Error() != "heavy: the VM sampler is closed" {
		t.Fatalf("Sample after Close = %v", err)
	}
}

// plan 3.2: a signal goes through the helper, which skips a PID without the tag
func TestVMSignalSkipsAProcessWithoutTheTag(t *testing.T) {
	fake := newFakeDocker(t, dockerBehaviour{})
	fake.haveImage(t)
	vm := newVM(t, fake, &vmClock{now: time.Unix(1000, 0)}, 2*time.Second)

	signaled, err := vm.Signal("r1", "STOP", []int{4194301})

	if want := (helper.Signaled{Sent: []int{}, Skipped: []int{4194301}}); err != nil || !reflect.DeepEqual(signaled, want) {
		t.Fatalf("Signal = %#v, %v; want %#v", signaled, err, want)
	}
}
