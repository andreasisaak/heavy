package daemon

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// handle serves one connection: the hello, then its messages until the client closes it.
func (s *server) handle(connection net.Conn) {
	defer func() {
		connection.Close()
		s.mutex.Lock()
		delete(s.connections, connection)
		s.mutex.Unlock()
		s.handlers.Done()
	}()
	reader := bufio.NewReader(connection)
	if err := connection.SetReadDeadline(time.Now().Add(s.helloTimeout)); err != nil {
		s.logger.Warn("setting the hello deadline failed", "error", err.Error())
		return
	}
	hello, err := protocol.ReadHello(reader)
	if errors.Is(err, io.EOF) {
		return
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		s.logger.Info("closed a connection that sent no hello", "within", s.helloTimeout.String())
		return
	}
	if err == nil {
		err = connection.SetReadDeadline(time.Time{})
	}
	answer := protocol.Hello{Protocol: protocol.Version, Version: s.identity.Version, Stamp: s.identity.Stamp}
	if err != nil {
		s.logger.Warn("refused a connection without a valid hello", "error", err.Error())
		answer.Error = err.Error()
		send(s, connection, answer)
		return
	}
	if hello.Protocol != protocol.Version {
		answer.Error = fmt.Sprintf("heavy: the running daemon speaks protocol %d, this heavy %d; restart the session", protocol.Version, hello.Protocol)
		s.logger.Info("refused a heavy of another protocol", "protocol", hello.Protocol)
		send(s, connection, answer)
		return
	}
	client := protocol.Identity{Version: hello.Version, Stamp: hello.Stamp}
	answer.Handover = !hello.Reader && client.Newer(s.identity, s.installedStamp()) && !s.busy()
	if !send(s, connection, answer) {
		return
	}
	if answer.Handover {
		select {
		case s.handover <- client:
		default:
		}
		return
	}
	s.messages(connection, reader)
}

// messages serves what the first message asks for: a run, a monitor, the status, or a change of a run.
func (s *server) messages(connection net.Conn, reader *bufio.Reader) {
	message, err := protocol.ReadMessage(reader)
	if errors.Is(err, io.EOF) {
		return
	}
	switch {
	case err != nil:
	case message.Type == protocol.TypeRegister:
		s.serveRun(connection, reader, *message.Register)
		return
	case message.Type == protocol.TypeStatus:
		s.answerStatus(connection, *message.Status)
		return
	case message.Type == protocol.TypeSubscribe:
		s.serveSubscriber(connection, reader, *message.Subscribe)
		return
	case message.Type == protocol.TypeForward:
		s.answerForward(connection, message.Forward.Position)
		return
	case message.Type == protocol.TypePause:
		s.answerControl(connection, message.Type, message.Pause.ID)
		return
	case message.Type == protocol.TypeResume:
		s.answerControl(connection, message.Type, message.Resume.ID)
		return
	case message.Type == protocol.TypeTerminate:
		s.answerControl(connection, message.Type, message.Terminate.ID)
		return
	default:
		err = fmt.Errorf("heavy: the daemon takes no %s message from a client", message.Type)
	}
	s.logger.Warn("refused a message", "error", err.Error())
	send(s, connection, protocol.Message{Type: protocol.TypeError, Error: &protocol.Failure{Text: err.Error()}})
}

// send writes a hello or a message; a client that went away is logged and gets no more.
func send[T protocol.Hello | protocol.Message](s *server, connection net.Conn, value T) bool {
	if err := protocol.Write(connection, value); err != nil {
		s.logger.Warn("writing to a client failed", "error", err.Error())
		return false
	}
	return true
}
