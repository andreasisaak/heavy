package herdr_test

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/herdr"
	"github.com/andreasisaak/heavy/internal/testkit"
)

// shortTemp is a temporary herdr config directory with a path short enough for the sockets inside it.
func shortTemp(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp("", "hh-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	return directory
}

// listen starts a server on socket for the test, as a running herdr would.
func listen(t *testing.T, socket string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
}

// heavy 1.0 on 2026-11-09: the daemon refreshed only the herdr server whose socket it inherited, so the panes of
// the other servers (herdr --session shopapp beside the default one) showed no heavy rows
func TestServersAreTheSocketsOfEveryRunningHerdr(t *testing.T) {
	config := shortTemp(t)
	main := filepath.Join(config, "herdr.sock")
	named := filepath.Join(config, "sessions", "shopapp", "herdr.sock")
	listen(t, main)
	listen(t, named)
	gone := filepath.Join(config, "sessions", "old", "herdr.sock")
	if err := os.MkdirAll(filepath.Dir(gone), 0o700); err != nil {
		t.Fatal(err)
	}
	ended, err := net.Listen("unix", gone)
	if err != nil {
		t.Fatal(err)
	}
	ended.(*net.UnixListener).SetUnlinkOnClose(false)
	ended.Close()

	servers, err := herdr.Servers(config)

	if want := []string{main, named}; err != nil || !reflect.DeepEqual(servers, want) {
		t.Fatalf("Servers = %v, %v; want %v", servers, err, want)
	}
}

func TestServersOfAConfigWithoutHerdrAreNone(t *testing.T) {
	servers, err := herdr.Servers(shortTemp(t))

	if err != nil || len(servers) != 0 {
		t.Fatalf("Servers = %v, %v; want none", servers, err)
	}
}

func TestRedrawAllRunsTheRefreshInEveryServer(t *testing.T) {
	directory := t.TempDir()
	log := filepath.Join(directory, "calls")
	fake := testkit.FakeProgram(t, directory, "herdr", `echo "$HERDR_SOCKET_PATH $*" >> "`+log+`"`)

	err := herdr.RedrawAll(fake, []string{"/h/herdr.sock", "/h/sessions/shopapp/herdr.sock"}, 5*time.Second)

	calls, readErr := os.ReadFile(log)
	want := "/h/herdr.sock plugin action invoke refresh --plugin local.heavy-queue\n" +
		"/h/sessions/shopapp/herdr.sock plugin action invoke refresh --plugin local.heavy-queue\n"
	if err != nil || readErr != nil || string(calls) != want {
		t.Fatalf("RedrawAll = %v, calls %q (%v), want nil and %q", err, calls, readErr, want)
	}
}

// a server whose refresh fails is named, and the servers after it still get theirs
func TestRedrawAllNamesTheServerWhoseRefreshFailed(t *testing.T) {
	directory := t.TempDir()
	log := filepath.Join(directory, "calls")
	fake := testkit.FakeProgram(t, directory, "herdr", `echo "$HERDR_SOCKET_PATH" >> "`+log+`"
case "$HERDR_SOCKET_PATH" in /h/broken.sock) echo "example refusal" >&2; exit 1 ;; esac`)

	err := herdr.RedrawAll(fake, []string{"/h/broken.sock", "/h/herdr.sock"}, 5*time.Second)

	calls, _ := os.ReadFile(log)
	wantError := "heavy: " + fake + " plugin action invoke refresh on /h/broken.sock: exit status 1: example refusal"
	if err == nil || err.Error() != wantError || string(calls) != "/h/broken.sock\n/h/herdr.sock\n" {
		t.Fatalf("RedrawAll = %v, calls %q; want %q and both servers called", err, calls, wantError)
	}
	if strings.Contains(err.Error(), "/h/herdr.sock") {
		t.Fatalf("RedrawAll = %v names the server that worked", err)
	}
}
