package herdr_test

import (
	"errors"
	"testing"

	"github.com/andreasisaak/heavy/internal/herdr"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// forwarding is the forward action in pane against a queue of two waiting runs, ci of pane w1:p1 at #1 and test
// of pane w1:p2 at #2; forwarded collects the positions it moved ahead.
type forwarding struct {
	fake      fakeHerdr
	deps      herdr.ForwardDependencies
	forwarded *[]int
}

func newForwarding(t *testing.T, pane, failing string) forwarding {
	t.Helper()
	fake := newHerdr(t, nil, failing)
	forwarded := &[]int{}
	runs := []protocol.RunStatus{inPane(waiting(1, 50, "/sites/main", "ci"), "w1:p1"), inPane(waiting(2, 40, "/sites/main", "test"), "w1:p2")}
	return forwarding{fake: fake, forwarded: forwarded, deps: herdr.ForwardDependencies{
		Herdr: fake.path,
		Pane:  pane,
		Runs:  func() ([]protocol.RunStatus, error) { return runs, nil },
		Forward: func(position int) error {
			*forwarded = append(*forwarded, position)
			return nil
		},
	}}
}

func notification(body string) []string {
	return []string{"notification", "show", "heavy", "--body", body}
}

// herdr/test_heavy_forward.py: test_moves_the_run_of_the_focused_pane_ahead_and_shows_the_new_position
func TestForwardMovesTheRunOfTheFocusedPaneAheadAndShowsItsNewPosition(t *testing.T) {
	current := newForwarding(t, "w1:p2", "")

	err := herdr.Forward(current.deps)

	check(t, "error", err, nil)
	check(t, "herdr calls", current.fake.recorded(t), [][]string{notification("moved test to #1")})
	check(t, "forwarded positions", *current.forwarded, []int{2})
}

// herdr/test_heavy_forward.py: test_says_so_when_the_run_of_the_focused_pane_is_next_already
func TestForwardSaysSoWhenTheRunOfTheFocusedPaneIsNextAlready(t *testing.T) {
	current := newForwarding(t, "w1:p1", "")

	err := herdr.Forward(current.deps)

	check(t, "error", err, nil)
	check(t, "herdr calls", current.fake.recorded(t), [][]string{notification("ci is next already")})
	check(t, "forwarded positions", *current.forwarded, []int{})
}

// herdr/test_heavy_forward.py: test_says_so_when_the_focused_pane_has_no_waiting_run; a running run of the pane
// is no queued run either
func TestForwardSaysSoWhenTheFocusedPaneHasNoWaitingRun(t *testing.T) {
	for _, pane := range []string{"w9:p9", "w1:p3"} {
		t.Run(pane, func(t *testing.T) {
			current := newForwarding(t, pane, "")
			runs, _ := current.deps.Runs()
			current.deps.Runs = func() ([]protocol.RunStatus, error) {
				return append([]protocol.RunStatus{inPane(running(90, "/sites/main", "lint"), "w1:p3")}, runs...), nil
			}

			err := herdr.Forward(current.deps)

			check(t, "error", err, nil)
			check(t, "herdr calls", current.fake.recorded(t), [][]string{notification("no queued run in this pane")})
			check(t, "forwarded positions", *current.forwarded, []int{})
		})
	}
}

func TestForwardFailsWithTheDaemonsAnswerAndShowsNothing(t *testing.T) {
	current := newForwarding(t, "w1:p2", "")
	current.deps.Forward = func(position int) error {
		*current.forwarded = append(*current.forwarded, position)
		return errors.New("heavy: no run waits at #2; heavy --status shows the queue")
	}

	err := herdr.Forward(current.deps)

	check(t, "error", errorText(err), "heavy: no run waits at #2; heavy --status shows the queue")
	check(t, "herdr calls", current.fake.recorded(t), [][]string(nil))
	check(t, "forwarded positions", *current.forwarded, []int{2})
}

func TestForwardFailsWithTheQueueError(t *testing.T) {
	current := newForwarding(t, "w1:p2", "")
	current.deps.Runs = func() ([]protocol.RunStatus, error) {
		return nil, errors.New("heavy: reading the queue from the daemon: EOF")
	}

	err := herdr.Forward(current.deps)

	check(t, "error", errorText(err), "heavy: reading the queue from the daemon: EOF")
	check(t, "herdr calls", current.fake.recorded(t), [][]string(nil))
}

func TestForwardFailsWithHerdrsAnswerWhenTheNotificationFails(t *testing.T) {
	current := newForwarding(t, "w1:p1", "notification show")

	err := herdr.Forward(current.deps)

	want := "heavy: " + current.fake.path + " notification show heavy --body ci is next already failed: exit status 3: herdr: example failure of notification show"
	check(t, "error", errorText(err), want)
}

// herdr sets HERDR_PANE_ID for a plugin action; without it the action cannot know the focused pane
func TestForwardWithoutAPaneFailsBeforeReadingTheQueue(t *testing.T) {
	current := newForwarding(t, "", "")
	current.deps.Runs = func() ([]protocol.RunStatus, error) {
		t.Error("read the queue without a pane")
		return nil, nil
	}

	err := herdr.Forward(current.deps)

	check(t, "error", errorText(err), "heavy: --herdr-forward needs HERDR_PANE_ID, the focused pane herdr sets for a plugin action")
	check(t, "herdr calls", current.fake.recorded(t), [][]string(nil))
}

func errorText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}
