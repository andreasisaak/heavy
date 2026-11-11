package procs_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/procs"
)

// critic 3.2: a run without an absolute worktree claims no container; its pause stops the host and the VM but
// pauses no container and names the reason
func TestRunWithoutAnAbsoluteWorktreeClaimsNoContainer(t *testing.T) {
	unlabeled := procs.Container{ID: "c-bare", Name: "bare-1", WorkingDirectory: "", Started: runStart.Add(time.Minute)}
	for _, worktree := range []string{"", "work/app"} {
		t.Run(worktree, func(t *testing.T) {
			f := newFixture()
			f.docker.containers = []procs.Container{dbContainer, unlabeled}
			run := standardRun()
			run.Worktree = worktree

			containers, listError := f.tracker.CoStarted(run)
			pauseError := f.tracker.Pause(run)

			want := "heavy: run run-1 has the worktree \"" + worktree + "\", expected an absolute path, so heavy claims no container"
			if listError == nil || listError.Error() != want || containers != nil {
				t.Fatalf("CoStarted = %#v, %v; want no containers and %q", containers, listError, want)
			}
			assertErrorText(t, pauseError, want)
			assertEvents(t, f, []string{"host 4242 STOP", "vm run-1 STOP [10 11]"})
		})
	}
}

// critic 3.2: a worktree written with a trailing slash still claims the containers below it
func TestWorktreeWithATrailingSlashClaimsItsContainers(t *testing.T) {
	f := newFixture()
	f.docker.containers = []procs.Container{nestedContainer}
	run := standardRun()
	run.Worktree = "/work/app/"

	containers, err := f.tracker.CoStarted(run)

	if err != nil || !reflect.DeepEqual(containers, []procs.Container{nestedContainer}) {
		t.Fatalf("CoStarted = %#v, %v; want the nested container", containers, err)
	}
}
