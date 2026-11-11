package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/testkit"
)

func TestDaemonLogsItsStart(t *testing.T) {
	current := startDaemon(t, protocol.Identity{Version: "1.0.0", Stamp: "abc"})

	want := []map[string]any{{"level": "INFO", "msg": "daemon started", "pid": float64(os.Getpid()), "version": "1.0.0", "stamp": "abc", "protocol": float64(1), "vm": "not measured: no VM in this test"}}
	if got := logRecords(t, current.state); !reflect.DeepEqual(got, want) {
		t.Fatalf("daemon.log = %v, want %v", got, want)
	}
}

func TestSecondDaemonEndsAndLogsTheRunningOne(t *testing.T) {
	current := startDaemon(t, protocol.Identity{})
	command := exec.Command(testkit.BuildBinary(t), "--daemon")
	command.Env = append(os.Environ(), "HEAVY_STATE_DIRECTORY="+current.state)

	output, err := command.CombinedOutput()

	if err != nil || len(output) != 0 {
		t.Fatalf("second daemon ended with %v and output %q, want exit 0 and no output", err, output)
	}
	records := logRecords(t, current.state)
	want := map[string]any{"level": "INFO", "msg": "daemon already running", "holder": float64(os.Getpid())}
	if last := records[len(records)-1]; !reflect.DeepEqual(last, want) {
		t.Fatalf("last log record = %v, want %v", last, want)
	}
}

func TestOtherProtocolIsRefusedWithItsNumbers(t *testing.T) {
	current := startDaemon(t, protocol.Identity{Version: "1.0.0"})

	answer, reader, _ := greet(t, current.state, `{"protocol":2,"version":"2.0.0","stamp":"x"}`)

	want := protocol.Hello{Protocol: 1, Version: "1.0.0", Error: "heavy: the running daemon speaks protocol 1, this heavy 2; restart the session"}
	if answer != want {
		t.Fatalf("answer = %#v, want %#v", answer, want)
	}
	if rest, err := io.ReadAll(reader); err != nil || len(rest) != 0 {
		t.Fatalf("after the refusal the daemon sent %q, %v; want the connection closed", rest, err)
	}
}

func TestUnknownMessageGetsAnErrorAndTheConnectionEnds(t *testing.T) {
	current := startDaemon(t, protocol.Identity{})
	_, reader, connection := greet(t, current.state, `{"protocol":1,"version":"","stamp":""}`)

	if _, err := connection.Write([]byte(`{"type":"jump"}` + "\n")); err != nil {
		t.Fatalf("sending: %v", err)
	}
	answer, err := protocol.ReadMessage(reader)

	want := protocol.Message{Type: protocol.TypeError, Error: &protocol.Failure{Text: `heavy: expected a message whose payload fits its type, got {"type":"jump"}`}}
	if err != nil || !reflect.DeepEqual(answer, want) {
		t.Fatalf("answer = %#v, %v; want %#v", answer, err, want)
	}
	if rest, err := io.ReadAll(reader); err != nil || len(rest) != 0 {
		t.Fatalf("after the error the daemon sent %q, %v; want the connection closed", rest, err)
	}
}

func TestIdleDaemonEndsAfterTenMinutesAndRemovesItsSocket(t *testing.T) {
	current := startDaemon(t, protocol.Identity{})
	socket := filepath.Join(current.state, "daemon.sock")

	current.clock.advance(t, 9*time.Minute+59*time.Second)
	if !exists(socket) {
		t.Fatalf("the daemon removed its socket before ten idle minutes")
	}
	current.clock.advanceOnce(t, time.Second)

	if err := current.ended(t); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if exists(socket) {
		t.Fatalf("socket %s is left", socket)
	}
	records := logRecords(t, current.state)
	want := map[string]any{"level": "INFO", "msg": "daemon ends", "reason": "idle for 10m0s"}
	if last := records[len(records)-1]; !reflect.DeepEqual(last, want) {
		t.Fatalf("last log record = %v, want %v", last, want)
	}
}

