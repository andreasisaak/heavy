package shim_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The cases of hooks/test_heavy_shim.py whose calls wait in the queue or run inside a run.

// test_heavy_shim.py:90
func TestEntryExceptionLetsTheMarkedMakeCallPass(t *testing.T) {
	cases := []struct {
		name            string
		heavy, free     []string
		freeEnvironment []string
		heavyCommand    string
	}{
		{"word after the target", []string{"test-complete"}, []string{"test-complete", "TEST_WORKERS=12"}, nil, "make test-complete"},
		{"variable of the environment", []string{"test-complete", "TEST_WORKERS=1"}, []string{"test-complete"}, []string{"TEST_WORKERS=12"}, "make test-complete TEST_WORKERS=1"},
		{"word before the target", []string{"test-complete", "TEST_WORKERS=1 12"}, []string{"TEST_WORKERS=12", "test-complete"}, nil, "make test-complete TEST_WORKERS=1 12"},
	}
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			f := newFixture(t)
			f.writeProjectList(t, `{"commands": [{"command": "make test-complete", "unless": ["TEST_WORKERS=12"]}]}`)

			queued := f.call(t, f.project, f.environment(), "", "make", current.heavy...)
			passed := f.call(t, f.project, f.environment(current.freeEnvironment...), "", "make", current.free...)

			if queued.exitStatus != 0 {
				t.Fatalf("make %q = %+v, want exit status 0", current.heavy, queued)
			}
			matchOutput(t, "queued stdout", startedLine(current.heavyCommand)+regexp.QuoteMeta(printed("make", current.heavy...)), queued.stdout)
			want := outcome{stdout: printed("make", current.free...)}
			if passed != want {
				t.Fatalf("make %q = %+v, want %+v", current.free, passed, want)
			}
		})
	}
}

// test_heavy_shim.py:111
func TestHeavyCallWithoutHeavyWaitsInTheQueue(t *testing.T) {
	f := newFixture(t)

	got := f.call(t, f.project, f.environment(), "", "docker", heavyCall...)

	if got.exitStatus != 0 || got.stderr != netNote {
		t.Fatalf("docker %q = %+v, want exit status 0 and stderr %q", heavyCall, got, netNote)
	}
	command := "docker " + strings.Join(heavyCall, " ")
	entries := f.status(t)
	if len(entries) != 1 || entries[0].State != "finished" || entries[0].Command != command {
		t.Fatalf("queue after the run = %+v, want only the finished %q", entries, command)
	}
	// Deviation from 0.6 (FR-11): inside its run the docker call reaches the real docker with the run carried.
	carried := join(heavyCall[:2], []string{"-e", "HEAVY_RUN=" + entries[0].ID, "-e", "HEAVY_CPUS=" + strconv.Itoa(entries[0].Budget)}, heavyCall[2:])
	matchOutput(t, "stdout", startedLine(command)+regexp.QuoteMeta(printed("docker", carried...)), got.stdout)
}

// test_heavy_shim.py:123
func TestQueuedCallRecordsProgramNameAndProject(t *testing.T) {
	f := newFixture(t)
	hold := filepath.Join(f.scratch, "held")
	queued := start(t, filepath.Join(f.shims, "docker"), f.scratch,
		f.environment("FAKE_HOLD="+hold, "COMPOSE_PROJECT_DIRECTORY="+f.project), heavyCall...)
	eventually(t, "the fake docker starts", func() bool {
		_, err := os.Stat(hold)
		return err == nil
	})

	entries := f.status(t)

	want := []entry{{State: "running", Directory: f.project, Command: "docker " + strings.Join(heavyCall, " ")}}
	got := make([]entry, len(entries))
	for index, current := range entries {
		got[index] = entry{State: current.State, Directory: current.Directory, Command: current.Command}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("queue = %+v, want %+v", got, want)
	}
	if status := queued.wait(t); status != 0 {
		t.Fatalf("docker %q ended with %d, want 0; output %q", heavyCall, status, queued.output.String())
	}
}

// test_heavy_shim.py:143
func TestCallInsideAHeavyRunPassesWithoutWaitingAgain(t *testing.T) {
	f := newFixture(t)

	got := run(t, f.heavy, f.project, f.environment(), "", "sh", "-c", "command -v make && make ci")

	if got.exitStatus != 0 || got.stderr != "" {
		t.Fatalf("heavy sh -c … = %+v, want exit status 0 and no stderr", got)
	}
	matchOutput(t, "stdout", startedLine("sh -c command -v make && make ci")+
		regexp.QuoteMeta(filepath.Join(f.shims, "make")+"\n"+printed("make", "ci")), got.stdout)
}

// test_heavy_shim.py:161: 0.6 found its run through the lock holder among the ancestors; heavy finds it through
// HEAVY_RUN, so a call that carries the id of an active run passes from outside the run's processes too.
func TestCallWithTheIdOfAnActiveRunPassesWithoutWaiting(t *testing.T) {
	f := newFixture(t)
	active, _ := f.activeRun(t, "30.601")

	got := run(t, "/bin/sh", f.project, f.environment("HEAVY_RUN="+active.ID), "", "-c", "make ci")

	want := outcome{stdout: printed("make", "ci")}
	if got != want {
		t.Fatalf("make ci with HEAVY_RUN=%s = %+v, want %+v", active.ID, got, want)
	}
	if entries := f.status(t); len(entries) != 1 || entries[0].ID != active.ID {
		t.Fatalf("queue = %+v, want only the run %s", entries, active.ID)
	}
}

// test_heavy_shim.py:177
func TestListedMakeTargetReachedWithoutHeavyIsQueued(t *testing.T) {
	f := newFixture(t)

	got := f.call(t, f.project, f.environment(), "", "make", "FULL=1", "ci")

	note := "heavy: `make ci` is heavy and reached make without the queue, so it waits here in the machine-wide queue. " +
		"Start heavy calls with run_in_background, then the hook queues them up front.\n"
	if got.exitStatus != 0 || got.stderr != note {
		t.Fatalf("make FULL=1 ci = %+v, want exit status 0 and stderr %q", got, note)
	}
	matchOutput(t, "stdout", startedLine("make FULL=1 ci")+regexp.QuoteMeta(printed("make", "FULL=1", "ci")), got.stdout)
}
