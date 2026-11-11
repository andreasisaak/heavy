package daemon

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/andreasisaak/heavy/internal/history"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/scheduler"
)

// serveRun keeps the connection of a run's client: it queues the run, then reads the client's answers until the
// run ends or the client goes away.
func (s *server) serveRun(connection net.Conn, reader *bufio.Reader, register protocol.Register) {
	current, err := s.enqueue(connection, register)
	if err != nil {
		s.logger.Warn("refused a run", "label", register.Label, "error", err.Error())
		send(s, connection, protocol.Message{Type: protocol.TypeError, Error: &protocol.Failure{Text: err.Error()}})
		return
	}
	for {
		message, err := protocol.ReadMessage(reader)
		if err != nil {
			s.dropped(current, err)
			return
		}
		switch {
		case message.Type == protocol.TypeStart:
			s.started(current, message.Start.ProcessGroup)
		case message.Type == protocol.TypeRed:
			s.redStop(current, message.Red.Line)
		case message.Type == protocol.TypeEnded:
			if !s.ended(current, *message.Ended) {
				return
			}
		default:
			s.dropped(current, fmt.Errorf("heavy: the daemon takes no %s message from a run", message.Type))
			return
		}
	}
}

// enqueue adds a new run, or takes over a run a client announces again after a daemon ended (FR-17).
func (s *server) enqueue(connection net.Conn, register protocol.Register) (*run, error) {
	now := s.clock.Now()
	current := &run{register: register, status: scheduler.Waiting, arrived: now, order: unixSeconds(now), link: &link{connection: connection}}
	if adopt := register.Adopt; adopt != nil {
		if err := adoptable(*adopt); err != nil {
			return nil, err
		}
		since := time.Unix(adopt.Since, 0)
		current.id, current.arrived, current.order = adopt.ID, since, adopt.Order
		if adopt.State == "running" {
			current.status, current.started, current.cores, current.processGroup = scheduler.Running, since, adopt.Cores, adopt.ProcessGroup
		}
	} else {
		id, err := newID()
		if err != nil {
			return nil, err
		}
		current.id = id
	}
	if current.status == scheduler.Waiting {
		predicted, known, err := history.Predict(filepath.Join(s.stateDirectory, "history"), strings.Join(register.Command, " "), register.Label, register.Checkout)
		if err != nil {
			return nil, err
		}
		current.predicted, current.predictedBytes = known, predicted
	}
	s.state.Lock()
	defer s.state.Unlock()
	if s.queue.find(current.id) != nil {
		return nil, fmt.Errorf("heavy: run %s is in the queue already; heavy --status shows the runs", current.id)
	}
	s.queue.runs = append(s.queue.runs, current)
	if current.status == scheduler.Running {
		s.adoptSlot(current)
	}
	s.logger.Info("run queued", "id", current.id, "label", register.Label, "checkout", register.Checkout, "adopted", register.Adopt != nil)
	s.decideLocked()
	if current.status == scheduler.Waiting {
		waiting := protocol.Waiting{ID: current.id, Order: current.order, Position: s.queue.position(current), Since: current.arrived.Unix()}
		if err := current.link.send(protocol.Message{Type: protocol.TypeWaiting, Waiting: &waiting}); err != nil {
			s.queue.remove(current)
			return nil, fmt.Errorf("heavy: telling run %s its place: %w", current.id, err)
		}
	}
	return current, nil
}

// adoptable checks what a client says of a run it announces again.
func adoptable(adopt protocol.Adoption) error {
	switch adopt.State {
	case "waiting":
		return nil
	case "running":
		if adopt.Cores < 1 || adopt.ProcessGroup <= 1 {
			return fmt.Errorf("heavy: expected a running run %s with at least one core and a process group above 1, got %d cores and group %d",
				adopt.ID, adopt.Cores, adopt.ProcessGroup)
		}
		return nil
	}
	return fmt.Errorf("heavy: expected the state \"waiting\" or \"running\" of the run %s to take over, got %q", adopt.ID, adopt.State)
}

func (s *server) started(current *run, processGroup int) {
	s.state.Lock()
	defer s.state.Unlock()
	current.processGroup = processGroup
	s.logger.Info("run runs", "id", current.id, "process_group", processGroup)
}

// ended records the end of a run's command and lets the next run start; it reports whether the run goes on, as a
// restarted run waits again.
func (s *server) ended(current *run, end protocol.Ended) bool {
	exitStatus := end.ExitStatus
	s.state.Lock()
	stopping, restarting, red := current.stopping, current.restarting, current.red || end.Red != ""
	s.state.Unlock()
	if restarting && !stopping && !red {
		s.requeue(current, exitStatus)
		return true
	}
	if restarting && stopping {
		failure := protocol.Failure{Text: "heavy: stopped by the user during its restart"}
		if err := current.link.send(protocol.Message{Type: protocol.TypeError, Error: &failure}); err != nil {
			s.logger.Warn("telling a run of its stop failed", "id", current.id, "error", err.Error())
		}
	}
	reason := "passed"
	switch {
	case stopping:
		reason = "stopped"
	case red:
		reason = "red"
	case exitStatus != 0:
		reason = "failed"
	}
	s.finish(current, exitStatus, reason)
	return false
}

// dropped ends a run whose client went away: its process group, its VM processes and its co-started containers get
// SIGTERM, then SIGKILL (FR-16).
func (s *server) dropped(current *run, cause error) {
	s.state.Lock()
	closing, active, group := s.closing, current.status != scheduler.Waiting, current.processGroup
	s.state.Unlock()
	if closing {
		return
	}
	if !errors.Is(cause, io.EOF) {
		s.logger.Warn("lost the client of a run", "id", current.id, "error", cause.Error())
	}
	if active && group > 1 {
		s.state.Lock()
		tracker, target := s.tracker(), processRun(current)
		s.state.Unlock()
		s.jobs <- func() {
			if _, err := tracker.Terminate(target); reportable(err) != nil {
				s.logger.Warn("ending the processes of a lost run failed", "id", current.id, "error", err.Error())
			}
		}
	}
	s.finish(current, -1, "stopped")
}

func (s *server) finish(current *run, exitStatus int, reason string) {
	s.state.Lock()
	defer s.state.Unlock()
	now := s.clock.Now()
	s.queue.remove(current)
	s.releaseSlot(current)
	if current.status != scheduler.Waiting {
		status := s.queue.statusOf(current)
		status.State, status.Position, status.Ended, status.ExitStatus, status.PausedReason = "finished", 0, now.Unix(), exitStatus, ""
		status.Expires = now.Add(finishedFor).Unix()
		s.queue.finished = append(s.queue.finished, finished{status: status, expires: now.Add(finishedFor)})
		s.announce(current, fmt.Sprintf("ended at %s with exit status %d (%s) after %s", now.Format("15:04:05"), exitStatus, reason,
			now.Sub(current.started).Round(time.Second)))
		record := historyRecord(current, now, exitStatus, reason)
		if err := history.Append(filepath.Join(s.stateDirectory, "history"), record); err != nil {
			s.logger.Error("writing the history record failed", "id", current.id, "error", err.Error())
		}
	}
	s.logger.Info("run ended", "id", current.id, "reason", reason, "exit_status", exitStatus)
	s.decideLocked()
}

func unixSeconds(at time.Time) float64 {
	return float64(at.UnixNano()) / float64(time.Second)
}

// pausedFor is how long a run has been paused in all, a pause still going on included.
func pausedFor(current *run, now time.Time) time.Duration {
	if current.status == scheduler.Paused {
		return current.pausedTotal + now.Sub(current.pausedAt)
	}
	return current.pausedTotal
}
