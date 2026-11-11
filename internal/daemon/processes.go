package daemon

import (
	"errors"
	"time"

	"github.com/andreasisaak/heavy/internal/helper"
	"github.com/andreasisaak/heavy/internal/procs"
	"github.com/andreasisaak/heavy/internal/sampler"
)

// afterClock waits in real time before the SIGKILL phase of a run's end.
type afterClock struct{}

func (afterClock) After(wait time.Duration) <-chan time.Time { return time.After(wait) }

// absentVM stands for the VM while the daemon does not measure it: a run then has no VM processes heavy can reach.
type absentVM struct{}

func (absentVM) Find(string) ([]helper.Process, error)   { return nil, nil }
func (absentVM) Containers() ([]helper.Container, error) { return nil, nil }
func (absentVM) Signal(string, string, []int) (helper.Signaled, error) {
	return helper.Signaled{Sent: []int{}, Skipped: []int{}}, nil
}

// absentDocker stands for docker while the VM is not measured: heavy reaches no container then.
type absentDocker struct{}

func (absentDocker) Containers() ([]procs.Container, error) { return nil, nil }
func (absentDocker) Pause(string) error                     { return nil }
func (absentDocker) Unpause(string) error                   { return nil }

// machineHelper is what the daemon asks where docker runs: sampler.VM in the Docker VM, sampler.Local on Linux.
type machineHelper interface {
	procs.VM
	Sample() (helper.Sample, error)
	Close() error
}

// tracker acts on the processes of runs; while the VM is not measured it leaves the VM and docker out, since every
// docker call would wait for its timeout. The caller holds state.
func (s *server) tracker() procs.Tracker {
	tracker := procs.Tracker{Host: procs.SystemHost{}, VM: absentVM{}, Docker: absentDocker{}, Clock: afterClock{}, Logger: s.logger}
	if s.vm != nil && s.machine.VMStatus() == "" {
		tracker.VM = s.vm
		if s.docker != nil {
			tracker.Docker = s.docker
		}
	}
	return tracker
}

// processRun is what procs needs of a run.
func processRun(current *run) procs.Run {
	return procs.Run{ID: current.id, ProcessGroup: current.processGroup, Worktree: current.register.Worktree, Started: current.started}
}

// work runs the process jobs one after another, outside the state lock: signals to groups, to the helper and to
// docker can take seconds, and they must keep the order of the decisions.
func (s *server) work() {
	for job := range s.jobs {
		job()
	}
	close(s.worked)
}

// queueJob queues the job after those queued before it; the channel gives its error.
func (s *server) queueJob(job func() error) <-chan error {
	result := make(chan error, 1)
	s.jobs <- func() { result <- job() }
	return result
}

// reportable drops the parts of an error that only say docker does not answer: the VM then has no processes
// heavy can reach, which is no failure of the action.
func reportable(err error) error {
	if err == nil {
		return nil
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		if errors.Is(err, sampler.ErrNotReachable) {
			return nil
		}
		return err
	}
	var kept []error
	for _, part := range joined.Unwrap() {
		if part := reportable(part); part != nil {
			kept = append(kept, part)
		}
	}
	return errors.Join(kept...)
}

// report logs a failed process job and tells the run what failed.
func (s *server) report(current *run, action string, err error) {
	if err == nil {
		return
	}
	if kept := reportable(err); kept != nil {
		s.logger.Warn("acting on the processes of a run failed", "id", current.id, "action", action, "error", kept.Error())
		s.tell(current, kept.Error())
		return
	}
	s.logger.Info("the VM processes of a run were out of reach", "id", current.id, "action", action, "error", err.Error())
}
