package guard_test

import "testing"

const noMonitor = " No heavy monitor is connected for this session; start Monitor with: heavy --monitor --session " + session

// FR-29: without a monitor of the session, the rewrite context says how to start one
func TestRewriteWithoutAMonitorOfTheSessionSaysHowToStartOne(t *testing.T) {
	_, worktree := listed(t)
	for name, monitors := range map[string][]string{"no monitor": nil, "monitor of another session": {"4403ca6f-other"}} {
		t.Run(name, func(t *testing.T) {
			current := newWorld(t)
			current.monitors = monitors

			got := current.run(t, bash("make ci", worktree, true))

			check(t, "answer", got, rewrite(rewritten(fakeHeavy+" make ci"), contextOf("`make ci`", inFront, free, fakeHeavy+" make ci")+noMonitor))
		})
	}
}

// V20: hook input without a session names no session to start a monitor for, so the context stays as it is
func TestRewriteWithoutASessionIdAddsNoMonitorNote(t *testing.T) {
	_, worktree := listed(t)
	current := newWorld(t)
	current.monitors = nil
	input := bash("make ci", worktree, true)
	input.SessionID = ""

	got := current.run(t, input)

	check(t, "answer", got, rewrite(rewritten(fakeHeavy+" make ci"), contextOf("`make ci`", inFront, free, fakeHeavy+" make ci")))
}
