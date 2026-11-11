package daemon_test

import (
	"testing"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// critic 2.5: a run whose client has not told the daemon its process group yet cannot be paused or stopped, and
// the user hears why instead of a success line
func TestControlOfARunWithoutItsProcessGroupYetFails(t *testing.T) {
	current := startDaemon(t, protocol.Identity{})
	started, _ := register(t, current.state)
	id := started.Start.ID

	target := &protocol.Target{ID: id}
	for _, request := range []protocol.Message{{Type: protocol.TypePause, Pause: target}, {Type: protocol.TypeTerminate, Terminate: target}} {
		kind := request.Type
		_, reader, connection := greet(t, current.state, `{"protocol":1,"version":"","stamp":""}`)
		if err := protocol.Write(connection, request); err != nil {
			t.Fatalf("sending %s: %v", kind, err)
		}
		answer, err := protocol.ReadMessage(reader)
		want := protocol.Message{Type: protocol.TypeError, Error: &protocol.Failure{Text: "heavy: run " + id + " is starting its command; try again in a moment"}}
		if err != nil || answer.Type != want.Type || answer.Error == nil || *answer.Error != *want.Error {
			t.Fatalf("%s answer = %s %+v, %v; want %q", kind, answer.Type, answer.Error, err, want.Error.Text)
		}
	}
	if runs := status(t, current.state); len(runs) != 1 || runs[0].State != "running" || runs[0].PausedReason != "" {
		t.Fatalf("status = %#v, want the run still running", runs)
	}
}
