package guard_test

import (
	"testing"

	"github.com/andreasisaak/heavy/internal/shell"
)

func subagentDenial(targets string) *answer {
	return denial("Subagents start no heavy runs: " + targets + " would wait in the machine-wide queue and take a place there for minutes. " +
		"Do not run it in another form either. Finish what you can check without it, and name the exact command " +
		"in your final report: the main agent runs it once for all changes. " + targeted)
}

// test_heavy_guard.py:637
func TestSubagentListedRunIsDenied(t *testing.T) {
	_, worktree := listed(t)

	check(t, "answer", newWorld(t).run(t, subagent("make ci 2>&1 | tail -20", worktree, true)), subagentDenial("`make ci`"))
}

// test_heavy_guard.py:643
func TestSubagentStartingHeavyItselfIsDenied(t *testing.T) {
	for _, current := range []struct{ command, targets string }{
		{"heavy make test-complete", "`make test-complete`"},
		{"cd /sites/main && heavy env CHECK_WORKER=1 tests/Complete/checkall.sh 2>&1 | tail -5", "`env CHECK_WORKER=1 tests/Complete/checkall.sh`"},
		{"/opt/heavy/bin/heavy --directory /sites/main make ci", "`make ci`"},
	} {
		t.Run(current.command, func(t *testing.T) {
			check(t, "answer", newWorld(t).run(t, subagent(current.command, temporaryDirectory(t), true)), subagentDenial(current.targets))
		})
	}
}

// test_heavy_guard.py:658
func TestSubagentCatalogueRunIsDenied(t *testing.T) {
	_, worktree := listed(t)

	got := newWorld(t).run(t, subagent("docker compose exec -T web pnpm exec vitest run", worktree, false))

	check(t, "answer", got, subagentDenial("`vitest`"))
}

// test_heavy_guard.py:664
func TestSubagentSingleFileRunPasses(t *testing.T) {
	_, worktree := listed(t)

	check(t, "answer", newWorld(t).run(t, subagent("docker compose exec -T web pnpm exec vitest run tests/cart.spec.ts", worktree, false)), nil)
}

// test_heavy_guard.py:670
func TestAgentSessionRunNamesTheAgentToHeavy(t *testing.T) {
	_, worktree := listed(t)

	got := updated(newWorld(t).run(t, agentSession("make ci", worktree, false)))

	check(t, "updated input", got, rewritten(queued(fakeHeavy+" --agent test-author make ci")))
}

// test_heavy_guard.py:676
func TestAgentSessionRunThroughTheWholeShellNamesTheAgent(t *testing.T) {
	_, worktree := listed(t)
	command := "cd " + worktree + " && echo $(make ci)"

	got := updated(newWorld(t).run(t, agentSession(command, temporaryDirectory(t), false)))

	check(t, "updated input", got, rewritten(queued(fakeHeavy+" --directory "+worktree+" --agent test-author "+wholeShell+" "+shell.Quote(command))))
}

// test_heavy_guard.py:685
func TestAgentSessionCallingHeavyItselfNamesTheAgent(t *testing.T) {
	for _, current := range []struct{ command, expected string }{
		{
			"cd /sites/main && heavy env CHECK_WORKER=1 tests/Complete/checkall.sh 2>&1 | tee /tmp/security.log",
			"cd /sites/main && heavy --agent test-author env CHECK_WORKER=1 tests/Complete/checkall.sh 2>&1 | tee /tmp/security.log",
		},
		{"/opt/heavy/bin/heavy --directory /sites/main make ci", "/opt/heavy/bin/heavy --agent test-author --directory /sites/main make ci"},
		{"FULL=1 heavy make ci", "FULL=1 heavy --agent test-author make ci"},
	} {
		t.Run(current.command, func(t *testing.T) {
			got := updated(newWorld(t).run(t, agentSession(current.command, temporaryDirectory(t), true)))

			check(t, "updated input", got, rewritten(current.expected))
		})
	}
}

// test_heavy_guard.py:704
func TestSubagentCallWithoutAHeavyRunPassesUnchanged(t *testing.T) {
	for _, command := range []string{"heavy --status", "heavy --status --json", "heavy --inside-run", "heavy --help", "heavy status", "heavy help", "echo heavy make ci"} {
		t.Run(command, func(t *testing.T) {
			check(t, "answer", newWorld(t).run(t, subagent(command, temporaryDirectory(t), false)), nil)
		})
	}
}

// test_heavy_guard.py:779
func TestAgentSessionCallingHeavyWithAnAgentKeepsThatAgent(t *testing.T) {
	got := updated(newWorld(t).run(t, agentSession("heavy --agent critic make ci", temporaryDirectory(t), true)))

	check(t, "updated input", got, rewritten("heavy --agent critic make ci"))
}

// Decided in task 4.1: the Go CLI (design G14) has more actions than 0.6, and none of them starts a run.
func TestHeavyActionsOfTheCliStartNoRun(t *testing.T) {
	for _, command := range []string{"heavy --forward 3", "heavy --stop 4f2a", "heavy --pause 4f2a", "heavy --dashboard", "heavy --hook guard", "heavy --agent critic --monitor"} {
		t.Run(command, func(t *testing.T) {
			check(t, "answer", newWorld(t).run(t, subagent(command, temporaryDirectory(t), false)), nil)
		})
	}
}
