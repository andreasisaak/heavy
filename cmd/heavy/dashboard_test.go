package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/testkit"
)

// fakeDashboardDaemon listens on the socket of the state directory like a daemon: it greets each connection with
// hello, hands the one message it reads to the channel and answers it with reply.
func fakeDashboardDaemon(t *testing.T, state string, hello protocol.Hello, reply protocol.Message) <-chan protocol.Message {
	t.Helper()
	listener, err := net.Listen("unix", filepath.Join(state, "daemon.sock"))
	if err != nil {
		t.Fatalf("listening on the socket of %s: %v", state, err)
	}
	t.Cleanup(func() { listener.Close() })
	received := make(chan protocol.Message, 4)
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			reader := bufio.NewReader(connection)
			if _, err := protocol.ReadHello(reader); err == nil && protocol.Write(connection, hello) == nil && hello.Error == "" {
				if message, err := protocol.ReadMessage(reader); err == nil {
					received <- message
					_ = protocol.Write(connection, reply)
				}
			}
			connection.Close()
		}
	}()
	return received
}

// silentDashboardDaemon greets each connection, reads its message and then answers nothing until the test ends.
func silentDashboardDaemon(t *testing.T, state string) {
	t.Helper()
	listener, err := net.Listen("unix", filepath.Join(state, "daemon.sock"))
	if err != nil {
		t.Fatalf("listening on the socket of %s: %v", state, err)
	}
	ended := make(chan struct{})
	t.Cleanup(func() {
		close(ended)
		listener.Close()
	})
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				reader := bufio.NewReader(connection)
				if _, err := protocol.ReadHello(reader); err == nil && protocol.Write(connection, greeting) == nil {
					_, _ = protocol.ReadMessage(reader)
				}
				<-ended
			}()
		}
	}()
}

// withinTenSeconds runs the call and fails the test when it has not returned after 10 s; it returns how long the
// call took and its error.
func withinTenSeconds(t *testing.T, call func() error) (time.Duration, error) {
	t.Helper()
	began := time.Now()
	done := make(chan error, 1)
	go func() { done <- call() }()
	select {
	case err := <-done:
		return time.Since(began), err
	case <-time.After(10 * time.Second):
		t.Fatal("the call did not return within 10 s")
		return 0, nil
	}
}

// oneReceived is the message the fake daemon got, or a failure after 5 s.
func oneReceived(t *testing.T, received <-chan protocol.Message) protocol.Message {
	t.Helper()
	select {
	case message := <-received:
		return message
	case <-time.After(5 * time.Second):
		t.Fatal("the fake daemon got no message within 5 s")
		return protocol.Message{}
	}
}

func asJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encoding %#v: %v", value, err)
	}
	return string(encoded)
}

var greeting = protocol.Hello{Protocol: protocol.Version}

func TestDashboardWithAnArgumentIsAUsageError(t *testing.T) {
	got := runHeavy(t, "--dashboard", "--json")

	want := outcome{exitStatus: 64, stderr: "heavy: --dashboard takes no arguments, got --json. " + usage + "\n"}
	if got != want {
		t.Fatalf("heavy --dashboard --json = %+v, want %+v", got, want)
	}
}

// plan 6.3: the dashboard only watches: without a daemon it says so and starts none
func TestDashboardFetchWithoutADaemonStartsNone(t *testing.T) {
	state := testkit.StateDir(t)

	_, err := dashboardFetch(daemon.ConnectOptions{StateDirectory: state, Binary: testkit.BuildBinary(t)})()
	_, lockErr := os.Stat(filepath.Join(state, "daemon.lock"))

	want := fmt.Sprintf("heavy: no daemon answers in %s; the next heavy run starts one", state)
	if err == nil || err.Error() != want || !errors.Is(lockErr, os.ErrNotExist) {
		t.Fatalf("fetch = %v with daemon.lock %v, want %q and no daemon.lock", err, lockErr, want)
	}
}

// plan 6.3: the dashboard asks for the queue with Detail and gets the machine back
func TestDashboardFetchAsksTheDaemonForTheDetail(t *testing.T) {
	state := testkit.StateDir(t)
	answer := protocol.Status{
		Runs: []protocol.RunStatus{{State: "running", ID: "run-ci", Label: "ci"}}, Monitors: []string{},
		Machine: &protocol.Machine{Cores: 18, FreeSeries: []int64{1, 2}, Foreign: []protocol.ForeignContainer{}, Recent: []protocol.Result{}},
	}
	received := fakeDashboardDaemon(t, state, greeting, protocol.Message{Type: protocol.TypeStatus, Status: &answer})

	got, err := dashboardFetch(daemon.ConnectOptions{StateDirectory: state})()
	request := oneReceived(t, received)

	wantRequest := protocol.Message{Type: protocol.TypeStatus, Status: &protocol.Status{Detail: true}}
	if err != nil || !reflect.DeepEqual(got, answer) || !reflect.DeepEqual(request, wantRequest) {
		t.Fatalf("fetch = %s, %v after the request %s\nwant %s after %s", asJSON(t, got), err, asJSON(t, request), asJSON(t, answer), asJSON(t, wantRequest))
	}
}

