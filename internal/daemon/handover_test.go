package daemon_test

import (
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/protocol"
)

const newerHeavy = `{"protocol":1,"version":"1.0.1","stamp":""}`

// commandOf starts a command in a process group of its own, as a client does for its run, and returns the group.
func commandOf(t *testing.T) int {
	t.Helper()
	command := exec.Command("sleep", "300.61")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		command.Wait()
	})
	return command.Process.Pid
}

// runningRun announces a run, takes its start and tells the daemon the process group of its command.
func runningRun(t *testing.T, state string) (string, net.Conn) {
	t.Helper()
	answer, connection := register(t, state)
	if answer.Type != protocol.TypeStart {
		t.Fatalf("registration answered %#v, want a start", answer)
	}
	started := *answer.Start
	started.ProcessGroup = commandOf(t)
	if err := protocol.Write(connection, protocol.Message{Type: protocol.TypeStart, Start: &started}); err != nil {
		t.Fatal(err)
	}
	// The daemon takes a pause only once it knows the process group, and control tries until then.
	control(t, state, protocol.Message{Type: protocol.TypePause, Pause: &protocol.Target{ID: started.ID}})
	control(t, state, protocol.Message{Type: protocol.TypeResume, Resume: &protocol.Target{ID: started.ID}})
	return started.ID, connection
}

// The update of heavy on a busy machine: the daemon holds a running and a waiting run when the first heavy of the
// newer version greets it. It ends at once and closes the connections of both runs, whose clients announce them to
// the daemon the newer heavy starts; the mark it leaves makes that daemon wait for them before it starts a run.
// The commands of the runs are children of their clients and run on.
func TestANewerHeavyTakesOverADaemonWithARunningAndAWaitingRun(t *testing.T) {
	current := startDaemon(t, protocol.Identity{Version: "1.0.0"})
	_, first := runningRun(t, current.state)
	waiting, second := register(t, current.state)
	if waiting.Type != protocol.TypeWaiting {
		t.Fatalf("the second run in the same worktree answered %#v, want a place in the queue", waiting)
	}

	hello, _, _ := greet(t, current.state, newerHeavy)

	if want := (protocol.Hello{Protocol: 1, Version: "1.0.0", Handover: true}); hello != want {
		t.Fatalf("answer to the newer heavy = %#v, want %#v", hello, want)
	}
	if err := current.ended(t); err != nil {
		t.Fatalf("Run = %v, want the daemon gone", err)
	}
	mark, err := os.ReadFile(filepath.Join(current.state, "daemon.runs"))
	if err != nil || string(mark) != "2\n" {
		t.Fatalf("the mark of the runs = %q, %v; want 2", mark, err)
	}
	for name, connection := range map[string]net.Conn{"running": first, "waiting": second} {
		connection.SetReadDeadline(time.Now().Add(5 * time.Second))
		// What the daemon told the run before comes first; the copy ends without an error only at the end of the stream.
		if _, err := io.Copy(io.Discard, connection); err != nil {
			t.Fatalf("the connection of the %s run: %v, want it closed so its client announces the run again", name, err)
		}
	}
}

// A client that got its start has not told the daemon the process group of its command yet, and what it would
// announce to a new daemon is still a waiting run: the daemon keeps serving until the command runs.
func TestADaemonKeepsARunThatIsStartingItsCommand(t *testing.T) {
	current := startDaemon(t, protocol.Identity{Version: "1.0.0"})
	answer, _ := register(t, current.state)
	if answer.Type != protocol.TypeStart {
		t.Fatalf("registration answered %#v, want a start", answer)
	}

	hello, _, _ := greet(t, current.state, newerHeavy)

	if hello != (protocol.Hello{Protocol: 1, Version: "1.0.0"}) {
		t.Fatalf("answer to the newer heavy = %#v, want no handover while a run starts its command", hello)
	}
	current.clock.advance(t, time.Second)
}

// A new daemon learns of a run only that it waits or runs. A paused run would stay paused for good, so the daemon
// that paused it keeps serving until it runs again.
func TestADaemonKeepsAPausedRunAndHandsOverOnceItRunsAgain(t *testing.T) {
	current := startDaemon(t, protocol.Identity{Version: "1.0.0"})
	id, _ := runningRun(t, current.state)
	control(t, current.state, protocol.Message{Type: protocol.TypePause, Pause: &protocol.Target{ID: id}})

	paused, _, _ := greet(t, current.state, newerHeavy)
	control(t, current.state, protocol.Message{Type: protocol.TypeResume, Resume: &protocol.Target{ID: id}})
	resumed, _, _ := greet(t, current.state, newerHeavy)

	if paused != (protocol.Hello{Protocol: 1, Version: "1.0.0"}) || resumed != (protocol.Hello{Protocol: 1, Version: "1.0.0", Handover: true}) {
		t.Fatalf("answers to the newer heavy = %#v while paused, %#v after the resume; want a handover only after the resume", paused, resumed)
	}
	if err := current.ended(t); err != nil {
		t.Fatalf("Run = %v, want the daemon gone", err)
	}
}
