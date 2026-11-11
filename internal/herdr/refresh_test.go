package herdr_test

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/herdr"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// refreshed runs a refresh against the runs and a fake herdr with the agents and returns the herdr calls and stdout.
func refreshed(t *testing.T, runs []protocol.RunStatus, agents []agent) ([][]string, string) {
	t.Helper()
	fake := newHerdr(t, agents, "")
	var stdout bytes.Buffer
	if err := herdr.Refresh(dependencies(t, fake, runs), &stdout); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	return fake.recorded(t), stdout.String()
}

func dependencies(t *testing.T, fake fakeHerdr, runs []protocol.RunStatus) herdr.RefreshDependencies {
	return herdr.RefreshDependencies{
		Herdr: fake.path,
		Runs:  func() ([]protocol.RunStatus, error) { return runs, nil },
		Now:   func() time.Time { return time.Unix(now, 0) },
		Lock:  filepath.Join(t.TempDir(), "herdr-refresh.lock"),
		Event: "startup",
	}
}

// herdr/test_heavy_queue.py: test_reports_running_and_queued_runs_with_symbol_and_minutes_on_the_agents_of_their_checkout
func TestRunningAndQueuedRunsShowWithSymbolAndMinutesOnTheAgentsOfTheirCheckout(t *testing.T) {
	root := checkouts(t, "main", "main-feature", "other")
	main, feature, other := filepath.Join(root, "main"), filepath.Join(root, "main-feature"), filepath.Join(root, "other")
	runs := []protocol.RunStatus{running(365, filepath.Join(feature, "frontend"), "ui-test"), waiting(1, 95, main, "ci")}

	calls, stdout := refreshed(t, runs, []agent{
		agentOf("w1:p1", main, map[string]string{"kind": "claude"}),
		agentOf("w1:p2", feature, map[string]string{}),
		agentOf("w2:p1", root, map[string]string{}),
		agentOf("w1:p3", other, map[string]string{"heavy": "▶ 3m · ci"}),
	})

	check(t, "herdr calls", calls, [][]string{
		agentList,
		report("w1:p1", "--token", "heavy=⧗ 1m · #1 · ci"),
		report("w1:p2", "--token", "heavy=▶ 6m · ui-test"),
		report("w1:p3", "--clear-token", "heavy"),
	})
	check(t, "stdout", stdout, "heavy-queue: event=startup agents=4 reported=3\n")
}

// herdr/test_heavy_queue.py: test_run_started_in_herdr_shows_only_on_its_own_agent
func TestRunStartedInHerdrShowsOnlyOnItsOwnAgent(t *testing.T) {
	root := checkouts(t, "main", "other")
	main, other := filepath.Join(root, "main"), filepath.Join(root, "other")
	runs := []protocol.RunStatus{inPane(running(20, main, "ci"), "w4Q:p1")}

	calls, _ := refreshed(t, runs, []agent{agentOf("w3W:p47", main, nil), agentOf("w3W:p5T", main, nil), agentOf("w4Q:p1", other, nil)})

	check(t, "herdr calls", calls, [][]string{agentList, report("w4Q:p1", "--token", "heavy=▶ <1m · ci")})
}

// herdr/test_heavy_queue.py: test_long_run_shows_hours_and_minutes
func TestLongRunShowsHoursAndMinutes(t *testing.T) {
	main := filepath.Join(checkouts(t, "main"), "main")

	calls, _ := refreshed(t, []protocol.RunStatus{running(3_905, main, "ci")}, []agent{agentOf("w1:p1", main, nil)})

	check(t, "herdr calls", calls, [][]string{agentList, report("w1:p1", "--token", "heavy=▶ 1h05m · ci")})
}

// herdr/test_heavy_queue.py: test_finished_run_shows_its_result_until_it_leaves_the_status
func TestFinishedRunShowsItsResultUntilItLeavesTheStatus(t *testing.T) {
	root := checkouts(t, "main", "other")
	main, other := filepath.Join(root, "main"), filepath.Join(root, "other")
	runs := []protocol.RunStatus{finished(5, 30, other, "test-complete", 2), finished(10, 795, main, "ci", 0)}

	calls, _ := refreshed(t, runs, []agent{agentOf("w1:p1", main, nil), agentOf("w1:p2", other, nil)})

	check(t, "herdr calls", calls, [][]string{
		agentList,
		report("w1:p1", "--token", "heavy=✓ 13m · ci", "--ttl-ms", "20000"),
		report("w1:p2", "--token", "heavy=✗ <1m · test-complete", "--ttl-ms", "25000"),
	})
}