func TestNewerHeavyTakesOverAnIdleDaemon(t *testing.T) {
	cases := []struct {
		hello    string
		handover bool
	}{
		{`{"protocol":1,"version":"1.0.1","stamp":""}`, true},
		{`{"protocol":1,"version":"1.0.0","stamp":"other"}`, true},
		{`{"protocol":1,"version":"0.9.0","stamp":""}`, false},
		{`{"protocol":1,"version":"1.0.0","stamp":"own"}`, false},
		{`{"protocol":1,"version":"1.0.0","stamp":"stale"}`, false},
		// critic 6: a heavy that only reads (guard, shim, herdr refresh, dashboard) replaces no daemon
		{`{"protocol":1,"version":"1.0.1","stamp":"","reader":true}`, false},
	}
	for _, current := range cases {
		running := startDaemonWith(t, protocol.Identity{Version: "1.0.0", Stamp: "own"}, func(options *daemon.Options) {
			options.InstalledStamp = func() (string, error) { return "other", nil }
		})

		answer, _, _ := greet(t, running.state, current.hello)

		want := protocol.Hello{Protocol: 1, Version: "1.0.0", Stamp: "own", Handover: current.handover}
		if answer != want {
			t.Fatalf("%s: answer = %#v, want %#v", current.hello, answer, want)
		}
		if current.handover {
			if err := running.ended(t); err != nil || exists(filepath.Join(running.state, "daemon.sock")) {
				t.Fatalf("%s: Run = %v with the socket left %v; want the daemon gone", current.hello, err, exists(filepath.Join(running.state, "daemon.sock")))
			}
			// heavy-37 2026-11-08: the end names the identity of the newer heavy apart from the daemon's own
			var newer protocol.Hello
			if err := json.Unmarshal([]byte(current.hello), &newer); err != nil {
				t.Fatal(err)
			}
			records := logRecords(t, running.state)
			wantEnd := map[string]any{"level": "INFO", "msg": "daemon ends", "reason": "a newer heavy takes over", "newer_version": newer.Version, "newer_stamp": newer.Stamp}
			if last := records[len(records)-1]; !reflect.DeepEqual(last, wantEnd) {
				t.Fatalf("%s: last log record = %v, want %v", current.hello, last, wantEnd)
			}
		} else {
			running.clock.advance(t, time.Second)
		}
	}
}

func TestStaleSocketOfAGoneDaemonIsReplaced(t *testing.T) {
	state := testkit.StateDir(t)
	listener, err := net.Listen("unix", filepath.Join(state, "daemon.sock"))
	if err != nil {
		t.Fatalf("creating a socket: %v", err)
	}
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- daemon.Run(ctx, options(state, protocol.Identity{}, newFakeClock()))
	}()
	defer func() { cancel(); <-done }()

	waitFor(t, func() bool {
		connection, err := net.Dial("unix", filepath.Join(state, "daemon.sock"))
		if err == nil {
			connection.Close()
		}
		return err == nil
	}, "the new daemon to listen on the stale socket's path")
}

