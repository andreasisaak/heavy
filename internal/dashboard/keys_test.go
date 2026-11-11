package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// queueOfFour is a running, a paused and two waiting runs.
func queueOfFour() []protocol.RunStatus {
	runs := exampleRuns()[:3]
	return append(runs, protocol.RunStatus{State: "waiting", ID: "run-e2e", Label: "e2e", Directory: "/Users/example/sites/shopfront-search",
		Checkout: "/Users/example/sites/shopfront", Since: ago(time.Minute), Position: 2})
}

// selecting is the dashboard over queueOfFour with the cursor moved down the given number of rows.
func selecting(t *testing.T, daemon *fakeDaemon, row int) Model {
	t.Helper()
	daemon.answers = []fetchAnswer{{status: protocol.Status{Runs: queueOfFour()}}}
	model := started(t, daemon)
	for range row {
		model = step(t, model, keyDown)
	}
	return model
}

// lastLine is the last line of the view without escape sequences: the status line, or the key row without one.
func lastLine(t *testing.T, model Model) string {
	t.Helper()
	content := plain(t, model.View().Content)
	return content[strings.LastIndex(content, "\n")+1:]
}

// acted is what one or more key presses did: the messages sent, and the last line of the view afterwards.
type acted struct {
	Sent     []protocol.Message
	LastLine string
}

// String shows the messages as their JSON lines, so a failure names the run ids instead of pointers.
func (a acted) String() string {
	sent, err := json.Marshal(a.Sent)
	if err != nil {
		return fmt.Sprintf("unencodable messages %v: %v", a.Sent, err)
	}
	return fmt.Sprintf("{Sent: %s, LastLine: %q}", sent, a.LastLine)
}

// pressAll presses the keys in turn and hands the message of each command back to Update.
func pressAll(t *testing.T, model Model, keys ...tea.KeyPressMsg) Model {
	t.Helper()
	for _, key := range keys {
		var answer tea.Msg
		model, answer = press(t, model, key)
		if answer != nil {
			model = step(t, model, answer)
		}
	}
	return model
}

// plan 6.3: f on the selected waiting run sends forward with its position, and the answer shows under the key row
func TestForwardSendsThePositionOfTheSelectedWaitingRun(t *testing.T) {
	daemon := &fakeDaemon{sendLine: "heavy: moved e2e to #1"}
	model := pressAll(t, selecting(t, daemon, 3), letter('f'))

	got := acted{Sent: daemon.sent, LastLine: lastLine(t, model)}

	want := acted{
		Sent:     []protocol.Message{{Type: protocol.TypeForward, Forward: &protocol.Forward{Position: 2}}},
		LastLine: "heavy: moved e2e to #1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("after f = %+v, want %+v", got, want)
	}
}

func TestForwardOnARunThatDoesNotWaitSaysWhy(t *testing.T) {
	daemon := &fakeDaemon{}
	model := pressAll(t, selecting(t, daemon, 0), letter('f'))

	got := acted{Sent: daemon.sent, LastLine: lastLine(t, model)}

	want := acted{LastLine: "f moves a waiting run ahead; ci is running"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("after f = %+v, want %+v", got, want)
	}
}

// plan 6.3: p pauses a running run and resumes a paused one
func TestPauseKeyPausesARunningRunAndResumesAPausedOne(t *testing.T) {
	cases := []struct {
		name string
		row  int
		want protocol.Message
	}{
		{"running", 0, protocol.Message{Type: protocol.TypePause, Pause: &protocol.Target{ID: "run-ci"}}},
		{"paused", 1, protocol.Message{Type: protocol.TypeResume, Resume: &protocol.Target{ID: "run-test"}}},
	}
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			daemon := &fakeDaemon{sendLine: "heavy: done"}
			model := pressAll(t, selecting(t, daemon, current.row), letter('p'))

			got := acted{Sent: daemon.sent, LastLine: lastLine(t, model)}

			want := acted{Sent: []protocol.Message{current.want}, LastLine: "heavy: done"}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("after p = %+v, want %+v", got, want)
			}
		})
	}
}

func TestPauseKeyOnAWaitingRunSaysWhy(t *testing.T) {
	daemon := &fakeDaemon{}
	model := pressAll(t, selecting(t, daemon, 2), letter('p'))

	got := acted{Sent: daemon.sent, LastLine: lastLine(t, model)}

	want := acted{LastLine: "p pauses a running run and resumes a paused one; ui-test is waiting"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("after p = %+v, want %+v", got, want)
	}
}

func TestFailedSendShowsItsError(t *testing.T) {
	daemon := &fakeDaemon{sendErr: errors.New("heavy: no run waits at #2; heavy --status shows the queue")}
	model := pressAll(t, selecting(t, daemon, 3), letter('f'))

	got := acted{Sent: daemon.sent, LastLine: lastLine(t, model)}

	want := acted{
		Sent:     []protocol.Message{{Type: protocol.TypeForward, Forward: &protocol.Forward{Position: 2}}},
		LastLine: "heavy: no run waits at #2; heavy --status shows the queue",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("after f = %+v, want %+v", got, want)
	}
}

func TestActionKeysWithoutARunSayThereIsNone(t *testing.T) {
	for _, key := range []rune{'f', 'p', 's'} {
		t.Run(string(key), func(t *testing.T) {
			daemon := &fakeDaemon{answers: []fetchAnswer{{status: protocol.Status{Runs: []protocol.RunStatus{}}}}}
			model := pressAll(t, started(t, daemon), letter(key))

			got := acted{Sent: daemon.sent, LastLine: lastLine(t, model)}

			if want := (acted{LastLine: "no run selected"}); !reflect.DeepEqual(got, want) {
				t.Fatalf("after %c = %+v, want %+v", key, got, want)
			}
		})
	}
}

func TestQuitKeysEndTheProgram(t *testing.T) {
	for name, key := range map[string]tea.KeyPressMsg{"q": letter('q'), "ctrl+c": keyCtrlC} {
		t.Run(name, func(t *testing.T) {
			_, got := press(t, selecting(t, &fakeDaemon{}, 0), key)

			if got != (tea.QuitMsg{}) {
				t.Fatalf("%s = %#v, want tea.QuitMsg{}", name, got)
			}
		})
	}
}
