// Package daemon is heavy's one daemon per machine: it starts on demand, holds the queue, and ends a while after
// the last run (design G3). Clients reach it on the socket $STATE/daemon.sock; Connect starts it when none answers.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/andreasisaak/heavy/internal/foreign"
	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/procs"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/sampler"
	"github.com/andreasisaak/heavy/internal/slots"
)

// maxSocketPath is the longest socket path macOS takes: sun_path holds 104 bytes with the closing zero.
const maxSocketPath = 103

// Options configure a daemon.
type Options struct {
	StateDirectory string
	Identity       protocol.Identity
	Clock          Clock
	// IdleTimeout is how long the daemon stays without runs and subscribers before it ends.
	IdleTimeout time.Duration
	// HelloTimeout is how long a new connection may take to send its hello.
	HelloTimeout time.Duration
	// ReadHost measures the host, every 2 s.
	ReadHost func() (sampler.HostSample, error)
	// PersonalList is the path of ~/.config/heavy/config.json, read again every 2 s for the thresholds.
	PersonalList string
	// InstalledStamp reads the stamp of the build now on disk next to the daemon's binary, again for each newer
	// client of the same version.
	InstalledStamp func() (string, error)
	// RedrawHerdr asks the herdr plugin local.heavy-queue to redraw the sidebar; nil without herdr.
	RedrawHerdr func() error
	// VM reaches the helper in the Docker VM; nil when the daemon cannot measure the VM, for the reason WithoutVM.
	VM        *sampler.VMOptions
	WithoutVM string
	// Local reads the machine the daemon runs on, where docker needs no VM (native Linux); it stands in for VM.
	// Docker is then the docker program for the containers of the runs, "" without docker.
	Local         *sampler.LocalOptions
	Docker        string
	DockerTimeout time.Duration
	// MachineConfig is the path of the machine configuration, read again every 2 s: the limit of active runs over
	// the daemons of all users and the folder of their slots. "" or a missing file leaves each user's own limit.
	MachineConfig string
}

// adoptionWindow is how long a daemon that found the socket of a crashed one starts no run, so the clients of the
// runs of that daemon can announce them again first (FR-17).
const adoptionWindow = 5 * time.Second

// containerRefresh is how long the daemon trusts what docker inspect said of a container before it inspects it again.
const containerRefresh = 30 * time.Second

// StateDirectory is $HEAVY_STATE_DIRECTORY, or ~/.cache/heavy.
func StateDirectory() (string, error) {
	if directory := os.Getenv("HEAVY_STATE_DIRECTORY"); directory != "" {
		return directory, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("heavy: finding the home directory for the state directory: %w", err)
	}
	return filepath.Join(home, ".cache", "heavy"), nil
}

// socketPath is $STATE/daemon.sock, or an error when it is longer than macOS can bind; the daemon, the client and
// the hooks check it alike, so a client names the reason instead of waiting for a daemon that cannot start.
func socketPath(stateDirectory string) (string, error) {
	socket := filepath.Join(stateDirectory, "daemon.sock")
	if len(socket) > maxSocketPath {
		return "", fmt.Errorf("heavy: the socket path %s has %d bytes, more than the %d macOS allows; set HEAVY_STATE_DIRECTORY to a shorter directory", socket, len(socket), maxSocketPath)
	}
	return socket, nil
}

