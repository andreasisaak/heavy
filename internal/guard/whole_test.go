package guard_test

import (
	"testing"

	"github.com/andreasisaak/heavy/internal/shell"
)

// test_heavy_guard.py:332; 0.6 found no place for heavy (ScanError, `$( )`, backticks), Go finds the same commands
// outside the top-level list.
func TestHeavyCommandTheScannerCannotPlaceRunsWholeThroughHeavy(t *testing.T) {
	_, worktree := listed(t)
	for _, command := range []string{
		"echo $(make ci)",
		"status=`make ci`",
		"echo \"result: `make ci`\"",
		`echo "$(make ci)"`,
	} {
		t.Run(command, func(t *testing.T) {
			got := guardOf(t, command, worktree, false)

			wholeCommand := queued(fakeHeavy + " " + wholeShell + " " + shell.Quote(command))
			check(t, "answer", got, rewrite(rewritten(wholeCommand), contextOf("`make ci`", whole+" and sent it to the background", free, wholeCommand)))
		})
	}
}

// test_heavy_guard.py:367
func TestWholeCommandNamesTheProjectWhenCdMovesAway(t *testing.T) {
	_, worktree := listed(t)
	command := "cd " + worktree + " && echo $(make ci)"

	got := updated(guardOf(t, command, temporaryDirectory(t), false))

	check(t, "updated input", got, rewritten(queued(fakeHeavy+" --directory "+worktree+" "+wholeShell+" "+shell.Quote(command))))
}

// queuedLines is a call of several lines or with a comment in { … }: the brace closes on a line of its own, since
// a `; }` behind a comment or the end of a heredoc would be part of them.
func queuedLines(command string) string {
	return "{ " + command + "\n}; exit_status=$?; echo \"heavy $exit_status\"; exit $exit_status"
}

// A call of several lines, with a comment or with a heredoc gets heavy right in front of its heavy command, like a
// call on one line: the work before it runs at once in the same shell, and only the heavy command waits.
func TestHeavyGoesInFrontOfTheHeavyCommandOfACallOfSeveralLines(t *testing.T) {
	_, worktree := listed(t)
	for _, current := range []struct{ command, rewritten string }{
		{"cat <<'EOF'\nnotes\nEOF\nmake ci", "cat <<'EOF'\nnotes\nEOF\n" + fakeHeavy + " make ci"},
		{"make ci # the full run", fakeHeavy + " make ci # the full run"},
		{"make lint\nmake ci", "make lint\n" + fakeHeavy + " make ci"},
		{"python3 - <<'EOF'\nprint('docs')\nEOF\nmake ci <<'IN'\ndata\nIN", "python3 - <<'EOF'\nprint('docs')\nEOF\n" + fakeHeavy + " make ci <<'IN'\ndata\nIN"},
		{"# make test\nx=1\nif true; then\n  make ci\nfi", "# make test\nx=1\nif true; then\n  " + fakeHeavy + " make ci\nfi"},
		{"git add -A\n(make ci) 2>&1 | tee /tmp/run.log", "git add -A\n(" + fakeHeavy + " make ci) 2>&1 | tee /tmp/run.log"},
	} {
		t.Run(current.command, func(t *testing.T) {
			got := updated(guardOf(t, current.command, worktree, false))

			check(t, "updated input", got, rewritten(queuedLines(current.rewritten)))
		})
	}
}

// Several heavy commands behind other work queue each on their own: the other work runs at once, which a call
// through heavy as a whole would keep waiting.
func TestSeveralHeavyCommandsBehindOtherWorkQueueEachOnTheirOwn(t *testing.T) {
	_, worktree := listed(t)
	for _, current := range []struct{ command, rewritten string }{
		{"git add -A && make ci && make test", queued("git add -A && " + fakeHeavy + " make ci && " + fakeHeavy + " make test")},
		{
			"python3 - <<'EOF'\nprint('docs')\nEOF\nbash -n tools/check.sh && echo ok\n(make test && make ci) 2>&1 | tee /tmp/run.log",
			queuedLines("python3 - <<'EOF'\nprint('docs')\nEOF\nbash -n tools/check.sh && echo ok\n(" + fakeHeavy + " make test && " + fakeHeavy + " make ci) 2>&1 | tee /tmp/run.log"),
		},
	} {
		t.Run(current.command, func(t *testing.T) {
			got := updated(guardOf(t, current.command, worktree, false))

			check(t, "updated input", got, rewritten(current.rewritten))
		})
	}
}

func mixedReason(targets, others string) string {
	return "This call puts other work (" + others + ") before " + targets + ", and the hook cannot run it apart from them, " +
		"so it would wait in the queue as well. " +
		"Run the other work in a Bash call of its own first, then " + targets + " in a separate call."
}

// test_heavy_guard.py:832
func TestHeavyRunMixedWithOtherWorkItCannotSeparateIsDeniedWithHowToSplit(t *testing.T) {
	_, worktree := listed(t)
	for _, current := range []struct{ command, targets, others string }{
		{"git add -A && echo $(make ci)", "`make ci`", "`git add`"},
		{"python3 - <<'EOF'\nprint('docs')\nEOF\nresult=`make test`\nmake ci", "`make ci`, `make test`", "`python3`"},
	} {
		t.Run(current.command, func(t *testing.T) {
			check(t, "answer", guardOf(t, current.command, worktree, false), denial(mixedReason(current.targets, current.others)))
		})
	}
}

// test_heavy_guard.py:852
func TestWorkAfterTheHeavyCommandsRunsWithThemInOneHold(t *testing.T) {
	_, worktree := listed(t)
	for _, command := range []string{"(make ci && make test && make lint) 2>&1 | tee /tmp/ci.log", "make ci && make test && git status"} {
		t.Run(command, func(t *testing.T) {
			got := updated(guardOf(t, command, worktree, false))

			check(t, "updated input", got, rewritten(queued(fakeHeavy+" "+wholeShell+" "+shell.Quote(command))))
		})
	}
}

// test_heavy_guard.py:859
func TestSeveralHeavyCommandsRunInOneHoldOfTheLock(t *testing.T) {
	_, worktree := listed(t)
	command := "cd " + worktree + " && make ci && make test 2>&1 | tee /tmp/test.log"

	got := updated(guardOf(t, command, temporaryDirectory(t), false))

	check(t, "updated input", got, rewritten(queued(fakeHeavy+" --directory "+worktree+" "+wholeShell+" "+shell.Quote(command))))
}
