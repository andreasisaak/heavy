// Package procs knows the processes of a run (design G6): the process group of its command on the host, the VM
// processes that carry its tag, and the containers of its worktree started after it; it pauses, resumes, ends and
// measures them together.
package procs

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/andreasisaak/heavy/internal/helper"
)

// KillAfter is how long the processes of a run get after SIGTERM before SIGKILL.
const KillAfter = 10 * time.Second

// Run is what procs needs of a run: its tag, the group of its command, its worktree and when it started.
type Run struct {
	ID           string
	ProcessGroup int
	Worktree     string
	Started      time.Time
}

// Container is a container of docker with its compose working directory and start.
type Container struct {
	ID               string
	Name             string
	WorkingDirectory string
	Started          time.Time
	Paused           bool
}

// Host signals process groups of the host and measures them.
type Host interface {
	Signal(group int, signal syscall.Signal) error
	Alive(group int) bool
	RSS(group int) (int64, error)
	OwnGroup() int
}

// VM is the helper in the Docker VM.
type VM interface {
	Find(tag string) ([]helper.Process, error)
	Containers() ([]helper.Container, error)
	Signal(tag, signal string, pids []int) (helper.Signaled, error)
}

// Docker lists, pauses and unpauses containers.
type Docker interface {
	Containers() ([]Container, error)
	Pause(name string) error
	Unpause(name string) error
}

// Clock gives the wait before the SIGKILL phase.
type Clock interface {
	After(time.Duration) <-chan time.Time
}

// Tracker acts on the processes of runs.
type Tracker struct {
	Host   Host
	VM     VM
	Docker Docker
	Clock  Clock
	Logger *slog.Logger
}

// refuse rejects a group heavy must never signal: none, init, or the daemon's own.
func (t Tracker) refuse(group int) error {
	if group <= 1 {
		return fmt.Errorf("heavy: refusing to signal process group %d", group)
	}
	if group == t.Host.OwnGroup() {
		return fmt.Errorf("heavy: refusing to signal process group %d: it is the daemon's own group", group)
	}
	return nil
}

// CoStarted lists the containers of the run's worktree started after the run; a run without an absolute worktree
// claims none, since every container would lie below an empty one.
func (t Tracker) CoStarted(run Run) ([]Container, error) {
	if !filepath.IsAbs(run.Worktree) {
		return nil, fmt.Errorf("heavy: run %s has the worktree %q, expected an absolute path, so heavy claims no container", run.ID, run.Worktree)
	}
	all, err := t.Docker.Containers()
	if err != nil {
		return nil, fmt.Errorf("heavy: listing the containers of run %s: %w", run.ID, err)
	}
	var found []Container
	worktree := filepath.Clean(run.Worktree)
	for _, container := range all {
		directory := filepath.Clean(container.WorkingDirectory)
		inside := filepath.IsAbs(directory) && (directory == worktree || strings.HasPrefix(directory, worktree+string(filepath.Separator)))
		if inside && container.Started.After(run.Started) {
			found = append(found, container)
		}
	}
	return found, nil
}

// signalVM finds the tagged VM processes and sends them the signal.
func (t Tracker) signalVM(run Run, signal, verb string) error {
	pids, err := t.vmPIDs(run)
	if err != nil {
		return fmt.Errorf("heavy: VM processes of run %s not %s: %w", run.ID, verb, err)
	}
	return t.sendVM(run, signal, verb, pids)
}

func (t Tracker) vmPIDs(run Run) ([]int, error) {
	processes, err := t.VM.Find(run.ID)
	if err != nil {
		return nil, err
	}
	pids := make([]int, len(processes))
	for index, process := range processes {
		pids[index] = process.PID
	}
	return pids, nil
}

// sendVM sends the signal to the VM processes; a run without VM processes sends nothing.
func (t Tracker) sendVM(run Run, signal, verb string, pids []int) error {
	if len(pids) == 0 {
		return nil
	}
	signaled, err := t.VM.Signal(run.ID, signal, pids)
	if err != nil {
		return fmt.Errorf("heavy: VM processes of run %s not %s: %w", run.ID, verb, err)
	}
	t.Logger.Info("signaled the VM processes of a run", "id", run.ID, "signal", signal, "sent", signaled.Sent, "skipped", signaled.Skipped)
	return nil
}

// Pause stops the group, the tagged VM processes and pauses the co-started containers.
func (t Tracker) Pause(run Run) error {
	if err := t.refuse(run.ProcessGroup); err != nil {
		return err
	}
	hostError := t.Host.Signal(run.ProcessGroup, syscall.SIGSTOP)
	vmError := t.signalVM(run, "STOP", "paused")
	return errors.Join(hostError, vmError, t.containers(run, func(container Container) error {
		if container.Paused {
			return nil
		}
		return t.Docker.Pause(container.Name)
	}))
}

// Resume continues the group and the tagged VM processes and unpauses the co-started containers.
func (t Tracker) Resume(run Run) error {
	if err := t.refuse(run.ProcessGroup); err != nil {
		return err
	}
	hostError := t.Host.Signal(run.ProcessGroup, syscall.SIGCONT)
	vmError := t.signalVM(run, "CONT", "resumed")
	return errors.Join(hostError, vmError, t.unpause(run))
}

func (t Tracker) unpause(run Run) error {
	return t.containers(run, func(container Container) error {
		if !container.Paused {
			return nil
		}
		return t.Docker.Unpause(container.Name)
	})
}

// containers applies the action to each co-started container of the run.
func (t Tracker) containers(run Run, action func(Container) error) error {
	containers, err := t.CoStarted(run)
	if err != nil {
		return err
	}
	var failures []error
	for _, container := range containers {
		if err := action(container); err != nil {
			failures = append(failures, fmt.Errorf("heavy: container %s of run %s: %w", container.Name, run.ID, err))
		}
	}
	return errors.Join(failures...)
}