// herdr/test_heavy_queue.py: test_active_run_hides_the_result_of_the_previous_one
func TestActiveRunHidesTheResultOfThePreviousOne(t *testing.T) {
	main := filepath.Join(checkouts(t, "main"), "main")
	runs := []protocol.RunStatus{waiting(1, 5, main, "ci"), finished(10, 795, main, "ci", 2)}

	calls, _ := refreshed(t, runs, []agent{agentOf("w1:p1", main, nil)})

	check(t, "herdr calls", calls, [][]string{agentList, report("w1:p1", "--token", "heavy=⧗ <1m · #1 · ci")})
}

// herdr/test_heavy_queue.py: test_reports_nothing_when_every_token_is_current
func TestNothingIsReportedWhenEveryTokenIsCurrent(t *testing.T) {
	root := checkouts(t, "main")
	main := filepath.Join(root, "main")

	calls, stdout := refreshed(t, []protocol.RunStatus{waiting(1, 30, main, "ci")}, []agent{
		agentOf("w1:p1", main, map[string]string{"heavy": "⧗ <1m · #1 · ci"}),
		agentOf("w2:p1", root, nil),
	})

	check(t, "herdr calls", calls, [][]string{agentList})
	check(t, "stdout", stdout, "heavy-queue: event=startup agents=2 reported=0\n")
}

// herdr/test_heavy_queue.py: test_run_of_a_subagent_names_the_subagent
func TestRunOfASubagentNamesTheSubagent(t *testing.T) {
	main := filepath.Join(checkouts(t, "main"), "main")
	run := running(365, main, "checkall.sh")
	run.Agent = "test-author"

	calls, _ := refreshed(t, []protocol.RunStatus{run}, []agent{agentOf("w1:p1", main, nil)})

	check(t, "herdr calls", calls, [][]string{agentList, report("w1:p1", "--token", "heavy=▶ 6m · checkall.sh ↳ test-author")})
}

// FR-30: two runs of a pane stand in two rows, one token each, instead of one row joined with spaces
func TestTwoRunsOfAPaneGetARowEach(t *testing.T) {
	main := filepath.Join(checkouts(t, "main"), "main")
	runs := []protocol.RunStatus{inPane(running(365, main, "ci"), "w1:p1"), inPane(waiting(1, 95, main, "test"), "w1:p1")}

	calls, _ := refreshed(t, runs, []agent{agentOf("w1:p1", main, nil)})

	check(t, "herdr calls", calls, [][]string{agentList, report("w1:p1", "--token", "heavy=▶ 6m · ci", "--token", "heavy_2=⧗ 1m · #1 · test")})
}

// FR-30: from four runs on, the third row counts the runs it stands for
func TestFourRunsShowTwoRowsAndHowManyMore(t *testing.T) {
	main := filepath.Join(checkouts(t, "main"), "main")
	runs := []protocol.RunStatus{running(365, main, "ci"), paused(240, main, "test"), waiting(1, 95, main, "ui-test"), waiting(2, 30, main, "lint")}

	calls, _ := refreshed(t, runs, []agent{agentOf("w1:p1", main, nil)})

	check(t, "herdr calls", calls, [][]string{
		agentList,
		report("w1:p1", "--token", "heavy=▶ 6m · ci", "--token", "heavy_2=⏸ 4m · test", "--token", "heavy_3=+2 more"),
	})
}

// FR-30: a paused run shows ⏸ and the minutes since it started
func TestPausedRunShowsThePauseSymbol(t *testing.T) {
	main := filepath.Join(checkouts(t, "main"), "main")

	calls, _ := refreshed(t, []protocol.RunStatus{paused(240, main, "test")}, []agent{agentOf("w1:p1", main, nil)})

	check(t, "herdr calls", calls, [][]string{agentList, report("w1:p1", "--token", "heavy=⏸ 4m · test")})
}

// rows the runs no longer fill are cleared, and a row that stays as it is is not reported again
func TestRowsNoLongerNeededAreCleared(t *testing.T) {
	main := filepath.Join(checkouts(t, "main"), "main")

	calls, _ := refreshed(t, []protocol.RunStatus{running(365, main, "ci")}, []agent{
		agentOf("w1:p1", main, map[string]string{"heavy": "▶ 6m · ci", "heavy_2": "⧗ 1m · #1 · test", "heavy_3": "+2 more"}),
	})

	check(t, "herdr calls", calls, [][]string{agentList, report("w1:p1", "--clear-token", "heavy_2", "--clear-token", "heavy_3")})
}

// an agent without a pane id gets no tokens, and a run in a folder below an agent outside a checkout is not its run
func TestAgentWithoutPaneAndFolderOutsideACheckoutGetNothing(t *testing.T) {
	root := checkouts(t)

	calls, stdout := refreshed(t, []protocol.RunStatus{running(365, filepath.Join(root, "project"), "ci")}, []agent{
		agentOf("", root, nil), agentOf("w1:p1", root, nil),
	})

	check(t, "herdr calls", calls, [][]string{agentList})
	check(t, "stdout", stdout, "heavy-queue: event=startup agents=1 reported=0\n")
}
