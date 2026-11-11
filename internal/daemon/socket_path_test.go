package daemon_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/daemon"
)

// longState is a state directory whose socket path is longer than the 103 bytes macOS takes.
func longState(t *testing.T) string {
	t.Helper()
	state := filepath.Join(t.TempDir(), strings.Repeat("s", 60))
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	return state
}

// a socket path macOS cannot bind fails the client at once with the reason, not after the start wait with a pointer
// to a log the daemon never wrote
func TestConnectWithASocketPathTooLongNamesItAtOnce(t *testing.T) {
	state := longState(t)
	socket := filepath.Join(state, "daemon.sock")
	started := time.Now()

	_, err := daemon.Connect(daemon.ConnectOptions{StateDirectory: state, Binary: "/nonexistent/heavy"})

	want := "heavy: the socket path " + socket + " has " + strconv.Itoa(len(socket)) +
		" bytes, more than the 103 macOS allows; set HEAVY_STATE_DIRECTORY to a shorter directory"
	if err == nil || err.Error() != want || time.Since(started) > time.Second {
		t.Fatalf("Connect = %v after %s; want %q at once", err, time.Since(started), want)
	}
	if entries, _ := os.ReadDir(state); len(entries) != 0 {
		t.Fatalf("the state directory holds %v, want nothing", entries)
	}
}

func TestReachWithASocketPathTooLongFails(t *testing.T) {
	state := longState(t)

	_, found, err := daemon.Reach(daemon.ConnectOptions{StateDirectory: state})

	if found || err == nil || !strings.HasPrefix(err.Error(), "heavy: the socket path "+filepath.Join(state, "daemon.sock")+" has ") {
		t.Fatalf("Reach = %v, %v; want the socket path error", found, err)
	}
}
