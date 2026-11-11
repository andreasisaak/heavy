package daemon_test

import (
	"bufio"
	"regexp"
	"strings"
	"testing"

	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/slots"
)

// statusFor asks for the status as a heavy of the given version and returns the answer as sent and as read.
func statusFor(t *testing.T, state, version string) (string, []protocol.RunStatus) {
	t.Helper()
	_, reader, connection := greet(t, state, `{"protocol":1,"version":"`+version+`","stamp":"","reader":true}`)
	defer connection.Close()
	if err := protocol.Write(connection, protocol.Message{Type: protocol.TypeStatus, Status: &protocol.Status{}}); err != nil {
		t.Fatalf("asking for the status: %v", err)
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("reading the status: %v", err)
	}
	answer, err := protocol.ReadMessage(bufio.NewReader(strings.NewReader(line)))
	if err != nil || answer.Type != protocol.TypeStatus {
		t.Fatalf("status answer %q, %v", line, err)
	}
	return line, answer.Status.Runs
}

// The status names for a waiting run what keeps it. A heavy before 1.3.0 reads the status strictly and fails on a
// field it does not know, in the hook of every Bash call, so the daemon leaves the reason out for it.
func TestTheStatusNamesWhyARunWaitsForAHeavyThatKnowsTheField(t *testing.T) {
	current := startDaemon(t, protocol.Identity{Version: "1.3.0"})
	started, _ := register(t, current.state)
	register(t, current.state)

	for version, knows := range map[string]bool{"1.3.0": true, "1.10.0": true, "2.0.0": true, "": true, "1.2.0": false, "1.2.9": false, "0.9.0": false} {
		line, runs := statusFor(t, current.state, version)

		want := ""
		if knows {
			want = "same worktree as run " + started.Start.ID
		}
		if len(runs) != 2 || runs[0].Waits != "" || runs[1].Waits != want || strings.Contains(line, `"waits"`) != knows {
			t.Fatalf("status for heavy %q = %s\nwant the reason %q on the waiting run alone", version, line, want)
		}
	}
}

// While the daemons of the machine hold every slot, the status says so.
func TestTheStatusNamesTheMachineSlotsARunWaitsFor(t *testing.T) {
	scene := startLoadDaemon(t, plenty)
	config := scene.limitMachine(t, 1)
	other := slots.NewPool()
	hold(t, other, config)
	run := scene.start(t, repository(t), "sleep", "300.62")
	scene.step(t, 3, "line about the machine slot", func() bool {
		return run.has(`heavy: waits for one of the 1 machine slots \(` + regexp.QuoteMeta(config.Folder) + `\)\n`)
	})

	_, runs := statusFor(t, scene.state, "1.3.0")

	want := "machine slots: the runs of this machine hold all 1 (" + config.Folder + ")"
	if len(runs) != 1 || runs[0].Waits != want {
		t.Fatalf("status = %+v, want one waiting run with the reason %q", runs, want)
	}
	other.Release(1)
	scene.step(t, 3, "start after the release", func() bool { return run.has(startedLine + `sleep 300.62\n`) })
	scene.end(t, run)
}
