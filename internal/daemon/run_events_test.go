package daemon_test

import (
	"net"
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// eventRun registers a run of the session over a connection of the test and answers its start with a process
// group of the test's own, a sleep that the daemon may end.
func eventRun(t *testing.T, state, session string) (id string, write func(protocol.Message), connection net.Conn) {
	t.Helper()
	_, reader, connection := greet(t, state, `{"protocol":1,"version":"","stamp":""}`)
	write = func(message protocol.Message) {
		t.Helper()
		if err := protocol.Write(connection, message); err != nil {
			t.Fatal(err)
		}
	}
	write(protocol.Message{Type: protocol.TypeRegister, Register: &protocol.Register{
		Label: "ci", Command: []string{"make", "ci"}, Directory: state, Checkout: state, Worktree: state, Session: session, ClientPID: os.Getpid(),
	}})
	answer, err := protocol.ReadMessage(reader)
	if err != nil || answer.Type != protocol.TypeStart {
		t.Fatalf("registration answered %#v, %v, want a start", answer, err)
	}
	command := exec.Command("sleep", "30.97")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	started := *answer.Start
	started.ProcessGroup = command.Process.Pid
	write(protocol.Message{Type: protocol.TypeStart, Start: &started})
	return started.ID, write, connection
}

// The monitor of a session hears the start of its run and its end with the exit status, so the session needs no
// watch of its own on the output.
func TestSubscriberHearsTheStartAndTheEndOfARun(t *testing.T) {
	current := startDaemon(t, protocol.Identity{})
	_, monitor := subscribe(t, current.state, protocol.Subscribe{Session: "s-1"})

	id, write, _ := eventRun(t, current.state, "s-1")
	started := monitor.next(t)
	write(protocol.Message{Type: protocol.TypeEnded, Ended: &protocol.Ended{ExitStatus: 2}})
	ended := monitor.next(t)

	expectEvent(t, started, "s-1", `heavy: your run ci \(`+id+`\) started at \d{2}:\d{2}:\d{2} with 18 cores`)
	expectEvent(t, ended, "s-1", `heavy: your run ci \(`+id+`\) ended at \d{2}:\d{2}:\d{2} with exit status 2 \(failed\) after 0s`)
}

func TestSubscriberHearsAGreenEnd(t *testing.T) {
	current := startDaemon(t, protocol.Identity{})
	_, monitor := subscribe(t, current.state, protocol.Subscribe{Session: "s-1"})
	id, write, _ := eventRun(t, current.state, "s-1")
	monitor.next(t)

	write(protocol.Message{Type: protocol.TypeEnded, Ended: &protocol.Ended{ExitStatus: 0}})

	expectEvent(t, monitor.next(t), "s-1", `heavy: your run ci \(`+id+`\) ended at \d{2}:\d{2}:\d{2} with exit status 0 \(passed\) after 0s`)
}

// The first red line of a run reaches the monitor at once, before the run has ended.
func TestSubscriberHearsTheFirstRedLineOfARun(t *testing.T) {
	current := startDaemon(t, protocol.Identity{})
	_, monitor := subscribe(t, current.state, protocol.Subscribe{Session: "s-1"})
	id, write, _ := eventRun(t, current.state, "s-1")
	monitor.next(t)

	write(protocol.Message{Type: protocol.TypeRed, Red: &protocol.Red{Line: "First red in PHPUnit at 12:00:00: CartTest"}})
	red := monitor.next(t)
	write(protocol.Message{Type: protocol.TypeRed, Red: &protocol.Red{Line: "a second red line"}})
	write(protocol.Message{Type: protocol.TypeEnded, Ended: &protocol.Ended{ExitStatus: 143}})
	ended := monitor.next(t)

	expectEvent(t, red, "s-1", `heavy: your run ci \(`+id+`\) is red since \d{2}:\d{2}:\d{2} and is being stopped: First red in PHPUnit at 12:00:00: CartTest`)
	expectEvent(t, ended, "s-1", `heavy: your run ci \(`+id+`\) ended at \d{2}:\d{2}:\d{2} with exit status 143 \(red\) after 0s`)
}

// A run whose client goes away ends as stopped.
func TestSubscriberHearsTheEndOfARunWhoseClientWentAway(t *testing.T) {
	current := startDaemon(t, protocol.Identity{})
	_, monitor := subscribe(t, current.state, protocol.Subscribe{Session: "s-1"})
	id, _, connection := eventRun(t, current.state, "s-1")
	monitor.next(t)

	connection.Close()

	expectEvent(t, monitor.next(t), "s-1", `heavy: your run ci \(`+id+`\) ended at \d{2}:\d{2}:\d{2} with exit status -1 \(stopped\) after 0s`)
}