func TestLogOverTenMegabytesMovesToTheFirstOldLog(t *testing.T) {
	state := testkit.StateDir(t)
	old := strings.Repeat("x", 10<<20)
	if err := os.WriteFile(filepath.Join(state, "daemon.log"), []byte(old), 0o644); err != nil {
		t.Fatalf("writing the old log: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- daemon.Run(ctx, options(state, protocol.Identity{}, newFakeClock()))
	}()
	waitFor(t, func() bool { return exists(filepath.Join(state, "daemon.log.1")) }, "the log to move")
	cancel()
	<-done

	moved, err := os.ReadFile(filepath.Join(state, "daemon.log.1"))
	if err != nil || string(moved) != old {
		t.Fatalf("daemon.log.1 holds %d bytes, %v; want the old 10 MiB", len(moved), err)
	}
	records := logRecords(t, state)
	if len(records) != 2 || records[0]["msg"] != "daemon started" || records[1]["msg"] != "daemon ends" {
		t.Fatalf("daemon.log = %v, want the start and the end of this daemon", records)
	}
}

func TestStateDirectoryThatCannotBeCreatedIsAnError(t *testing.T) {
	file := filepath.Join(testkit.StateDir(t), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatalf("writing %s: %v", file, err)
	}

	err := daemon.Run(context.Background(), options(filepath.Join(file, "state"), protocol.Identity{}, newFakeClock()))

	want := "heavy: creating the state directory " + filepath.Join(file, "state") + ": mkdir " + file + ": not a directory"
	if err == nil || err.Error() != want {
		t.Fatalf("Run error = %v, want %q", err, want)
	}
}

func TestSocketPathTooLongForMacOSIsAnError(t *testing.T) {
	state := filepath.Join(testkit.StateDir(t), strings.Repeat("d", 100))

	err := daemon.Run(context.Background(), options(state, protocol.Identity{}, newFakeClock()))

	socket := filepath.Join(state, "daemon.sock")
	want := "heavy: the socket path " + socket + " has " + strconv.Itoa(len(socket)) + " bytes, more than the 103 macOS allows; set HEAVY_STATE_DIRECTORY to a shorter directory"
	if err == nil || err.Error() != want {
		t.Fatalf("Run error = %v, want %q", err, want)
	}
}

func TestConnectStartsADaemonWithinTwoSeconds(t *testing.T) {
	state := testkit.StateDir(t)
	started := time.Now()

	connection, err := daemon.Connect(daemon.ConnectOptions{StateDirectory: state, Binary: testkit.BuildBinary(t)})

	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer connection.Close()
	if elapsed := time.Since(started); elapsed >= 2*time.Second {
		t.Fatalf("Connect took %s, want under 2s", elapsed)
	}
	pid := daemonPid(t, state)
	t.Cleanup(func() { stopProcess(t, pid) })
	if connection.Daemon != (protocol.Hello{Protocol: 1}) {
		t.Fatalf("daemon hello = %#v, want protocol 1 with an empty identity", connection.Daemon)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("daemon %d is not running: %v", pid, err)
	}
}

func TestConnectReportsTheRefusalOfTheDaemon(t *testing.T) {
	state := testkit.StateDir(t)
	listener, err := net.Listen("unix", filepath.Join(state, "daemon.sock"))
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	defer listener.Close()
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		_, _ = connection.Read(make([]byte, 512))
		_, _ = connection.Write([]byte(`{"protocol":2,"version":"","stamp":"","error":"heavy: the running daemon speaks protocol 2, this heavy 1; restart the session"}` + "\n"))
	}()

	_, err = daemon.Connect(daemon.ConnectOptions{StateDirectory: state, Binary: testkit.BuildBinary(t)})

	if want := "heavy: the running daemon speaks protocol 2, this heavy 1; restart the session"; err == nil || err.Error() != want {
		t.Fatalf("Connect error = %v, want %q", err, want)
	}
	// critic 5.1: a refusal does not change by asking again, so a monitor can tell it from a lost daemon
	var refused *daemon.RefusedError
	if !errors.As(err, &refused) || refused.Text != err.Error() {
		t.Fatalf("Connect error %#v is no RefusedError with its text", err)
	}
}

func TestConnectWithAMissingBinaryIsAnError(t *testing.T) {
	state := testkit.StateDir(t)
	binary := filepath.Join(t.TempDir(), "heavy")

	_, err := daemon.Connect(daemon.ConnectOptions{StateDirectory: state, Binary: binary})

	want := "heavy: starting the daemon through " + binary + " --daemon --detach failed: fork/exec " + binary + ": no such file or directory"
	if err == nil || err.Error() != want {
		t.Fatalf("Connect error = %v, want %q", err, want)
	}
}

// V43: the step that detaches the daemon fails with what it said
func TestConnectWhoseDetachingStepFailsNamesItsOutput(t *testing.T) {
	state := testkit.StateDir(t)
	binary := filepath.Join(t.TempDir(), "heavy")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho \"heavy: example failure of $*\" >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := daemon.Connect(daemon.ConnectOptions{StateDirectory: state, Binary: binary})

	want := "heavy: starting the daemon through " + binary + " --daemon --detach failed: exit status 1: heavy: example failure of --daemon --detach"
	if err == nil || err.Error() != want {
		t.Fatalf("Connect error = %v, want %q", err, want)
	}
}

