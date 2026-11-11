package guard_test

import (
	"os"
	"strings"
	"testing"
)

const missingDirectory = "/tmp/heavy-missing-directory"

// noMissingDirectory makes sure the directory the tee cases name does not exist, as they need.
func noMissingDirectory(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(missingDirectory); !os.IsNotExist(err) {
		t.Fatalf("%s must not exist for these cases, stat gave %v", missingDirectory, err)
	}
}

func conflictReason(targets, cause, example string) string {
	return targets + " must run through `heavy`, but another hook may rewrite this background call at the same moment " +
		"because " + cause + ", and then `heavy` goes missing. Write it in the form such hooks leave alone, with the log " +
		"in an existing directory, for example: set -o pipefail; " + example + " 2>&1 | tee <file>; exit_status=$?; " +
		`echo "` + strings.Fields(example)[0] + ` $exit_status"; exit $exit_status`
}

// test_heavy_guard.py:374
func TestBackgroundHeavyCommandAnotherHookRewritesIsDenied(t *testing.T) {
	noMissingDirectory(t)
	_, worktree := listed(t)
	for _, current := range []struct{ command, cause string }{
		{"make ci > /tmp/ci.log 2>&1", "stdout goes to a file"},
		{"make ci >> /tmp/ci.log", "stdout goes to a file"},
		{"make ci &> /tmp/ci.log", "stdout goes to a file"},
		{"make ci 1> /tmp/ci.log", "stdout goes to a file"},
		{"make ci 2>&1 | tee /tmp/heavy-missing-directory/ci.log", "the directory of the tee target /tmp/heavy-missing-directory/ci.log does not exist"},
		{`make ci; echo "ci $?"`, "a trailing echo of $? does not end with exit"},
		{`make ci 2>&1 | tee /tmp/ci.log; echo "done $?"`, "a trailing echo of $? does not end with exit"},
	} {
		t.Run(current.command, func(t *testing.T) {
			check(t, "answer", guardOf(t, current.command, worktree, true), denial(conflictReason("`make ci`", current.cause, "make ci")))
		})
	}
}

// test_heavy_guard.py:403
func TestOtherRedirectsStayRewritable(t *testing.T) {
	noMissingDirectory(t)
	_, worktree := listed(t)
	for _, current := range []struct {
		command    string
		background bool
	}{
		{"make ci > /dev/null 2>&1", true},
		{"make ci 2> /tmp/errors.log", true},
		{"make ci >&2", true},
		{"make ci > /tmp/ci.log 2>&1", false},
		{"mkdir -p /tmp/heavy-missing-directory && make ci 2>&1 | tee /tmp/heavy-missing-directory/ci.log", true},
		{`make ci && echo "ci $?"`, true},
	} {
		t.Run(current.command, func(t *testing.T) {
			withHeavy := strings.Replace(current.command, "make ci", fakeHeavy+" make ci", 1)
			want := withHeavy
			if !current.background {
				want = queued(withHeavy)
			}
			check(t, "updated input", updated(guardOf(t, current.command, worktree, current.background)), rewritten(want))
		})
	}
}

// test_heavy_guard.py:762
func TestBackgroundCallThatStartsHeavyInAFormOtherHooksRewriteIsDenied(t *testing.T) {
	got := guardOf(t, "heavy make ci > /tmp/ci.log 2>&1", temporaryDirectory(t), true)

	check(t, "answer", got, denial(conflictReason("`make ci`", "stdout goes to a file", "heavy make ci")))
}
