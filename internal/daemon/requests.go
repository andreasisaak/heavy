package daemon

import (
	"fmt"
	"net"
	"path/filepath"

	"github.com/andreasisaak/heavy/internal/history"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// answerStatus sends the queue; with Detail also the machine for the dashboard, and the daemon then looks at the
// containers outside the runs every 10 s while the dashboard keeps asking (design G13).
func (s *server) answerStatus(connection net.Conn, request protocol.Status) {
	var recent []history.Record
	if request.Detail {
		var err error
		if recent, err = history.Recent(filepath.Join(s.stateDirectory, "history"), recentResults); err != nil {
			s.logger.Warn("reading the recent results failed", "error", err.Error())
		}
	}
	s.state.Lock()
	now := s.clock.Now()
	status := protocol.Status{Runs: s.queue.snapshot(now), VM: s.machine.VMStatus(), Monitors: s.monitoredSessions()}
	if request.Detail {
		s.detailAt = now
		status.Machine = s.machineStatus(now, recent)
	}
	s.state.Unlock()
	send(s, connection, protocol.Message{Type: protocol.TypeStatus, Status: &status})
}

// answerForward moves the run waiting at the position one place ahead, between the two runs before it, as 0.6 did.
func (s *server) answerForward(connection net.Conn, position int) {
	line, err := s.forward(position)
	if err != nil {
		send(s, connection, protocol.Message{Type: protocol.TypeError, Error: &protocol.Failure{Text: err.Error()}})
		return
	}
	send(s, connection, protocol.Message{Type: protocol.TypePrint, Print: &protocol.Print{Line: line}})
}

func (s *server) forward(position int) (string, error) {
	s.state.Lock()
	defer s.state.Unlock()
	waiting := s.queue.waiting()
	if position == 1 && len(waiting) >= 1 {
		return "", fmt.Errorf("heavy: the run at #1 is next already, nothing waits ahead of it")
	}
	if position < 1 || position > len(waiting) {
		return "", fmt.Errorf("heavy: no run waits at #%d; heavy --status shows the queue", position)
	}
	moved := waiting[position-1]
	ahead := waiting[position-2].order
	if position == 2 {
		moved.order = ahead - 1
	} else {
		moved.order = (waiting[position-3].order + ahead) / 2
	}
	update := protocol.Waiting{ID: moved.id, Order: moved.order, Position: position - 1, Since: moved.arrived.Unix()}
	if err := moved.link.send(protocol.Message{Type: protocol.TypeWaiting, Waiting: &update}); err != nil {
		s.logger.Warn("telling a moved run its place failed", "id", moved.id, "error", err.Error())
	}
	s.logger.Info("run moved ahead", "id", moved.id, "from", position, "to", position-1)
	s.decideLocked()
	return fmt.Sprintf("heavy: moved %s from #%d to #%d", moved.register.Label, position, position-1), nil
}