// daemonPid reads the pid the daemon wrote into its lock file.
func daemonPid(t *testing.T, state string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(state, "daemon.lock"))
	if err != nil {
		t.Fatalf("reading daemon.lock: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("daemon.lock holds %q, want a pid", data)
	}
	return pid
}

func stopProcess(t *testing.T, pid int) {
	t.Helper()
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		t.Errorf("stopping daemon %d: %v", pid, err)
		return
	}
	waitFor(t, func() bool { return syscall.Kill(pid, 0) != nil }, "the daemon to end")
}

func TestGarbledHelloIsRefusedWithTheReason(t *testing.T) {
	current := startDaemon(t, protocol.Identity{})

	answer, _, _ := greet(t, current.state, "not json")

	want := protocol.Hello{Protocol: 1, Error: "heavy: expected a hello of protocol 1, got not json: invalid character 'o' in literal null (expecting 'u')"}
	if answer != want {
		t.Fatalf("answer = %#v, want %#v", answer, want)
	}
}

func TestSilentConnectionIsClosedAfterTheHelloTimeout(t *testing.T) {
	current := startDaemon(t, protocol.Identity{})
	connection, err := net.Dial("unix", filepath.Join(current.state, "daemon.sock"))
	if err != nil {
		t.Fatalf("dialing: %v", err)
	}
	defer connection.Close()
	if err := connection.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("setting a deadline: %v", err)
	}

	rest, err := io.ReadAll(connection)

	if err != nil || len(rest) != 0 {
		t.Fatalf("silent connection got %q, %v; want it closed without an answer", rest, err)
	}
}

