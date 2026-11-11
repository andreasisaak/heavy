package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/andreasisaak/heavy/internal/procs"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/scheduler"
)

// finishedFor is how long a finished run stays in the status, for the herdr sidebar.
const finishedFor = 30 * time.Second

// writeTimeout bounds every write to a client, so a client that stops reading cannot stall the daemon.
const writeTimeout = 2 * time.Second

// run is a run of a connected client.
type run struct {
	id             string
	register       protocol.Register
	status         scheduler.Status
	arrived        time.Time
	started        time.Time
	order          float64
	cores          int
	processGroup   int
	predicted      bool
	predictedBytes int64
	pausedReason   scheduler.PauseReason
	pausedAt       time.Time
	pausedTotal    time.Duration
	restarts       int
	// stopping is set when the user stopped the run, so its end counts as stopped.
	stopping bool
	// restarting is set while the processes of a run with restart end under lasting pressure; the run then waits
	// again with requeueOrder.
	restarting   bool
	requeueOrder float64
	// restartedAt is when the command of a restarted run ended; the run starts again only after the SIGKILL phase of
	// that command, which finds its VM processes by the same tag.
	restartedAt time.Time
	// red is set when the client saw a red line; the run then ends as red (design G8).
	red bool
	// rss is the newest resident memory of the run, peak the highest since its start.
	rss  int64
	peak int64
	// slot is the machine slot the run holds while it is active, 0 for none; slotTold is set once the client heard
	// that the run waits for a slot, or why it started without one.
	slot     int
	slotTold bool
	link     *link
}

// link is the connection of a run's client; writes to it are serialized.
type link struct {
	mutex      sync.Mutex
	connection net.Conn
}

func (l *link) send(message protocol.Message) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if err := l.connection.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return err
	}
	return protocol.Write(l.connection, message)
}

// queue holds the runs of the daemon and the runs that finished in the last 30 s.
type queue struct {
	runs     []*run
	finished []finished
}

type finished struct {
	status  protocol.RunStatus
	expires time.Time
}

func newID() (string, error) {
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("heavy: making a run id: %w", err)
	}
	return hex.EncodeToString(random), nil
}

func (q *queue) find(id string) *run {
	for _, current := range q.runs {
		if current.id == id {
			return current
		}
	}
	return nil
}

func (q *queue) remove(target *run) {
	for index, current := range q.runs {
		if current == target {
			q.runs = append(q.runs[:index], q.runs[index+1:]...)
			return
		}
	}
}

// waiting returns the waiting runs in queue order: order, then arrival.
func (q *queue) waiting() []*run {
	var found []*run
	for _, current := range q.runs {
		if current.status == scheduler.Waiting {
			found = append(found, current)
		}
	}
	sort.SliceStable(found, func(left, right int) bool {
		if found[left].order != found[right].order {
			return found[left].order < found[right].order
		}
		return found[left].arrived.Before(found[right].arrived)
	})
	return found
}

func (q *queue) position(target *run) int {
	for index, current := range q.waiting() {
		if current == target {
			return index + 1
		}
	}
	return 0
}

// schedulerState is the queue as the scheduler sees it at now, without the runs whose processes end for a restart
// and the restarted runs whose old command may still get its SIGKILL.
func (q *queue) schedulerState(now time.Time) scheduler.State {
	var state scheduler.State
	for _, current := range q.runs {
		if current.restarting || now.Before(current.restartedAt.Add(procs.KillAfter)) {
			continue
		}
		state.Runs = append(state.Runs, scheduler.Run{
			ID: current.id, Checkout: current.register.Worktree, Status: current.status, Order: current.order,
			Arrived: current.arrived, Started: current.started, Predicted: current.predicted,
			PredictedBytes: current.predictedBytes, PeakBytes: current.peak, Exclusive: current.register.Exclusive, Restart: current.register.Restart,
			PausedReason: current.pausedReason, PausedAt: current.pausedAt,
		})
	}
	return state
}

// statusOf describes an active or waiting run for `heavy --status --json`.
func (q *queue) statusOf(current *run) protocol.RunStatus {
	state, since := "waiting", current.arrived
	switch current.status {
	case scheduler.Running:
		state, since = "running", current.started
	case scheduler.Paused:
		state, since = "paused", current.started
	}
	return protocol.RunStatus{
		State: state, PID: current.register.ClientPID, Since: since.Unix(), Order: current.order,
		Directory: current.register.Directory, Label: current.register.Label, Command: strings.Join(current.register.Command, " "),
		Agent: current.register.Agent, Pane: current.register.Pane, Session: current.register.Session, Position: q.position(current),
		ID: current.id, Checkout: current.register.Checkout, Budget: current.cores, RSSBytes: current.rss, PredictedBytes: current.predictedBytes,
		PausedReason: string(current.pausedReason), Restarts: current.restarts,
	}
}

// snapshot lists the active runs by start, the waiting ones by position, then those finished in the last 30 s,
// newest first.
func (q *queue) snapshot(now time.Time) []protocol.RunStatus {
	var kept []finished
	for _, current := range q.finished {
		if current.expires.After(now) {
			kept = append(kept, current)
		}
	}
	q.finished = kept
	var active []*run
	for _, current := range q.runs {
		if current.status != scheduler.Waiting {
			active = append(active, current)
		}
	}
	sort.SliceStable(active, func(left, right int) bool { return active[left].started.Before(active[right].started) })
	statuses := []protocol.RunStatus{}
	for _, current := range append(active, q.waiting()...) {
		statuses = append(statuses, q.statusOf(current))
	}
	for index := len(q.finished) - 1; index >= 0; index-- {
		statuses = append(statuses, q.finished[index].status)
	}
	return statuses
}
