package guard_test

import (
	"testing"

	"github.com/andreasisaak/heavy/internal/shell"
)

// test_heavy_guard.py:723
func TestCallThatStartsHeavyItselfRunsInTheBackgroundWithTheContext(t *testing.T) {
	command := "heavy env CHECK_WORKER=1 tests/Complete/checkall.sh"

	got := guardOf(t, command, temporaryDirectory(t), false)

	check(t, "answer", got, rewrite(rewritten(queued(command)), contextOf(
		"`env CHECK_WORKER=1 tests/Complete/checkall.sh`", direct+" and the hook sent it to the background", free, queued(command),
	)))
}

// test_heavy_guard.py:741; the runs sit in a subshell, where the hook names the agent like anywhere it can place
// heavy.
func TestBackgroundCallTheScannerCannotPlaceThatStartsHeavyGetsTheContext(t *testing.T) {
	command := "set -o pipefail; (heavy --directory /sites/main make test-complete && heavy env CHECK_WORKER=1 tests/Complete/checkall.sh) " +
		"2>&1 | tee /tmp/run.log"

	got := newWorld(t).run(t, agentSession(command, temporaryDirectory(t), true))

	named := "set -o pipefail; (heavy --agent test-author --directory /sites/main make test-complete && " +
		"heavy --agent test-author env CHECK_WORKER=1 tests/Complete/checkall.sh) 2>&1 | tee /tmp/run.log"
	check(t, "answer", got, rewrite(rewritten(named), contextOf(
		"`make test-complete`, `env CHECK_WORKER=1 tests/Complete/checkall.sh`", direct, free, named,
	)))
}

// test_heavy_guard.py:866
func TestFilterBehindAHeavyRunGoesWithANote(t *testing.T) {
	_, worktree := listed(t)
	for _, current := range []struct{ command, expected, removed string }{
		{"make ci 2>&1 | tail -80", fakeHeavy + " make ci 2>&1", "`| tail -80`"},
		{
			"set -o pipefail; make ci 2>&1 | tee /tmp/ci.log | grep -E 'FAIL|passed'",
			"set -o pipefail; " + fakeHeavy + " make ci 2>&1 | tee /tmp/ci.log",
			"`| grep -E 'FAIL|passed'`",
		},
	} {
		t.Run(current.command, func(t *testing.T) {
			got := guardOf(t, current.command, worktree, true)

			note := " The hook removed " + current.removed + " behind the heavy run, so the user can follow its output live; " +
				"read the end of the task output when you are woken."
			check(t, "answer", got, rewrite(rewritten(current.expected), contextOf("`make ci`", inFront, free, current.expected)+note))
		})
	}
}

// test_heavy_guard.py:897
func TestFilterAfterCdIntoTheProjectGoes(t *testing.T) {
	_, worktree := listed(t)
	command := "cd " + worktree + " && make ci && make test 2>&1 | tail -5"

	got := updated(guardOf(t, command, temporaryDirectory(t), false))

	stripped := "cd " + worktree + " && make ci && make test 2>&1"
	check(t, "updated input", got, rewritten(queued(fakeHeavy+" --directory "+worktree+" "+wholeShell+" "+shell.Quote(stripped))))
}
