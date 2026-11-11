package guard_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/andreasisaak/heavy/internal/protocol"
)

const pollAdvice = "You will be woken when it ends, so the queue needs no check: keep working on files the run does not read, " +
	"or end your turn when no such work is left."

func otherRun() protocol.RunStatus {
	return runOf("running", 1_800_000_000-240, "/sites/other", "make test-complete")
}

// test_heavy_guard.py:926
func TestPollWhileTheOwnRunWaitsIsDeniedWithItsState(t *testing.T) {
	_, worktree := listed(t)
	own := protocol.RunStatus{State: "waiting", Position: 1, PID: 2, Since: 1_800_000_000 - 360, Directory: worktree, Command: "make ci", Session: session}
	for _, command := range []string{
		"heavy --status",
		"heavy --status --json | jq '.[0]'",
		"until ! heavy --status | grep -q waiting; do sleep 30; done",
	} {
		t.Run(command, func(t *testing.T) {
			got := newWorld(t).withQueue(otherRun(), own).run(t, bash(command, worktree, false))

			check(t, "answer", got, denial("Your heavy run `make ci` waits at position 1 for 6 min. Running now: `make test-complete` in /sites/other for 4 min. "+pollAdvice))
		})
	}
}

// test_heavy_guard.py:945
func TestPollWhileTheOwnRunHoldsTheLockNamesOnlyThatRun(t *testing.T) {
	_, worktree := listed(t)
	own := protocol.RunStatus{State: "running", PID: 2, Since: 1_800_000_000 - 180, Directory: worktree, Command: "make ci", Session: session}

	got := newWorld(t).withQueue(own).run(t, bash("heavy --status", filepath.Join(worktree, "app"), false))

	check(t, "answer", got, denial("Your heavy run `make ci` runs for 3 min. "+pollAdvice))
}

// test_heavy_guard.py:960; a record without a session carries the empty session in the daemon's queue.
func TestPollWhileAnotherSessionRunsInTheSameCheckoutPasses(t *testing.T) {
	_, worktree := listed(t)
	for _, other := range []string{"4403ca6f-other", ""} {
		t.Run(other, func(t *testing.T) {
			run := runOf("running", 1_800_000_000-60, worktree, "make ci")
			run.Session = other

			check(t, "answer", newWorld(t).withQueue(run).run(t, bash("heavy --status", worktree, false)), nil)
		})
	}
}

// test_heavy_guard.py:967
func TestPollWithoutAnOwnRunPasses(t *testing.T) {
	_, worktree := listed(t)
	for _, queue := range [][]protocol.RunStatus{nil, {otherRun()}} {
		t.Run(show(queue), func(t *testing.T) {
			check(t, "answer", newWorld(t).withQueue(queue...).run(t, bash("heavy --status", worktree, false)), nil)
		})
	}
}

func TestPollWhoseQueueCannotBeReadIsDenied(t *testing.T) {
	_, worktree := listed(t)
	current := newWorld(t)
	current.queueError = errors.New("heavy: reading the queue from the daemon: EOF")

	got := current.run(t, bash("heavy --status", worktree, false))

	check(t, "answer", got, denial("heavy: reading the queue from the daemon: EOF"))
	check(t, "queue reads", *current.queueCalls, 1)
}

func TestLightCallReadsNoQueue(t *testing.T) {
	_, worktree := listed(t)
	current := newWorld(t)
	current.queueError = errors.New("heavy: the queue must stay unread")

	check(t, "answer", current.run(t, bash("git status", worktree, false)), nil)
	check(t, "queue reads", *current.queueCalls, 0)
}

// test_heavy_guard.py:571
func TestQueueStatusNamesTheRunningCommandAndCountsTheWaiters(t *testing.T) {
	_, worktree := listed(t)
	current := newWorld(t).withQueue(
		runOf("running", 1_800_000_000-125, "/sites/main-feature", "make ci"),
		protocol.RunStatus{State: "waiting", Position: 1, PID: 2, Since: 1_800_000_000 - 30, Directory: "/sites/main", Command: "make test"},
	)

	got := current.run(t, bash("make ci", worktree, true))

	status := "Running now: `make ci` in /sites/main-feature for 2 min. Waiting ahead of you: 1."
	check(t, "answer", got, rewrite(rewritten(fakeHeavy+" make ci"), contextOf("`make ci`", inFront, status, fakeHeavy+" make ci")))
}

// test_heavy_guard.py:580
func TestQueueStatusCountsWaitersWhileTheLockChangesHands(t *testing.T) {
	_, worktree := listed(t)
	current := newWorld(t).withQueue(protocol.RunStatus{State: "waiting", Position: 1, PID: 2, Since: 1_800_000_000 - 30, Directory: "/sites/main", Command: "make test"})

	got := current.run(t, bash("make ci", worktree, true))

	check(t, "answer", got, rewrite(rewritten(fakeHeavy+" make ci"), contextOf("`make ci`", inFront, "Waiting ahead of you: 1.", fakeHeavy+" make ci")))
}

// test_heavy_guard.py:586
func TestQueueStatusIsFreeWithoutRuns(t *testing.T) {
	_, worktree := listed(t)

	got := guardOf(t, "make ci", worktree, true)

	check(t, "answer", got, rewrite(rewritten(fakeHeavy+" make ci"), contextOf("`make ci`", inFront, free, fakeHeavy+" make ci")))
}

func TestQueueThatCannotBeReadDeniesTheRewrite(t *testing.T) {
	_, worktree := listed(t)
	current := newWorld(t)
	current.queueError = errors.New("heavy: reading the daemon's hello: EOF")

	got := current.run(t, bash("make ci", worktree, false))

	check(t, "answer", got, denial("heavy: reading the daemon's hello: EOF"))
	check(t, "queue reads", *current.queueCalls, 1)
}

func TestLongCommandOfTheOwnRunIsCutAtSixtyCharacters(t *testing.T) {
	_, worktree := listed(t)
	command := "make ci ÄÖÜ " + "abcdefghij" + "abcdefghij" + "abcdefghij" + "abcdefghij" + "abcdefghij"
	own := protocol.RunStatus{State: "running", PID: 2, Since: 1_800_000_000 + 30, Directory: worktree, Command: command, Session: session}

	got := newWorld(t).withQueue(own).run(t, bash("heavy --status", worktree, false))

	check(t, "answer", got, denial("Your heavy run `make ci ÄÖÜ abcdefghijabcdefghijabcdefghijabcdefghijabcdefg…` runs for -1 min. "+pollAdvice))
}

// V12: a paused own run is still open, so a poll is denied with its state; it holds no lock, so the running run
// is named too
func TestPollWhileTheOwnRunIsPausedIsDeniedWithItsState(t *testing.T) {
	_, worktree := listed(t)
	own := protocol.RunStatus{State: "paused", PID: 2, Since: 1_800_000_000 - 540, Directory: worktree, Command: "make ci", Session: session}

	got := newWorld(t).withQueue(otherRun(), own).run(t, bash("heavy --status", worktree, false))

	check(t, "answer", got, denial("Your heavy run `make ci` is paused, started 9 min ago. Running now: `make test-complete` in /sites/other for 4 min. "+pollAdvice))
}
