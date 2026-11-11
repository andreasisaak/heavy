package stophook_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// hooks/test_heavy_stop.py: the first stop of a waiting heavy run is denied with its cost
func TestFirstStopOfAWaitingHeavyRunIsDeniedWithItsCost(t *testing.T) {
	f := newFixture(t)

	got := f.stop(t, f.input(t, "task_id", queuedTask), []protocol.RunStatus{own("waiting")}, now)

	expect(t, "first stop", got, denial(t, "Task "+queuedTask+" is a heavy run of this session: `make ci` waits at position 2 for 5 min. "+
		"Stopping it gives up its place in the queue, and a new start waits at the end. "+advice))
}

// hooks/test_heavy_stop.py: the first stop of a running heavy run names the lost work; KillShell names it shell_id
func TestFirstStopOfARunningHeavyRunNamesTheLostWork(t *testing.T) {
	f := newFixture(t)

	got := f.stop(t, f.input(t, "shell_id", queuedTask), []protocol.RunStatus{own("running")}, now)

	expect(t, "first stop", got, denial(t, "Task "+queuedTask+" is a heavy run of this session: `make ci` runs for 5 min. "+
		"Stopping it throws away the minutes it has run and makes room for the next run. "+advice))
}

// hooks/test_heavy_stop.py: the second stop within ten minutes passes, and the third meets the bump again
func TestSecondStopWithinTenMinutesPassesAndTheThirdMeetsTheBumpAgain(t *testing.T) {
	f := newFixture(t)
	queue := []protocol.RunStatus{own("waiting")}
	input := f.input(t, "task_id", queuedTask)
	f.stop(t, input, queue, now)

	got := []string{f.stop(t, input, queue, now.Add(120*time.Second)), f.stop(t, input, queue, now.Add(180*time.Second))}

	want := []string{"", denial(t, "Task "+queuedTask+" is a heavy run of this session: `make ci` waits at position 2 for 8 min. "+
		"Stopping it gives up its place in the queue, and a new start waits at the end. "+advice)}
	expect(t, "second stop", got[0], want[0])
	expect(t, "third stop", got[1], want[1])
}

// hooks/test_heavy_stop.py: a stop repeated after ten minutes meets the bump again
func TestStopRepeatedAfterTenMinutesMeetsTheBumpAgain(t *testing.T) {
	f := newFixture(t)
	queue := []protocol.RunStatus{own("waiting")}
	input := f.input(t, "task_id", queuedTask)
	f.stop(t, input, queue, now)

	got := f.stop(t, input, queue, now.Add(601*time.Second))

	expect(t, "stop after 601 s", got, denial(t, "Task "+queuedTask+" is a heavy run of this session: `make ci` waits at position 2 for 15 min. "+
		"Stopping it gives up its place in the queue, and a new start waits at the end. "+advice))
}

// hooks/test_heavy_stop.py: the stop passes for other tasks and ended runs; the queue is asked only for a task the
// guard queued
func TestStopPassesForOtherTasksAndEndedRuns(t *testing.T) {
	foreign := own("running")
	foreign.Session = "4403ca6f-other"
	cases := []struct {
		name       string
		task       string
		queue      []protocol.RunStatus
		queueCalls int
	}{
		{"task the guard did not queue", otherTask, []protocol.RunStatus{own("waiting")}, 0},
		{"queued task whose run ended", queuedTask, nil, 1},
		{"task missing from the transcript", "bunknown1", []protocol.RunStatus{own("running")}, 0},
		{"queued task whose run finished", queuedTask, []protocol.RunStatus{{State: "finished", Command: "make ci", Session: session}}, 1},
		// hooks/test_heavy_stop.py: the stop while only another session runs in the checkout passes
		{"only another session runs", queuedTask, []protocol.RunStatus{foreign}, 1},
	}
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			f := newFixture(t)

			got := f.stop(t, f.input(t, "task_id", current.task), current.queue, now)

			expect(t, "stop", got, "")
			if f.queueCalls != current.queueCalls {
				t.Fatalf("the hook asked for the queue %d times, want %d", f.queueCalls, current.queueCalls)
			}
		})
	}
}

// hooks/test_heavy_stop.py: the stop without a transcript fails with its path; Go denies with the path (design G15)
func TestStopWithoutATranscriptIsDeniedWithItsPath(t *testing.T) {
	f := newFixture(t)
	f.transcript = filepath.Join(f.directory, "missing.jsonl")

	got := f.stop(t, f.input(t, "task_id", queuedTask), []protocol.RunStatus{own("waiting")}, now)

	expect(t, "stop", got, denial(t, "heavy: found no transcript at "+f.transcript))
}

// plan 4.2: a task id that is no plain id never becomes a file name
func TestStopOfATaskIdThatIsNoPlainIdIsDenied(t *testing.T) {
	f := newFixture(t)

	got := f.stop(t, f.input(t, "task_id", "../x"), []protocol.RunStatus{own("waiting")}, now)

	expect(t, "stop", got, denial(t, "heavy: task id ../x is not a plain id"))
	if f.queueCalls != 0 {
		t.Fatalf("the hook asked for the queue %d times, want 0", f.queueCalls)
	}
}

// a TaskStop without task_id and shell_id is denied with what it got
func TestStopWithoutATaskIdIsDenied(t *testing.T) {
	f := newFixture(t)

	got := f.stop(t, f.input(t, "name", queuedTask), nil, now)

	expect(t, "stop", got, denial(t, `heavy: expected task_id or shell_id in the input of TaskStop, got {"name":"bq7heavy1"}`))
}

// a paused run has run its minutes like a running one
func TestFirstStopOfAPausedHeavyRunNamesTheLostWork(t *testing.T) {
	f := newFixture(t)

	got := f.stop(t, f.input(t, "task_id", queuedTask), []protocol.RunStatus{own("paused")}, now)

	expect(t, "first stop", got, denial(t, "Task "+queuedTask+" is a heavy run of this session: `make ci` is paused, started 5 min ago. "+
		"Stopping it throws away the minutes it has run and makes room for the next run. "+advice))
}

// hooks/heavy_queue.py: a command longer than 60 characters is cut after 59 code points; the answer keeps & and
// non-ASCII characters as they are, like json.dumps(…, ensure_ascii=False)
func TestLongCommandIsCutToSixtyCharactersInTheDenial(t *testing.T) {
	f := newFixture(t)
	run := own("running")
	run.Command = "make ci && " + strings.Repeat("ü", 60)
	waiting := own("waiting")
	waiting.Since = now.Unix() - 61

	got := f.stop(t, f.input(t, "task_id", queuedTask), []protocol.RunStatus{run, waiting}, now)

	want := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"Task ` + queuedTask +
		" is a heavy run of this session: `make ci && " + strings.Repeat("ü", 48) + "…` runs for 5 min; `make ci` waits at position 2 for 1 min. " +
		"Stopping it throws away the minutes it has run and makes room for the next run. " + advice + `"}}` + "\n"
	expect(t, "stop", got, want)
}
