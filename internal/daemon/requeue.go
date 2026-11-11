package daemon

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/andreasisaak/heavy/internal/history"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/scheduler"
)

// historyRecord is the record of a command's end (design G11).
func historyRecord(current *run, now time.Time, exitStatus int, reason string) history.Record {
	return history.Record{
		ID: current.id, Label: current.register.Label, Command: strings.Join(current.register.Command, " "),
		Checkout: current.register.Checkout, Directory: current.register.Directory, Session: current.register.Session,
		Agent: current.register.Agent, Since: current.arrived.Unix(), Started: current.started.Unix(), Ended: now.Unix(),
		ExitStatus: exitStatus, Reason: reason, PeakRSSBytes: current.peak, Budget: current.cores, Restarts: current.restarts,
		PausedSeconds: int64(pausedFor(current, now).Seconds()),
	}
}

// requeue records the end of a restarted run's command and lets the run wait again ahead of the waiting runs; its
// client starts the command again once the run is admitted (design G5).
func (s *server) requeue(current *run, exitStatus int) {
	s.state.Lock()
	defer s.state.Unlock()
	now := s.clock.Now()
	directory := filepath.Join(s.stateDirectory, "history")
	if err := history.Append(directory, historyRecord(current, now, exitStatus, "restarted")); err != nil {
		s.logger.Error("writing the history record failed", "id", current.id, "error", err.Error())
	}
	current.restarts++
	s.releaseSlot(current)
	current.status, current.order, current.arrived = scheduler.Waiting, current.requeueOrder, now
	current.started, current.cores, current.processGroup = time.Time{}, 0, 0
	current.pausedReason, current.pausedAt, current.pausedTotal = scheduler.NotPaused, time.Time{}, 0
	current.restarting, current.restartedAt, current.rss, current.peak = false, now, 0, 0
	predicted, known, err := history.Predict(directory, strings.Join(current.register.Command, " "), current.register.Label, current.register.Checkout)
	if err != nil {
		s.logger.Error("predicting the memory of a restarted run failed", "id", current.id, "error", err.Error())
	}
	current.predicted, current.predictedBytes = known && err == nil, predicted
	waiting := protocol.Waiting{ID: current.id, Order: current.order, Position: s.queue.position(current), Since: now.Unix()}
	if err := current.link.send(protocol.Message{Type: protocol.TypeWaiting, Waiting: &waiting}); err != nil {
		s.logger.Warn("telling a restarted run its place failed", "id", current.id, "error", err.Error())
	}
	s.logger.Info("run waits again after its restart", "id", current.id, "label", current.register.Label, "restarts", current.restarts,
		"command", strings.Join(current.register.Command, " "))
	s.decideLocked()
}

// redStop ends the processes of a run whose client saw a red line (design G8, FR-26).
func (s *server) redStop(current *run, line string) {
	s.state.Lock()
	defer s.state.Unlock()
	if current.red || current.processGroup <= 1 {
		return
	}
	current.red = true
	s.logger.Info("run stopped red", "id", current.id, "line", line)
	s.announce(current, fmt.Sprintf("is red since %s and is being stopped: %s", s.clock.Now().Format("15:04:05"), line))
	tracker, target := s.tracker(), processRun(current)
	s.jobs <- func() {
		_, err := tracker.Terminate(target)
		s.report(current, "red stop", err)
	}
}