func TestDashboardFetchReturnsTheRefusalOfTheDaemon(t *testing.T) {
	state := testkit.StateDir(t)
	fakeDashboardDaemon(t, state, protocol.Hello{Protocol: protocol.Version, Error: "heavy: example refusal"}, protocol.Message{})

	_, err := dashboardFetch(daemon.ConnectOptions{StateDirectory: state})()

	if err == nil || err.Error() != "heavy: example refusal" {
		t.Fatalf("fetch = %v, want heavy: example refusal", err)
	}
}

// a daemon that greets and then hangs must not freeze the dashboard: the fetch ends after 5 s
func TestDashboardFetchGivesUpOnASilentDaemonAfterFiveSeconds(t *testing.T) {
	state := testkit.StateDir(t)
	silentDashboardDaemon(t, state)

	took, err := withinTenSeconds(t, func() error {
		_, err := dashboardFetch(daemon.ConnectOptions{StateDirectory: state})()
		return err
	})

	want := "heavy: the daemon did not answer the dashboard within 5s"
	if err == nil || err.Error() != want || took < 5*time.Second || took > 7*time.Second {
		t.Fatalf("fetch = %v after %s, want %q after 5 s", err, took, want)
	}
}

func TestDashboardSendGivesUpOnASilentDaemonAfterFiveSeconds(t *testing.T) {
	state := testkit.StateDir(t)
	silentDashboardDaemon(t, state)

	took, err := withinTenSeconds(t, func() error {
		_, err := dashboardSend(daemon.ConnectOptions{StateDirectory: state})(protocol.Message{Type: protocol.TypePause, Pause: &protocol.Target{ID: "run-ci"}})
		return err
	})

	want := "heavy: the daemon did not answer the pause within 5s"
	if err == nil || err.Error() != want || took < 5*time.Second || took > 7*time.Second {
		t.Fatalf("send = %v after %s, want %q after 5 s", err, took, want)
	}
}

// plan 6.3: the keys reach the daemon the way --forward, --pause, --resume and --stop do
func TestDashboardSendPassesEachActionToTheDaemon(t *testing.T) {
	for _, message := range []protocol.Message{
		{Type: protocol.TypeForward, Forward: &protocol.Forward{Position: 2}},
		{Type: protocol.TypePause, Pause: &protocol.Target{ID: "run-ci"}},
		{Type: protocol.TypeResume, Resume: &protocol.Target{ID: "run-ci"}},
		{Type: protocol.TypeTerminate, Terminate: &protocol.Target{ID: "run-ci"}},
	} {
		t.Run(message.Type, func(t *testing.T) {
			state := testkit.StateDir(t)
			received := fakeDashboardDaemon(t, state, greeting, protocol.Message{Type: protocol.TypePrint, Print: &protocol.Print{Line: "heavy: example answer"}})

			line, err := dashboardSend(daemon.ConnectOptions{StateDirectory: state})(message)
			request := oneReceived(t, received)

			if err != nil || line != "heavy: example answer" || !reflect.DeepEqual(request, message) {
				t.Fatalf("send = %q, %v after the request %s, want heavy: example answer after %s", line, err, asJSON(t, request), asJSON(t, message))
			}
		})
	}
}

func TestDashboardSendReturnsTheErrorOfTheDaemon(t *testing.T) {
	state := testkit.StateDir(t)
	fakeDashboardDaemon(t, state, greeting, protocol.Message{Type: protocol.TypeError, Error: &protocol.Failure{Text: "heavy: no run waits at #2; heavy --status shows the queue"}})

	_, err := dashboardSend(daemon.ConnectOptions{StateDirectory: state})(protocol.Message{Type: protocol.TypeForward, Forward: &protocol.Forward{Position: 2}})

	if want := "heavy: no run waits at #2; heavy --status shows the queue"; err == nil || err.Error() != want {
		t.Fatalf("send = %v, want %s", err, want)
	}
}

func TestDashboardSendRefusesOtherMessages(t *testing.T) {
	_, err := dashboardSend(daemon.ConnectOptions{StateDirectory: testkit.StateDir(t)})(protocol.Message{Type: protocol.TypeStatus, Status: &protocol.Status{}})

	if want := "heavy: the dashboard sends no status message"; err == nil || err.Error() != want {
		t.Fatalf("send = %v, want %s", err, want)
	}
}
