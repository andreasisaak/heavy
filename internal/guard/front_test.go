package guard_test

import (
	"strings"
	"testing"
)

// test_heavy_guard.py:104
func TestForegroundMakeCiInAWorktreeRunsQueuedInTheBackground(t *testing.T) {
	_, worktree := listed(t)

	got := guardOf(t, "make ci", worktree, false)

	command := queued(fakeHeavy + " make ci")
	check(t, "answer", got, rewrite(rewritten(command), contextOf("`make ci`", inFront+" and sent the command to the background", free, command)))
}

// test_heavy_guard.py:127
func TestContextNamesTheRunAhead(t *testing.T) {
	_, worktree := listed(t)
	current := newWorld(t).withQueue(runOf("running", 1_800_000_000-60, "/sites/other", "make test"))

	got := current.run(t, bash("make ci", worktree, false))

	command := queued(fakeHeavy + " make ci")
	status := "Running now: `make test` in /sites/other for 1 min."
	check(t, "answer", got, rewrite(rewritten(command), contextOf("`make ci`", inFront+" and sent the command to the background", status, command)))
}

// test_heavy_guard.py:153
func TestBackgroundCommandKeepsItsLoggingAndOnlyGainsHeavy(t *testing.T) {
	main, _ := listed(t)
	command := `set -o pipefail; make ci 2>&1 | tee /tmp/ci.log; exit_status=$?; echo "ci $exit_status"; exit $exit_status`

	got := updated(guardOf(t, command, main, true))

	check(t, "updated input", got, rewritten(`set -o pipefail; `+fakeHeavy+` make ci 2>&1 | tee /tmp/ci.log; exit_status=$?; echo "ci $exit_status"; exit $exit_status`))
}

// test_heavy_guard.py:162
func TestHeavyGoesRightInFrontOfTheHeavyCommand(t *testing.T) {
	_, worktree := listed(t)
	for _, current := range []struct{ command, expected string }{
		{"FULL=1 make ci", "FULL=1 " + fakeHeavy + " make ci"},
		{"make lint && make ci", "make lint && " + fakeHeavy + " make ci"},
		{"make test;", fakeHeavy + " make test"},
	} {
		t.Run(current.command, func(t *testing.T) {
			check(t, "updated input", updated(guardOf(t, current.command, worktree, false)), rewritten(queued(current.expected)))
		})
	}
}

// test_heavy_guard.py:176
func TestMakeDirectoryOptionNamesTheProject(t *testing.T) {
	_, worktree := listed(t)
	elsewhere := temporaryDirectory(t)

	got := updated(guardOf(t, "make -C "+worktree+" ci", elsewhere, false))

	check(t, "updated input", got, rewritten(queued(fakeHeavy+" --directory "+worktree+" make -C "+worktree+" ci")))
}

// test_heavy_guard.py:184
func TestCdBeforeTheCommandNamesTheProject(t *testing.T) {
	_, worktree := listed(t)
	elsewhere := temporaryDirectory(t)

	got := updated(guardOf(t, "cd "+worktree+" && make test", elsewhere, false))

	check(t, "updated input", got, rewritten(queued("cd "+worktree+" && "+fakeHeavy+" make test")))
}

// test_heavy_guard.py:224
func TestHeavyGoesAfterTheShellKeywordsInFrontOfTheCommand(t *testing.T) {
	_, worktree := listed(t)
	for _, command := range []string{
		"if make ci; then echo green; fi",
		"for attempt in 1 2; do make ci && break; done",
		"while ! make ci; do sleep 5; done",
		"until make ci; do sleep 5; done",
		"! make ci",
		"{ make ci; }",
		"if true; then echo; else make ci; fi",
	} {
		t.Run(command, func(t *testing.T) {
			want := rewritten(queued(strings.Replace(command, "make ci", fakeHeavy+" make ci", 1)))
			check(t, "updated input", updated(guardOf(t, command, worktree, false)), want)
		})
	}
}

// test_heavy_guard.py:242
func TestForegroundCommandSentToTheBackgroundByTheShellStaysValid(t *testing.T) {
	_, worktree := listed(t)

	got := updated(guardOf(t, "make ci &", worktree, false))

	check(t, "updated input", got, rewritten("{ "+fakeHeavy+` make ci & }; exit_status=$?; echo "heavy $exit_status"; exit $exit_status`))
}

// test_heavy_guard.py:322
func TestHeavyGoesInFrontOfWrappersAndShellStrings(t *testing.T) {
	_, worktree := listed(t)
	for _, command := range []string{"bash -c 'make ci'", "nice make ci", "nice -n 10 make ci", "timeout 900 make ci", "timeout -s KILL 15m make ci", "env FULL=1 make ci"} {
		t.Run(command, func(t *testing.T) {
			check(t, "updated input", updated(guardOf(t, command, worktree, false)), rewritten(queued(fakeHeavy+" "+command)))
		})
	}
}

// test_heavy_guard.py:787
func TestListedCommandBehindHeavyGetsNoSecondHeavy(t *testing.T) {
	_, worktree := listed(t)

	got := updated(guardOf(t, fakeHeavy+" make ci", worktree, true))

	check(t, "updated input", got, rewritten(fakeHeavy+" make ci"))
}

// test_heavy_guard.py:906
func TestSingleHeavyCommandNextToOtherWorkStillQueuesAlone(t *testing.T) {
	_, worktree := listed(t)

	got := updated(guardOf(t, "git add -A && make ci", worktree, false))

	check(t, "updated input", got, rewritten(queued("git add -A && "+fakeHeavy+" make ci")))
}
