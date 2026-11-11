package daemon_test

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/testkit"
)

// plan 4.1/4.3: hooks and shims ask a running daemon and never start one
func TestReachWithoutADaemonFindsNoneAndStartsNone(t *testing.T) {
	state := testkit.StateDir(t)

	connection, found, err := daemon.Reach(daemon.ConnectOptions{StateDirectory: state, Binary: "/nonexistent/heavy"})

	if connection != nil || found || err != nil {
		t.Fatalf("Reach = %v, %v, %v; want nothing found", connection, found, err)
	}
	if entries, _ := os.ReadDir(state); len(entries) != 0 {
		t.Fatalf("the state directory holds %v, want nothing started", entries)
	}
}

func TestReachTalksToARunningDaemon(t *testing.T) {
	current := startDaemon(t, protocol.Identity{})

	connection, found, err := daemon.Reach(daemon.ConnectOptions{StateDirectory: current.state, Binary: "/nonexistent/heavy"})
	if err != nil || !found {
		t.Fatalf("Reach = %v, %v; want the daemon", found, err)
	}
	defer connection.Close()
	if err := connection.Send(protocol.Message{Type: protocol.TypeStatus, Status: &protocol.Status{}}); err != nil {
		t.Fatal(err)
	}
	answer, err := connection.Receive()
	if err != nil || answer.Type != protocol.TypeStatus || len(answer.Status.Runs) != 0 {
		t.Fatalf("status answer %#v, %v", answer, err)
	}
}

// a socket nobody listens on, as a crashed daemon leaves it, counts as no daemon
func TestReachOfAStaleSocketFindsNone(t *testing.T) {
	state := testkit.StateDir(t)
	if err := os.WriteFile(filepath.Join(state, "daemon.sock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	_, found, err := daemon.Reach(daemon.ConnectOptions{StateDirectory: state})

	if found || err != nil {
		t.Fatalf("Reach = %v, %v; want nothing found", found, err)
	}
}

// critic 6: a daemon that accepts and never says hello fails Reach within the hello deadline instead of freezing
// the dashboard, the guard or a shim
func TestReachOfADaemonThatNeverGreetsFailsWithinFiveSeconds(t *testing.T) {
	state := testkit.StateDir(t)
	listener, err := net.Listen("unix", filepath.Join(state, "daemon.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			t.Cleanup(func() { connection.Close() })
		}
	}()
	started := time.Now()

	_, found, err := daemon.Reach(daemon.ConnectOptions{StateDirectory: state})

	took := time.Since(started)
	if found || err == nil || !errors.Is(err, os.ErrDeadlineExceeded) || took < 4*time.Second || took > 7*time.Second {
		t.Fatalf("Reach = %v, %v after %s; want a deadline error after about 5s", found, err, took)
	}
}
