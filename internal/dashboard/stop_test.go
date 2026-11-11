package dashboard

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// plan 6.3: s asks `stop <label>? y/n` and sends nothing yet
func TestStopKeyAsksFirst(t *testing.T) {
	daemon := &fakeDaemon{}
	model := pressAll(t, selecting(t, daemon, 0), letter('s'))

	got := acted{Sent: daemon.sent, LastLine: lastLine(t, model)}

	want := acted{LastLine: "stop ci? y/n"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("after s = %+v, want %+v", got, want)
	}
}

// plan 6.3: s then n sends nothing, s then y sends terminate for the run
func TestStopKeyEndsTheRunOnlyAfterYes(t *testing.T) {
	cases := []struct {
		name   string
		answer tea.KeyPressMsg
		want   acted
	}{
		{"no", letter('n'), acted{LastLine: "↑/↓ select · f forward · p pause/resume · s stop · q quit"}},
		{"other key", letter('x'), acted{LastLine: "↑/↓ select · f forward · p pause/resume · s stop · q quit"}},
		{"yes", letter('y'), acted{
			Sent:     []protocol.Message{{Type: protocol.TypeTerminate, Terminate: &protocol.Target{ID: "run-test"}}},
			LastLine: "heavy: stopped run-test",
		}},
	}
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			daemon := &fakeDaemon{sendLine: "heavy: stopped run-test"}
			model := pressAll(t, selecting(t, daemon, 1), letter('s'), current.answer)

			got := acted{Sent: daemon.sent, LastLine: lastLine(t, model)}

			if !reflect.DeepEqual(got, current.want) {
				t.Fatalf("after s and %s = %+v, want %+v", current.answer, got, current.want)
			}
		})
	}
}

func TestStopAnsweredWithQDoesNotQuit(t *testing.T) {
	daemon := &fakeDaemon{}
	model, _ := press(t, selecting(t, daemon, 0), letter('s'))

	_, got := press(t, model, letter('q'))

	if got != nil || daemon.sent != nil {
		t.Fatalf("q after s = %#v and sent %+v, want no command and nothing sent", got, daemon.sent)
	}
}

func TestCtrlCQuitsWhileTheStopQuestionIsOpen(t *testing.T) {
	model, _ := press(t, selecting(t, &fakeDaemon{}, 0), letter('s'))

	_, got := press(t, model, keyCtrlC)

	if got != (tea.QuitMsg{}) {
		t.Fatalf("ctrl+c after s = %#v, want tea.QuitMsg{}", got)
	}
}
