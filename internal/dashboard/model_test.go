package dashboard

import (
	"reflect"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andreasisaak/heavy/internal/protocol"
)

func windowWidth(width int) tea.WindowSizeMsg {
	return tea.WindowSizeMsg{Width: width, Height: 24}
}

// idOf is the id of the selected run, "" without one.
func idOf(selected *protocol.RunStatus) string {
	if selected == nil {
		return ""
	}
	return selected.ID
}

// nextFetch is the command the refresh tick starts.
func nextFetch(t *testing.T, model Model) tea.Cmd {
	t.Helper()
	_, command := model.Update(tickMsg{})
	return command
}

// FR-33: the dashboard asks again 2 s after each answer
func TestAnswerOfTheDaemonStartsTheNextRefreshAfterTwoSeconds(t *testing.T) {
	daemon := &fakeDaemon{answers: []fetchAnswer{{status: protocol.Status{Runs: exampleRuns()}}}}
	model := New(Options{Host: "Mac", Fetch: daemon.fetch, Send: daemon.send, Now: func() time.Time { return exampleNow }, Estimate: func() int64 { return 0 }})
	answer := run(t, model.Init(), "Init")

	began := time.Now()
	_, refresh := model.Update(answer)
	got := run(t, refresh, "Update of the answer")
	waited := time.Since(began)

	if got != (tickMsg{}) || waited < RefreshEvery || waited > RefreshEvery+time.Second || daemon.fetches != 1 {
		t.Fatalf("refresh = %#v after %s with %d fetches, want tickMsg{} after %s with 1 fetch", got, waited, daemon.fetches, RefreshEvery)
	}
}

func TestTickFetchesTheQueueAgain(t *testing.T) {
	daemon := &fakeDaemon{answers: []fetchAnswer{
		{status: protocol.Status{Runs: exampleRuns()[:1]}},
		{status: protocol.Status{Runs: exampleRuns()[1:2]}},
	}}
	model := started(t, daemon)

	model = step(t, model, run(t, nextFetch(t, model), "the tick"))

	if got, want := model.open(), exampleRuns()[1:2]; !reflect.DeepEqual(got, want) || daemon.fetches != 2 {
		t.Fatalf("open runs after %d fetches = %+v, want 2 fetches and %+v", daemon.fetches, got, want)
	}
}

func TestSelectionStaysOnItsRunWhenTheQueueChanges(t *testing.T) {
	runs := exampleRuns()
	daemon := &fakeDaemon{answers: []fetchAnswer{
		{status: protocol.Status{Runs: runs}},
		{status: protocol.Status{Runs: []protocol.RunStatus{runs[1], runs[2]}}},
	}}
	model := started(t, daemon)
	model = step(t, model, keyDown)

	model = step(t, model, run(t, nextFetch(t, model), "the tick"))

	if got := idOf(model.selectedRun()); got != "run-test" {
		t.Fatalf("selected run = %q, want run-test", got)
	}
}

func TestSelectionMovesUpAndDownWithinTheRuns(t *testing.T) {
	daemon := &fakeDaemon{answers: []fetchAnswer{{status: protocol.Status{Runs: exampleRuns()}}}}
	model := started(t, daemon)

	var visited []string
	for _, key := range []tea.KeyPressMsg{keyUp, keyDown, keyDown, keyDown, keyUp} {
		model = step(t, model, key)
		visited = append(visited, idOf(model.selectedRun()))
	}

	if want := []string{"run-ci", "run-test", "run-ui", "run-ui", "run-test"}; !reflect.DeepEqual(visited, want) {
		t.Fatalf("selected runs = %v, want %v", visited, want)
	}
}

func TestSelectionFallsBackToTheLastRunWhenItsRunEnds(t *testing.T) {
	runs := exampleRuns()
	daemon := &fakeDaemon{answers: []fetchAnswer{
		{status: protocol.Status{Runs: runs}},
		{status: protocol.Status{Runs: runs[:2]}},
	}}
	model := started(t, daemon)
	model = step(t, step(t, model, keyDown), keyDown)

	model = step(t, model, run(t, nextFetch(t, model), "the tick"))

	if got := idOf(model.selectedRun()); got != "run-test" {
		t.Fatalf("selected run = %q, want run-test", got)
	}
}
