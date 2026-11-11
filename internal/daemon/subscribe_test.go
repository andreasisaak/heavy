package daemon_test

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/platform"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// subscription is the open connection of a monitor.
type subscription struct {
	reader     *bufio.Reader
	connection net.Conn
}

// subscribe sends the subscription and returns the daemon's first answer with the open connection.
func subscribe(t *testing.T, state string, request protocol.Subscribe) (protocol.Message, *subscription) {
	t.Helper()
	_, reader, connection := greet(t, state, `{"protocol":1,"version":"","stamp":""}`)
	if err := protocol.Write(connection, protocol.Message{Type: protocol.TypeSubscribe, Subscribe: &request}); err != nil {
		t.Fatalf("subscribing: %v", err)
	}
	if err := connection.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	answer, err := protocol.ReadMessage(reader)
	if err != nil {
		t.Fatalf("reading the answer to the subscription: %v", err)
	}
	return answer, &subscription{reader: reader, connection: connection}
}

// afterStarts reads the next message that is not the start of a run: the tests of pauses and restarts start
// their runs first.
func (s *subscription) afterStarts(t *testing.T) protocol.Message {
	t.Helper()
	for {
		message := s.next(t)
		if message.Event == nil || !strings.Contains(message.Event.Line, ") started at ") {
			return message
		}
	}
}

// next reads the next message of a subscription within 5 s.
func (s *subscription) next(t *testing.T) protocol.Message {
	t.Helper()
	if err := s.connection.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	message, err := protocol.ReadMessage(s.reader)
	if err != nil {
		t.Fatalf("reading an event: %v", err)
	}
	return message
}

// monitors asks for the status and returns the sessions with a monitor.
func monitors(t *testing.T, state string) []string {
	t.Helper()
	_, reader, connection := greet(t, state, `{"protocol":1,"version":"","stamp":""}`)
	if err := protocol.Write(connection, protocol.Message{Type: protocol.TypeStatus, Status: &protocol.Status{}}); err != nil {
		t.Fatal(err)
	}
	answer, err := protocol.ReadMessage(reader)
	if err != nil || answer.Status == nil {
		t.Fatalf("status answer %#v, %v", answer, err)
	}
	return answer.Status.Monitors
}

// FR-29 and G9: a subscription is answered once it holds; the status names the sessions with a monitor, through
// the registration of a Claude Code PID or directly, and forgets a monitor that went away
func TestSubscribedMonitorsAreNamedInTheStatusUntilTheyLeave(t *testing.T) {
	current := startDaemon(t, protocol.Identity{})
	write(t, filepath.Join(current.state, "sessions", "4242.json"), `{"session_id":"s-pid","cwd":"/w","checkout":"/w","time":1}`)

	byPID, first := subscribe(t, current.state, protocol.Subscribe{ClaudePID: 4242})
	direct, _ := subscribe(t, current.state, protocol.Subscribe{Session: "s-direct"})

	if want := (protocol.Message{Type: protocol.TypeSubscribe, Subscribe: &protocol.Subscribe{ClaudePID: 4242}}); !reflect.DeepEqual(byPID, want) {
		t.Fatalf("answer = %#v, want %#v", byPID, want)
	}
	if want := (protocol.Message{Type: protocol.TypeSubscribe, Subscribe: &protocol.Subscribe{Session: "s-direct"}}); !reflect.DeepEqual(direct, want) {
		t.Fatalf("answer = %#v, want %#v", direct, want)
	}
	if got := monitors(t, current.state); !reflect.DeepEqual(got, []string{"s-direct", "s-pid"}) {
		t.Fatalf("monitors = %q, want s-direct and s-pid", got)
	}
	first.connection.Close()
	waitFor(t, func() bool { return reflect.DeepEqual(monitors(t, current.state), []string{"s-direct"}) }, "the status without the closed monitor")
}

// a PID whose registration cannot be read names no session; the daemon logs why
func TestMonitorOfAnUnregisteredPIDNamesNoSession(t *testing.T) {
	current := startDaemon(t, protocol.Identity{})

	subscribe(t, current.state, protocol.Subscribe{ClaudePID: 4343})

	if got := monitors(t, current.state); !reflect.DeepEqual(got, []string{}) {
		t.Fatalf("monitors = %q, want none", got)
	}
}

func TestSubscriptionNeedsEitherAPIDOrASession(t *testing.T) {
	current := startDaemon(t, protocol.Identity{})
	for _, request := range []protocol.Subscribe{{}, {ClaudePID: 1, Session: "s"}} {
		answer, _ := subscribe(t, current.state, request)

		want := refusal("heavy: a subscription names either the PID of a Claude Code process or a session, got " + show(request))
		if !reflect.DeepEqual(answer, want) {
			t.Fatalf("answer to %+v = %#v, want %#v", request, answer, want)
		}
	}
}

// G3: the daemon ends 10 minutes after the last run and the last subscriber
func TestSubscriberKeepsTheDaemonFromEndingIdle(t *testing.T) {
	current := startDaemon(t, protocol.Identity{})
	_, monitor := subscribe(t, current.state, protocol.Subscribe{Session: "s-1"})

	current.clock.advance(t, 11*time.Minute)
	monitor.connection.Close()
	waitFor(t, func() bool { return len(monitors(t, current.state)) == 0 }, "the monitor to leave")
	current.clock.advance(t, 9*time.Minute+59*time.Second)
	current.clock.advanceOnce(t, time.Second)

	if err := current.ended(t); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
}