func TestFailedLogRotationKeepsTheRecord(t *testing.T) {
	state := testkit.StateDir(t)
	if err := os.WriteFile(filepath.Join(state, "daemon.log"), []byte(strings.Repeat("x", 10<<20)+"\n"), 0o644); err != nil {
		t.Fatalf("writing the old log: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(state, "daemon.log.1", "blocked"), 0o755); err != nil {
		t.Fatalf("blocking the rotation: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- daemon.Run(ctx, options(state, protocol.Identity{}, newFakeClock()))
	}()
	waitFor(t, func() bool { return exists(filepath.Join(state, "daemon.sock")) }, "the daemon to listen")
	cancel()
	<-done

	data, err := os.ReadFile(filepath.Join(state, "daemon.log"))
	if err != nil {
		t.Fatalf("reading daemon.log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 || !strings.Contains(lines[1], `"msg":"daemon started"`) || !strings.Contains(lines[2], `"msg":"daemon ends"`) {
		t.Fatalf("daemon.log ends with %q, want the old content, the start and the end", lines[1:])
	}
}

func TestLockThatCannotBeOpenedIsAnError(t *testing.T) {
	state := testkit.StateDir(t)
	lock := filepath.Join(state, "daemon.lock")
	if err := os.Mkdir(lock, 0o755); err != nil {
		t.Fatalf("blocking the lock: %v", err)
	}

	err := daemon.Run(context.Background(), options(state, protocol.Identity{}, newFakeClock()))

	want := "heavy: opening the daemon lock: open " + lock + ": is a directory"
	if err == nil || err.Error() != want {
		t.Fatalf("Run error = %v, want %q", err, want)
	}
}

func TestSocketPathTakenByADirectoryIsAnError(t *testing.T) {
	state := testkit.StateDir(t)
	socket := filepath.Join(state, "daemon.sock")
	if err := os.MkdirAll(filepath.Join(socket, "taken"), 0o755); err != nil {
		t.Fatalf("blocking the socket path: %v", err)
	}

	err := daemon.Run(context.Background(), options(state, protocol.Identity{}, newFakeClock()))

	want := "heavy: removing the stale socket " + socket + ": remove " + socket + ": directory not empty"
	if err == nil || err.Error() != want {
		t.Fatalf("Run error = %v, want %q", err, want)
	}
}

func TestListenFailureIsAnError(t *testing.T) {
	state := testkit.StateDir(t)
	for _, name := range []string{"daemon.lock", "daemon.log"} {
		if err := os.WriteFile(filepath.Join(state, name), nil, 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	if err := os.Chmod(state, 0o500); err != nil {
		t.Fatalf("closing %s for writing: %v", state, err)
	}
	t.Cleanup(func() { os.Chmod(state, 0o700) })

	err := daemon.Run(context.Background(), options(state, protocol.Identity{}, newFakeClock()))

	socket := filepath.Join(state, "daemon.sock")
	want := "heavy: listening on " + socket + ": listen unix " + socket + ": bind: permission denied"
	if err == nil || err.Error() != want {
		t.Fatalf("Run error = %v, want %q", err, want)
	}
}

func TestConnectOfANewerHeavyReplacesTheIdleDaemon(t *testing.T) {
	current := startDaemon(t, protocol.Identity{Version: "1.0.0"})
	binary := pluginBinary(t, "1.0.1")

	connection, err := daemon.Connect(daemon.ConnectOptions{StateDirectory: current.state, Binary: binary, Identity: protocol.Identity{Version: "1.0.1"}})

	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer connection.Close()
	t.Cleanup(func() { stopProcess(t, daemonPid(t, current.state)) })
	if err := current.ended(t); err != nil {
		t.Fatalf("the old daemon ended with %v", err)
	}
	if connection.Daemon != (protocol.Hello{Protocol: 1, Version: "1.0.1"}) {
		t.Fatalf("daemon hello = %#v, want the new daemon of 1.0.1", connection.Daemon)
	}
}

func TestConnectWithoutAnAnsweringDaemonGivesUpAfterTwoSeconds(t *testing.T) {
	state := testkit.StateDir(t)
	binary := testkit.FakeProgram(t, t.TempDir(), "heavy", "exit 0")
	started := time.Now()

	_, err := daemon.Connect(daemon.ConnectOptions{StateDirectory: state, Binary: binary})

	want := "heavy: no daemon answered on " + filepath.Join(state, "daemon.sock") + " within 2s; see " + filepath.Join(state, "daemon.log")
	if err == nil || err.Error() != want {
		t.Fatalf("Connect error = %v, want %q", err, want)
	}
	if elapsed := time.Since(started); elapsed < 2*time.Second || elapsed > 3*time.Second {
		t.Fatalf("Connect gave up after %s, want about 2s", elapsed)
	}
}

func TestTerminatedDaemonRemovesItsSocketAndLogsTheEnd(t *testing.T) {
	state := testkit.StateDir(t)
	connection, err := daemon.Connect(daemon.ConnectOptions{StateDirectory: state, Binary: testkit.BuildBinary(t)})
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	connection.Close()

	stopProcess(t, daemonPid(t, state))

	if exists(filepath.Join(state, "daemon.sock")) {
		t.Fatalf("the terminated daemon left its socket")
	}
	records := logRecords(t, state)
	want := map[string]any{"level": "INFO", "msg": "daemon ends", "reason": "stopped"}
	if last := records[len(records)-1]; !reflect.DeepEqual(last, want) {
		t.Fatalf("last log record = %v, want %v", last, want)
	}
}

// pluginBinary lays out a plugin of the given version around a copy of the heavy binary and returns its path.
func pluginBinary(t *testing.T, version string) string {
	t.Helper()
	root := t.TempDir()
	for _, folder := range []string{"bin", ".claude-plugin"} {
		if err := os.Mkdir(filepath.Join(root, folder), 0o755); err != nil {
			t.Fatalf("creating %s: %v", folder, err)
		}
	}
	data, err := os.ReadFile(testkit.BuildBinary(t))
	if err != nil {
		t.Fatalf("reading the heavy binary: %v", err)
	}
	binary := filepath.Join(root, "bin", "heavy")
	if err := os.WriteFile(binary, data, 0o755); err != nil {
		t.Fatalf("copying the heavy binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude-plugin", "plugin.json"), []byte(`{"version": "`+version+`"}`), 0o644); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}
	return binary
}

// register announces a run of this test on its own connection and returns the daemon's first answer.
func register(t *testing.T, state string) (protocol.Message, net.Conn) {
	t.Helper()
	_, reader, connection := greet(t, state, `{"protocol":1,"version":"","stamp":""}`)
	message := protocol.Message{Type: protocol.TypeRegister, Register: &protocol.Register{
		Label: "sleep", Command: []string{"sleep", "1"}, Directory: state, Checkout: state, Worktree: state, ClientPID: os.Getpid(),
	}}
	if err := protocol.Write(connection, message); err != nil {
		t.Fatalf("registering: %v", err)
	}
	answer, err := protocol.ReadMessage(reader)
	if err != nil {
		t.Fatalf("reading the answer to the registration: %v", err)
	}
	return answer, connection
}

func TestIdleTimeCountsFromTheEndOfTheLastRun(t *testing.T) {
	current := startDaemon(t, protocol.Identity{})
	_, connection := register(t, current.state)

	current.clock.advance(t, 11*time.Minute)
	if err := protocol.Write(connection, protocol.Message{Type: protocol.TypeEnded, Ended: &protocol.Ended{ExitStatus: 0}}); err != nil {
		t.Fatalf("ending the run: %v", err)
	}
	waitFor(t, func() bool {
		records := logRecords(t, current.state)
		return records[len(records)-1]["msg"] == "run ended"
	}, "the run to end")
	current.clock.advance(t, 9*time.Minute)
	if !exists(filepath.Join(current.state, "daemon.sock")) {
		t.Fatalf("the daemon ended 9 minutes after its last run")
	}
	current.clock.advanceOnce(t, time.Minute+time.Second)

	if err := current.ended(t); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
}

// announce sends a registration on a fresh connection and returns the daemon's first answer and the reader of
// the connection.
func announce(t *testing.T, state string, registration protocol.Register) (protocol.Message, *bufio.Reader) {
	t.Helper()
	_, reader, connection := greet(t, state, `{"protocol":1,"version":"","stamp":""}`)
	if err := protocol.Write(connection, protocol.Message{Type: protocol.TypeRegister, Register: &registration}); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if err := connection.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("setting a deadline: %v", err)
	}
	answer, err := protocol.ReadMessage(reader)
	if err != nil {
		t.Fatalf("reading the answer to the registration: %v", err)
	}
	return answer, reader
}

func refusal(text string) protocol.Message {
	return protocol.Message{Type: protocol.TypeError, Error: &protocol.Failure{Text: text}}
}

func TestAdoptionThatDoesNotFitIsRefused(t *testing.T) {
	current := startDaemon(t, protocol.Identity{})
	started, _ := register(t, current.state)
	cases := []struct {
		adopt protocol.Adoption
		want  string
	}{
		{protocol.Adoption{ID: started.Start.ID, State: "waiting", Since: 1, Order: 1},
			"heavy: run " + started.Start.ID + " is in the queue already; heavy --status shows the runs"},
		{protocol.Adoption{ID: "abc", State: "paused", Since: 1, Order: 1},
			`heavy: expected the state "waiting" or "running" of the run abc to take over, got "paused"`},
		{protocol.Adoption{ID: "abc", State: "running", Since: 1, Order: 1, ProcessGroup: 4242, Cores: 0},
			"heavy: expected a running run abc with at least one core and a process group above 1, got 0 cores and group 4242"},
		{protocol.Adoption{ID: "abc", State: "running", Since: 1, Order: 1, ProcessGroup: 1, Cores: 4},
			"heavy: expected a running run abc with at least one core and a process group above 1, got 4 cores and group 1"},
	}
	for _, testCase := range cases {
		adopt := testCase.adopt
		answer, _ := announce(t, current.state, protocol.Register{Label: "sleep", Command: []string{"sleep"}, Checkout: "/c2", Worktree: "/c2", Adopt: &adopt})
		if !reflect.DeepEqual(answer, refusal(testCase.want)) {
			t.Errorf("adopting %#v answered %#v, want %q", testCase.adopt, answer, testCase.want)
		}
	}
}

func TestBrokenHistoryRefusesANewRunWithItsLine(t *testing.T) {
	current := startDaemon(t, protocol.Identity{})
	path := filepath.Join(current.state, "history", "2026-11-07.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating the history folder: %v", err)
	}
	if err := os.WriteFile(path, []byte("not json\n"), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	answer, _ := announce(t, current.state, protocol.Register{Label: "sleep", Command: []string{"sleep"}, Checkout: "/c", Worktree: "/c"})

	want := refusal("heavy: " + path + ":1: invalid character 'o' in literal null (expecting 'u')")
	if !reflect.DeepEqual(answer, want) {
		t.Fatalf("answer = %#v, want %#v", answer, want)
	}
}

func TestDaemonThatEndsWithRunsLetsTheNextOneWaitForThem(t *testing.T) {
	first := startDaemon(t, protocol.Identity{})
	if answer, _ := register(t, first.state); answer.Type != protocol.TypeStart {
		t.Fatalf("registration answered %#v, want a start", answer)
	}
	first.cancel()
	if err := first.ended(t); err != nil {
		t.Fatalf("the first daemon ended with %v", err)
	}
	clock := newFakeClock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- daemon.Run(ctx, options(first.state, protocol.Identity{}, clock)) }()
	defer func() { cancel(); <-done }()
	waitFor(t, func() bool { return exists(filepath.Join(first.state, "daemon.sock")) }, "the second daemon to listen")

	answer, reader := announce(t, first.state, protocol.Register{Label: "make", Command: []string{"make"}, Checkout: "/other", Worktree: "/other"})

	if answer.Type != protocol.TypeWaiting {
		t.Fatalf("a new run right after the restart got %#v, want it to wait for the runs of the ended daemon", answer)
	}
	clock.advance(t, 6*time.Second)
	if next, err := protocol.ReadMessage(reader); err != nil || next.Type != protocol.TypeStart {
		t.Fatalf("after the window the run got %#v, %v; want a start", next, err)
	}
}

// status asks the daemon for the queue on a fresh connection.
func status(t *testing.T, state string) []protocol.RunStatus {
	t.Helper()
	_, reader, connection := greet(t, state, `{"protocol":1,"version":"","stamp":""}`)
	if err := protocol.Write(connection, protocol.Message{Type: protocol.TypeStatus, Status: &protocol.Status{}}); err != nil {
		t.Fatalf("asking for the status: %v", err)
	}
	answer, err := protocol.ReadMessage(reader)
	if err != nil || answer.Type != protocol.TypeStatus {
		t.Fatalf("status answer %#v, %v", answer, err)
	}
	return answer.Status.Runs
}

// bin/test_heavy.py:332 and :347: a finished run stays 30 s with its result, then leaves the status
func TestFinishedRunStaysThirtySecondsInTheStatus(t *testing.T) {
	current := startDaemon(t, protocol.Identity{})
	started, connection := register(t, current.state)
	if err := protocol.Write(connection, protocol.Message{Type: protocol.TypeEnded, Ended: &protocol.Ended{ExitStatus: 3}}); err != nil {
		t.Fatalf("ending the run: %v", err)
	}
	waitFor(t, func() bool {
		return len(status(t, current.state)) == 1 && status(t, current.state)[0].State == "finished"
	}, "the run to finish")

	runs := status(t, current.state)
	ended := current.clock.Now().Unix()
	want := protocol.RunStatus{State: "finished", PID: os.Getpid(), Since: ended, Directory: current.state, Label: "sleep", Command: "sleep 1",
		Ended: ended, ExitStatus: 3, Expires: ended + 30, ID: started.Start.ID, Checkout: current.state, Budget: 18}
	runs[0].Order = 0
	if !reflect.DeepEqual(runs, []protocol.RunStatus{want}) {
		t.Fatalf("status = %#v, want %#v", runs, want)
	}
	current.clock.advance(t, 29*time.Second)
	if got := len(status(t, current.state)); got != 1 {
		t.Fatalf("after 29 s the status holds %d runs, want the finished one", got)
	}
	current.clock.advance(t, time.Second)
	if got := status(t, current.state); len(got) != 0 {
		t.Fatalf("after 30 s the status holds %#v, want nothing", got)
	}
}
