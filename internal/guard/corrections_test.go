package guard_test

import (
	"testing"

	"github.com/andreasisaak/heavy/internal/shell"
)

// Calls where 0.6 fell back for want of its scanner (analysis.md, Port-Fallen; plan 4.1 note), and the syntax tree
// decides now.
func TestSyntaxTreePlacesHeavyWhereTheScannerCouldNot(t *testing.T) {
	_, worktree := listed(t)
	for _, current := range []struct{ command, expected string }{
		// A subshell elsewhere made 0.6 run the whole call through heavy.
		{"make ci && (cd /tmp && ls)", queued(fakeHeavy + " make ci && (cd /tmp && ls)")},
		{"make ci; case $x in a) echo;; esac", queued(fakeHeavy + " make ci; case $x in a) echo;; esac")},
		// time is a keyword of Bash, not a program heavy runs.
		{"time make ci", queued("time " + fakeHeavy + " make ci")},
		// A heavy substitution in the words of a heavy command runs before heavy starts.
		{"make ci $(make test)", queued(fakeHeavy + " " + wholeShell + " " + shell.Quote("make ci $(make test)"))},
		// The tokenizer of 0.6 lost the command of a process substitution.
		{"cat <(make ci)", queued(fakeHeavy + " " + wholeShell + " " + shell.Quote("cat <(make ci)"))},
	} {
		t.Run(current.command, func(t *testing.T) {
			check(t, "updated input", updated(guardOf(t, current.command, worktree, false)), rewritten(current.expected))
		})
	}
}

// 0.6 skipped the check of background forms for calls its scanner could not place.
func TestBackgroundFormOfAWholeCallIsDenied(t *testing.T) {
	_, worktree := listed(t)

	got := guardOf(t, "(make ci) > /tmp/ci.log", worktree, true)

	check(t, "answer", got, denial(conflictReason("`make ci`", "stdout goes to a file", "make ci")))
}

// 0.6 named no agent behind a wrapper.
func TestAgentSessionCallingHeavyBehindAWrapperNamesTheAgent(t *testing.T) {
	got := updated(newWorld(t).run(t, agentSession("nice -n 5 heavy make ci", temporaryDirectory(t), true)))

	check(t, "updated input", got, rewritten("nice -n 5 heavy --agent test-author make ci"))
}