// FR-27: a monitor hears the pause and the resume of a run of its session, with the numbers
func TestSubscriberHearsThePauseAndResumeOfARunForMemory(t *testing.T) {
	scene := startLoadDaemon(t, plenty)
	first, second := repository(t), repository(t)
	scene.remember(t, "sleep", first, second)
	_, monitor := subscribe(t, scene.state, protocol.Subscribe{Session: "s-young"})
	older := scene.start(t, first, "sleep", "300.41")
	scene.step(t, 3, "start of the older run", func() bool { return older.has(startedLine + `sleep 300.41\n`) })
	t.Setenv("HEAVY_SESSION", "s-young")
	younger := scene.start(t, second, "sleep", "300.42")
	scene.step(t, 3, "start of the younger run", func() bool { return younger.has(startedLine + `sleep 300.42\n`) })
	id := status(t, scene.state)[1].ID

	scene.setAvailable(t, pressure)
	scene.step(t, 8, "pause line", func() bool { return younger.has(`heavy: paused at `) })
	paused := monitor.afterStarts(t)
	scene.setAvailable(t, recovery)
	scene.step(t, 20, "resume line", func() bool { return younger.has(`heavy: resumed at `) })
	resumed := monitor.afterStarts(t)

	expectEvent(t, paused, "s-young", `heavy: your run sleep \(`+id+`\) was paused at \d{2}:\d{2}:\d{2}: `+platform.Docker+` 1\.0 GiB free, host 50 % free`)
	expectEvent(t, resumed, "s-young", `heavy: your run sleep \(`+id+`\) was resumed at \d{2}:\d{2}:\d{2}: `+platform.Docker+` 3\.0 GiB free, host 50 % free`)
}

// FR-27: the restart of a run under lasting pressure, and a pause and resume by the user, reach the monitor too
func TestSubscriberHearsARestartAndAPauseByTheUser(t *testing.T) {
	scene := startLoadDaemon(t, plenty)
	first, second := repository(t), repository(t)
	scene.allow(t, second, `{"command": "sleep", "restart": true}`)
	scene.remember(t, "sleep", first, second)
	_, monitor := subscribe(t, scene.state, protocol.Subscribe{Session: "s-r"})
	older := scene.start(t, first, "sleep", "300.43")
	scene.step(t, 3, "start of the older run", func() bool { return older.has(startedLine + `sleep 300.43\n`) })
	t.Setenv("HEAVY_SESSION", "s-r")
	younger := scene.start(t, second, "sleep", "300.44")
	scene.step(t, 3, "start of the younger run", func() bool { return younger.has(startedLine + `sleep 300.44\n`) })
	id := status(t, scene.state)[1].ID

	scene.setAvailable(t, pressure)
	scene.step(t, 8, "pause line", func() bool { return younger.has(`heavy: paused at `) })
	monitor.afterStarts(t)
	scene.step(t, 35, "restart line", func() bool { return younger.has(`heavy: restarting at `) })
	restarted := monitor.afterStarts(t)
	scene.setAvailable(t, recovery)
	scene.step(t, 25, "second start", func() bool { return regexp.MustCompile(`(?s)started at.*started at`).MatchString(younger.output()) })
	control(t, scene.state, protocol.Message{Type: protocol.TypePause, Pause: &protocol.Target{ID: id}})
	byUser := monitor.afterStarts(t)
	control(t, scene.state, protocol.Message{Type: protocol.TypeResume, Resume: &protocol.Target{ID: id}})
	resumedByUser := monitor.afterStarts(t)

	expectEvent(t, restarted, "s-r", `heavy: your run sleep \(`+id+`\) was ended at \d{2}:\d{2}:\d{2} to start again as the next run: `+platform.Docker+` 1\.0 GiB free, host 50 % free`)
	expectEvent(t, byUser, "s-r", `heavy: your run sleep \(`+id+`\) was paused by heavy --pause at \d{2}:\d{2}:\d{2}`)
	expectEvent(t, resumedByUser, "s-r", `heavy: your run sleep \(`+id+`\) was resumed by heavy --resume at \d{2}:\d{2}:\d{2}`)
}

// control sends a pause or resume and checks that the daemon carried it out. A client prints its start line
// before its command runs, so a run a test just saw start may not have told the daemon its process group yet; the
// daemon then says to try again in a moment, and control does.
func control(t *testing.T, state string, request protocol.Message) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, reader, connection := greet(t, state, `{"protocol":1,"version":"","stamp":""}`)
		if err := protocol.Write(connection, request); err != nil {
			t.Fatal(err)
		}
		answer, err := protocol.ReadMessage(reader)
		if err == nil && answer.Type == protocol.TypePrint {
			return
		}
		starting := err == nil && answer.Error != nil && strings.HasSuffix(answer.Error.Text, "is starting its command; try again in a moment")
		if !starting || time.Now().After(deadline) {
			t.Fatalf("answer to %s = %#v (%+v), %v", request.Type, answer, answer.Error, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func expectEvent(t *testing.T, got protocol.Message, session, line string) {
	t.Helper()
	if got.Type != protocol.TypeEvent || got.Event == nil || got.Event.Session != session || got.Event.Checkout != "" ||
		!regexp.MustCompile(`^`+line+`$`).MatchString(got.Event.Line) {
		t.Fatalf("event = %#v %+v, want session %s and a line matching %s", got, got.Event, session, line)
	}
}

// show is the subscription as compact JSON, as the refusal names it.
func show(request protocol.Subscribe) string {
	data, err := json.Marshal(request)
	if err != nil {
		panic(err)
	}
	return string(data)
}
