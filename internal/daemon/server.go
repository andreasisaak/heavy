package daemon

import (
	"log/slog"
	"net"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/andreasisaak/heavy/internal/foreign"
	"github.com/andreasisaak/heavy/internal/history"
	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/procs"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/sampler"
	"github.com/andreasisaak/heavy/internal/scheduler"
	"github.com/andreasisaak/heavy/internal/slots"
)

// server holds what the connections of a daemon share. mutex guards the connections, state the queue and the
// measurements.
type server struct {
	identity      protocol.Identity
	readInstalled func() (string, error)
	redrawHerdr   func() error
	// redrawnFor is the queue the sidebar was last redrawn for, at redrawnAt; redrawing is set while herdr works,
	// redrawError is the last error of herdr, logged once.
	redrawnFor     string
	redrawnAt      time.Time
	redrawing      atomic.Bool
	redrawError    string
	logger         *slog.Logger
	helloTimeout   time.Duration
	handover       chan protocol.Identity
	mutex          sync.Mutex
	connections    map[net.Conn]bool
	handlers       sync.WaitGroup
	clock          Clock
	stateDirectory string
	readHost       func() (sampler.HostSample, error)
	personalList   string
	state          sync.Mutex
	queue          queue
	// subscribers are the connected monitors, guarded by state.
	subscribers map[*subscriber]bool
	machine     *sampler.Machine
	// vm is the helper that measures where docker runs and reaches the processes there: in the Docker VM of a Mac,
	// or the machine itself on Linux; nil when neither is there. docker is nil without a docker program.
	vm     machineHelper
	docker procs.Docker
	// dockerProgram and dockerTimeout list the containers for foreign, which notices the big ones outside runs.
	dockerProgram string
	dockerTimeout time.Duration
	foreign       *foreign.Notifier
	// foreignAt is when the daemon last looked for foreign load; detailAt when a dashboard last asked for the
	// machine. biggest are the biggest containers outside the runs of the look at biggestAt, checkouts the main
	// checkout of each compose working directory seen, so git runs once per directory.
	foreignAt time.Time
	detailAt  time.Time
	biggest   []protocol.ForeignContainer
	biggestAt time.Time
	checkouts map[string]string
	// jobs carries the work on the processes of runs, done in order by work; worked closes when it is done.
	jobs   chan func()
	worked chan struct{}
	// sampledAt is the time of the last sample.
	sampledAt time.Time
	// lastResume and lastRestart are when the daemon last resumed a run paused for memory and last restarted one.
	lastResume  time.Time
	lastRestart time.Time
	// load are the thresholds in force: personalLoad, those of the personal list, with the limit of the machine.
	load         lists.Load
	personalLoad lists.Load
	loadError    string
	// machineConfig is the path of the machine configuration, "" for none; limit is the last good one read, limited
	// whether there is one, and slots the machine slots this daemon holds for its active runs.
	machineConfig string
	limit         slots.Config
	limited       bool
	limitError    string
	slots         *slots.Pool
	adoptUntil    time.Time
	// closing is set when the daemon ends: its runs go on and their clients announce them to the next daemon.
	closing bool
	// cleaned is when the daemon last deleted old history files, at its start and then daily.
	cleaned time.Time
}

// cleanHistory deletes the history files older than 30 days (design G11).
func (s *server) cleanHistory() {
	s.cleaned = s.clock.Now()
	if err := history.Clean(filepath.Join(s.stateDirectory, "history"), s.cleaned); err != nil {
		s.logger.Error("deleting old history files failed", "error", err.Error())
	}
}

func (s *server) accept(listener net.Listener) {
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		s.mutex.Lock()
		s.connections[connection] = true
		s.handlers.Add(1)
		s.mutex.Unlock()
		go s.handle(connection)
	}
}

// closeConnections ends the open connections once accept has returned, so no handler starts after Wait.
func (s *server) closeConnections() {
	s.mutex.Lock()
	for connection := range s.connections {
		connection.Close()
	}
	s.mutex.Unlock()
	s.handlers.Wait()
}

// installedStamp is the stamp of the build on disk; one that cannot be read takes over nothing.
func (s *server) installedStamp() string {
	stamp, err := s.readInstalled()
	if err != nil {
		s.logger.Warn("reading the stamp of the installed build failed", "error", err.Error())
		return ""
	}
	return stamp
}

// bound reports a run that would not survive a new daemon. A client announces its run to a new daemon as waiting
// or as running with the process group of its command, and no more: a run that is paused would stay paused, one
// that got its start but has not named its process group yet would be announced as waiting and start twice, and
// one that ends for a restart or a stop is between both. The daemon hands over to a newer heavy as soon as it
// holds no such run; the waiting and the running ones go to the new daemon through their clients.
func (s *server) bound() bool {
	s.state.Lock()
	defer s.state.Unlock()
	now := s.clock.Now()
	for _, current := range s.queue.runs {
		switch {
		case current.restarting, current.stopping, now.Before(current.restartedAt.Add(procs.KillAfter)):
			return true
		case current.status == scheduler.Waiting:
		case current.status == scheduler.Running && current.processGroup > 1:
		default:
			return true
		}
	}
	return false
}

// idle reports neither runs nor monitors; the daemon ends after IdleTimeout of it (design G3).
func (s *server) idle() bool {
	s.state.Lock()
	defer s.state.Unlock()
	return len(s.queue.runs) == 0 && len(s.subscribers) == 0
}
