package daemon

import (
	"fmt"
	"net"

	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/scheduler"
)

// answerControl pauses, resumes or stops a run for `heavy --pause|--resume|--stop <id>` (FR-19): the state changes
// at once, then the processes of the run follow; a failure of that work is the answer.
func (s *server) answerControl(connection net.Conn, kind, id string) {
	current, line, result, err := s.control(kind, id)
	if err == nil {
		err = reportable(<-result)
	}
	if err != nil {
		if current != nil {
			s.logger.Warn("acting on a run for the user failed", "id", id, "action", kind, "error", err.Error())
		}
		send(s, connection, protocol.Message{Type: protocol.TypeError, Error: &protocol.Failure{Text: err.Error()}})
		return
	}
	send(s, connection, protocol.Message{Type: protocol.TypePrint, Print: &protocol.Print{Line: line}})
}

// control changes the state of the run and queues the work on its processes before anything it decides next, so
// the jobs keep the order of the state changes; the result channel gives the error of that work.
func (s *server) control(kind, id string) (*run, string, <-chan error, error) {
	s.state.Lock()
	defer s.state.Unlock()
	current := s.queue.find(id)
	if current == nil || current.status == scheduler.Waiting {
		return nil, "", nil, fmt.Errorf("heavy: no active run has id %s; heavy --status shows the runs", id)
	}
	if current.processGroup == 0 {
		return nil, "", nil, fmt.Errorf("heavy: run %s is starting its command; try again in a moment", id)
	}
	now := s.clock.Now()
	at := now.Format("15:04:05")
	tracker, target := s.tracker(), processRun(current)
	switch kind {
	case protocol.TypePause:
		if current.status == scheduler.Paused {
			return nil, "", nil, fmt.Errorf("heavy: run %s is paused already", id)
		}
		current.status, current.pausedReason, current.pausedAt = scheduler.Paused, scheduler.PausedByUser, now
		s.tell(current, "heavy: paused by the user at "+at)
		s.announce(current, "was paused by heavy --pause at "+at)
		s.logger.Info("run paused by the user", "id", id)
		return current, fmt.Sprintf("heavy: paused run %s (%s)", id, current.register.Label), s.queueJob(func() error { return tracker.Pause(target) }), nil
	case protocol.TypeResume:
		if current.status != scheduler.Paused {
			return nil, "", nil, fmt.Errorf("heavy: run %s is not paused", id)
		}
		current.pausedTotal += now.Sub(current.pausedAt)
		current.status, current.pausedReason, current.pausedAt = scheduler.Running, scheduler.NotPaused, now
		s.tell(current, "heavy: resumed at "+at)
		s.announce(current, "was resumed by heavy --resume at "+at)
		s.logger.Info("run resumed by the user", "id", id)
		result := s.queueJob(func() error { return tracker.Resume(target) })
		s.decideLocked()
		return current, fmt.Sprintf("heavy: resumed run %s (%s)", id, current.register.Label), result, nil
	}
	current.stopping = true
	s.tell(current, "heavy: stopped by the user at "+at)
	s.logger.Info("run stopped by the user", "id", id)
	return current, fmt.Sprintf("heavy: stopped run %s (%s)", id, current.register.Label), s.queueJob(func() error {
		_, err := tracker.Terminate(target)
		return err
	}), nil
}

// tell prints a line into the output of a run.
func (s *server) tell(current *run, line string) {
	if err := current.link.send(protocol.Message{Type: protocol.TypePrint, Print: &protocol.Print{Line: line}}); err != nil {
		s.logger.Warn("telling a run failed", "id", current.id, "line", line, "error", err.Error())
	}
}
