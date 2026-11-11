package procs

import (
	"errors"
	"fmt"
	"syscall"
)

// Terminate ends the processes of a run: SIGTERM to the group and the tagged VM processes, SIGCONT to both so a
// paused run sees the SIGTERM, unpause of its paused containers, and after 10 s SIGKILL to what is still alive. The
// error says what failed; nothing is sent when the group is refused. The channel closes after the SIGKILL phase.
func (t Tracker) Terminate(run Run) (<-chan struct{}, error) {
	if err := t.refuse(run.ProcessGroup); err != nil {
		return nil, err
	}
	failures := []error{ended(t.Host.Signal(run.ProcessGroup, syscall.SIGTERM))}
	pids, err := t.vmPIDs(run)
	if err != nil {
		failures = append(failures, fmt.Errorf("heavy: VM processes of run %s not terminated: %w", run.ID, err))
	}
	failures = append(failures, t.sendVM(run, "TERM", "terminated", pids), ended(t.Host.Signal(run.ProcessGroup, syscall.SIGCONT)),
		t.sendVM(run, "CONT", "continued", pids), t.unpause(run))
	t.Logger.Info("terminated the processes of a run", "id", run.ID, "process_group", run.ProcessGroup)
	done := make(chan struct{})
	wait := t.Clock.After(KillAfter)
	go func() {
		defer close(done)
		<-wait
		t.kill(run)
	}()
	return done, errors.Join(failures...)
}

// ended drops the error of a signal to a group whose processes all ended, often right after SIGTERM: ending them
// is what Terminate is for (probe 2026-11-08: every heavy --stop printed "sending SIGCONT …: no such process").
func ended(err error) error {
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// kill sends SIGKILL to the group and the tagged VM processes that outlived SIGTERM.
func (t Tracker) kill(run Run) {
	if t.Host.Alive(run.ProcessGroup) {
		t.Logger.Info("killing what is left of a run", "id", run.ID, "process_group", run.ProcessGroup)
		if err := t.Host.Signal(run.ProcessGroup, syscall.SIGKILL); err != nil {
			t.Logger.Warn("killing the process group of a run failed", "id", run.ID, "error", err.Error())
		}
	}
	if err := t.signalVM(run, "KILL", "killed"); err != nil {
		t.Logger.Warn("killing the VM processes of a run failed", "id", run.ID, "error", err.Error())
	}
}

// RSS is the resident memory of a run: its group, its VM processes outside its co-started containers, and those
// containers. When the VM part fails, the host part comes with the error.
func (t Tracker) RSS(run Run) (int64, error) {
	host, err := t.Host.RSS(run.ProcessGroup)
	if err != nil {
		return 0, fmt.Errorf("heavy: measuring the process group of run %s: %w", run.ID, err)
	}
	vm, err := t.vmRSS(run)
	if err != nil {
		return host, fmt.Errorf("heavy: VM processes of run %s not measured: %w", run.ID, err)
	}
	return host + vm, nil
}

func (t Tracker) vmRSS(run Run) (int64, error) {
	processes, err := t.VM.Find(run.ID)
	if err != nil {
		return 0, err
	}
	usage, err := t.VM.Containers()
	if err != nil {
		return 0, err
	}
	containers, err := t.CoStarted(run)
	if err != nil {
		return 0, err
	}
	own := map[string]bool{}
	for _, container := range containers {
		own[container.ID] = true
	}
	var total int64
	for _, process := range processes {
		if !own[process.Container] {
			total += process.RSSBytes
		}
	}
	for _, container := range usage {
		if own[container.ID] {
			total += container.RSSBytes
		}
	}
	return total, nil
}
