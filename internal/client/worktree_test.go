package client_test

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// worktree adds a git worktree of repository and returns its resolved path.
func worktree(t *testing.T, repository string) string {
	t.Helper()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "second")
	for _, arguments := range [][]string{
		{"-C", repository, "-c", "user.name=heavy", "-c", "user.email=heavy@example.com", "commit", "-q", "--allow-empty", "-m", "start"},
		{"-C", repository, "worktree", "add", "-q", "-b", "second", path},
	} {
		if output, err := exec.Command("/usr/bin/git", arguments...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
	}
	return path
}

// requirements.md:85 and the note of 2026-11-08 under task 2.4: two worktrees of one repository share the main
// checkout and its history, but a run in each runs at the same time
func TestRunsInTwoWorktreesOfOneRepositoryOverlap(t *testing.T) {
	e := setup(t)
	main := repository(t)
	second := worktree(t, main)
	e.history(t, main, main)
	killMarked(t, "5.701", "5.702")
	first := e.start(t, main, "sleep", "5.701")
	first.waitStdout(t, startedPattern+`sleep 5.701\n`)
	other := e.start(t, second, "sleep", "5.702")

	other.waitStdout(t, startedPattern+`sleep 5.702\n`)
	if first.finished() {
		t.Fatalf("the run in the main checkout ended before the run in the worktree started; stdout %q", other.stdout.String())
	}
	if first.wait(t) != 0 || other.wait(t) != 0 {
		t.Fatalf("a run failed")
	}
}
