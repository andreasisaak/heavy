package daemon

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"

	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/sessions"
)

// subscriber is the connection of one session's monitor (design G9): it names the PID of its Claude Code process,
// whose registration says the current session, or the session itself.
type subscriber struct {
	link      *link
	claudePID int
	session   string
}

// serveSubscriber answers the subscription once it holds and keeps the monitor until its connection ends. Adding
// and answering happen under the state lock, so no event reaches the monitor before the answer.
func (s *server) serveSubscriber(connection net.Conn, reader *bufio.Reader, request protocol.Subscribe) {
	if (request.ClaudePID > 0) == (request.Session != "") {
		shown, _ := json.Marshal(request)
		text := "heavy: a subscription names either the PID of a Claude Code process or a session, got " + string(shown)
		s.logger.Warn("refused a subscription", "error", text)
		send(s, connection, protocol.Message{Type: protocol.TypeError, Error: &protocol.Failure{Text: text}})
		return
	}
	current := &subscriber{link: &link{connection: connection}, claudePID: request.ClaudePID, session: request.Session}
	s.state.Lock()
	s.subscribers[current] = true
	err := current.link.send(protocol.Message{Type: protocol.TypeSubscribe, Subscribe: &request})
	s.state.Unlock()
	if err == nil {
		s.logger.Info("monitor subscribed", "claude_pid", request.ClaudePID, "session", request.Session)
		err = drain(reader)
	}
	s.state.Lock()
	delete(s.subscribers, current)
	s.state.Unlock()
	s.logger.Info("monitor left", "claude_pid", request.ClaudePID, "session", request.Session, "reason", err.Error())
}

// drain reads until the monitor closes its connection; a monitor sends nothing after its subscription.
func drain(reader *bufio.Reader) error {
	message, err := protocol.ReadMessage(reader)
	switch {
	case errors.Is(err, io.EOF):
		return errors.New("connection closed")
	case err != nil:
		return err
	}
	return fmt.Errorf("unexpected %s message", message.Type)
}

// publish sends the event to every monitor; the monitors pick the events of their session. A monitor that cannot
// take it loses its connection and subscribes again. The caller holds state.
func (s *server) publish(event protocol.Event) {
	s.logger.Info("event", "session", event.Session, "checkout", event.Checkout, "line", event.Line)
	for current := range s.subscribers {
		if err := current.link.send(protocol.Message{Type: protocol.TypeEvent, Event: &event}); err != nil {
			s.logger.Warn("sending an event to a monitor failed", "claude_pid", current.claudePID, "session", current.session, "error", err.Error())
			current.link.connection.Close()
		}
	}
}

// monitoredSessions are the sessions with a connected monitor, sorted (FR-29). The caller holds state.
func (s *server) monitoredSessions() []string {
	names := []string{}
	seen := map[string]bool{}
	for current := range s.subscribers {
		session := current.session
		if current.claudePID > 0 {
			registered, err := sessions.Read(s.stateDirectory, current.claudePID)
			if err != nil {
				s.logger.Warn("reading the session of a monitor failed", "claude_pid", current.claudePID, "error", err.Error())
				continue
			}
			session = registered.SessionID
		}
		if !seen[session] {
			seen[session] = true
			names = append(names, session)
		}
	}
	sort.Strings(names)
	return names
}