// Run serves until ctx ends, the daemon stays idle for IdleTimeout, or a newer heavy takes over. A second daemon
// logs the running one and returns nil at once.
func Run(ctx context.Context, options Options) error {
	socket, err := socketPath(options.StateDirectory)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(options.StateDirectory, 0o700); err != nil {
		return fmt.Errorf("heavy: creating the state directory %s: %w", options.StateDirectory, err)
	}
	logPath := filepath.Join(options.StateDirectory, "daemon.log")
	lock, holder, err := acquireLock(filepath.Join(options.StateDirectory, "daemon.lock"))
	if errors.Is(err, errHeld) {
		return logHeld(logPath, holder)
	}
	if err != nil {
		return err
	}
	defer lock.Close()
	log, err := openRotatingLog(logPath)
	if err != nil {
		return err
	}
	defer log.Close()
	logger := slog.New(slog.NewJSONHandler(log, nil))
	crashed, err := removeStaleSocket(socket, logger)
	if err != nil {
		return err
	}
	handedRuns, err := takeAdoptionMark(options.StateDirectory, logger)
	if err != nil {
		return err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		logger.Error("listening failed", "socket", socket, "error", err.Error())
		return fmt.Errorf("heavy: listening on %s: %w", socket, err)
	}
	vmNote := "not measured: " + options.WithoutVM
	switch {
	case options.Local != nil:
		vmNote = "the machine itself, " + options.Local.ProcRoot
	case options.VM != nil:
		vmNote = "helper " + options.VM.Image
	}
	logger.Info("daemon started", "pid", os.Getpid(), "version", options.Identity.Version, "stamp", options.Identity.Stamp, "protocol", protocol.Version,
		"vm", vmNote)
	current := &server{
		identity: options.Identity, readInstalled: options.InstalledStamp, redrawHerdr: options.RedrawHerdr, logger: logger, helloTimeout: options.HelloTimeout, handover: make(chan protocol.Identity, 1),
		connections: map[net.Conn]bool{}, subscribers: map[*subscriber]bool{}, checkouts: map[string]string{}, clock: options.Clock, stateDirectory: options.StateDirectory, readHost: options.ReadHost,
		personalList: options.PersonalList, machine: sampler.NewMachine(), load: lists.DefaultLoad(), personalLoad: lists.DefaultLoad(),
		jobs: make(chan func(), 64), worked: make(chan struct{}),
		machineConfig: options.MachineConfig, slots: slots.NewPool(),
	}
	docker, dockerTimeout := options.Docker, options.DockerTimeout
	switch {
	case options.Local != nil:
		current.vm = sampler.NewLocal(*options.Local)
	case options.VM != nil:
		vm := *options.VM
		vm.Now, vm.Logger = options.Clock.Now, logger
		current.vm = sampler.NewVM(vm)
		docker, dockerTimeout = vm.Docker, vm.Timeout
	default:
		current.machine.VMFailed(errors.New(options.WithoutVM))
	}
	if current.vm != nil {
		defer current.vm.Close()
		if docker != "" {
			current.docker = procs.NewSystemDocker(docker, dockerTimeout, containerRefresh, options.Clock.Now)
			current.dockerProgram, current.dockerTimeout = docker, dockerTimeout
			current.foreign = foreign.NewNotifier(foreign.MainCheckout)
		}
	}
	go current.work()
	if crashed || handedRuns {
		current.adoptUntil = options.Clock.Now().Add(adoptionWindow)
	}
	current.cleanHistory()
	current.sample()
	accepted := make(chan struct{})
	go func() {
		current.accept(listener)
		close(accepted)
	}()
	reason := current.serve(ctx, options)
	listener.Close()
	<-accepted
	current.state.Lock()
	current.closing = true
	runs := len(current.queue.runs)
	current.state.Unlock()
	if runs > 0 {
		leaveAdoptionMark(options.StateDirectory, runs, logger)
	}
	current.closeConnections()
	close(current.jobs)
	<-current.worked
	logger.Info("daemon ends", reason...)
	return nil
}

// serve measures every 2 s and waits for the reason to end, which it returns as log attributes.
func (s *server) serve(ctx context.Context, options Options) []any {
	idleSince := options.Clock.Now()
	for tick := 1; ; tick++ {
		select {
		case <-ctx.Done():
			return []any{"reason", "stopped"}
		case client := <-s.handover:
			return []any{"reason", "a newer heavy takes over", "newer_version", client.Version, "newer_stamp", client.Stamp}
		case now := <-options.Clock.Ticks():
			if tick%2 == 0 {
				s.sample()
			}
			if now.YearDay() != s.cleaned.YearDay() || now.Year() != s.cleaned.Year() {
				s.cleanHistory()
			}
			s.redraw(now)
			if !s.idle() {
				idleSince = now
			} else if now.Sub(idleSince) >= options.IdleTimeout {
				return []any{"reason", "idle for " + options.IdleTimeout.String()}
			}
		}
	}
}

// logHeld notes in the log that another daemon runs; the log stays with that daemon, which alone moves it.
func logHeld(path string, holder int) error {
	file, err := openAppend(path)
	if err != nil {
		return err
	}
	defer file.Close()
	slog.New(slog.NewJSONHandler(file, nil)).Info("daemon already running", "holder", holder)
	return nil
}

// removeStaleSocket deletes the socket a crashed daemon left, and reports whether there was one; only the holder of
// the lock gets here, and a daemon that ends cleanly removes its socket.
func removeStaleSocket(socket string, logger *slog.Logger) (bool, error) {
	err := os.Remove(socket)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		logger.Error("removing the stale socket failed", "socket", socket, "error", err.Error())
		return false, fmt.Errorf("heavy: removing the stale socket %s: %w", socket, err)
	}
	logger.Info("removed the socket of a crashed daemon", "socket", socket)
	return true, nil
}
