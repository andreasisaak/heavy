package daemon

import (
	"errors"
	"strings"

	"github.com/andreasisaak/heavy/internal/helper"
	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/procs"
	"github.com/andreasisaak/heavy/internal/scheduler"
)

// measured is the resident memory of one active run.
type measured struct {
	run   *run
	bytes int64
}

// sample measures the host, the VM and the memory of each active run, reads the thresholds of the personal list
// again (a broken list keeps the last ones and is logged once), and decides; a tick at the time of the last sample
// measures nothing. The measuring happens outside the state lock, since the VM and docker can take seconds.
func (s *server) sample() {
	now := s.clock.Now()
	s.state.Lock()
	moved := s.sampledAt.IsZero() || now.After(s.sampledAt)
	s.sampledAt = now
	// Pressure of 10 s in the samples so far, or a dashboard that asked in the last 10 s, makes this sample look at
	// the containers outside the runs, every 10 s (design G7, G10, G13).
	pressure := s.machine.Measurements(s.load).PressureFor >= scheduler.DefaultConfig(s.load).PressureHold
	look := s.foreign != nil && (pressure || now.Sub(s.detailAt) <= foreignEvery) && now.Sub(s.foreignAt) >= foreignEvery
	if look {
		s.foreignAt = now
	}
	s.state.Unlock()
	if !moved {
		return
	}
	host, hostError := s.readHost()
	// The clock of the daemon times every sample, as it times the decisions.
	host.Time = now
	var vm helper.Sample
	var vmError error
	if s.vm != nil {
		vm, vmError = s.vm.Sample()
	}
	usage, inRuns := s.measureRuns(look)
	var load foreignLoad
	if look {
		load = s.listForeign(inRuns)
	}
	personal := lists.LoadPersonal(s.personalList)
	limit, limited, limitError := s.readMachine()
	s.state.Lock()
	if hostError != nil {
		s.logger.Error("measuring the host failed", "error", hostError.Error())
	} else {
		s.machine.AddHost(host)
	}
	if s.vm != nil {
		before := s.machine.VMStatus()
		if vmError != nil {
			s.machine.VMFailed(vmError)
		} else {
			s.machine.AddVM(now, vm)
		}
		if after := s.machine.VMStatus(); after != before {
			s.logger.Info("the VM measurement changed", "status", after, "before", before)
		}
	}
	for _, current := range usage {
		current.run.rss, current.run.peak = current.bytes, max(current.run.peak, current.bytes)
	}
	s.thresholdsLocked(personal)
	s.limitLocked(limit, limited, limitError)
	s.claimSlots()
	s.decideLocked()
	available := s.machine.Measurements(s.load).VMAvailableBytes
	s.state.Unlock()
	if look {
		s.lookAtForeign(now, available, pressure, load)
	}
}

// thresholdsLocked takes the thresholds of the personal list. A list with a value heavy cannot use keeps the last
// thresholds, so a list caught in the middle of an edit changes nothing; a list that only holds keys this heavy
// does not know counts. What the list holds beside that is logged once per change. The caller holds state.
func (s *server) thresholdsLocked(personal lists.Personal) {
	if !personal.Broken {
		s.personalLoad = personal.Load
	}
	noted := strings.Join(personal.Warnings, "; ")
	if noted == s.loadError {
		return
	}
	s.loadError = noted
	switch {
	case personal.Broken:
		s.logger.Error("reading the thresholds failed, the last ones stay", "error", noted)
	case noted != "":
		s.logger.Warn("the personal list holds keys this heavy does not know", "warning", noted)
	}
}

// listedOnce lists the containers once for all runs of one sample: docker inspect over every container of the
// machine takes up to a second (71 containers, probe 2026-11-08).
type listedOnce struct {
	procs.Docker
	listed     bool
	containers []procs.Container
	err        error
}

func (l *listedOnce) Containers() ([]procs.Container, error) {
	if !l.listed {
		l.containers, l.err = l.Docker.Containers()
		l.listed = true
	}
	return l.containers, l.err
}

// measureRuns measures the resident memory of the active runs that told their process group; under pressure it
// also names the containers of the runs, from the same container list, and their tagged processes.
func (s *server) measureRuns(pressure bool) ([]measured, runContainers) {
	s.state.Lock()
	tracker := s.tracker()
	tracker.Docker = &listedOnce{Docker: tracker.Docker}
	var active []*run
	var runs []procs.Run
	for _, current := range s.queue.runs {
		if current.processGroup > 1 && !current.restarting {
			active, runs = append(active, current), append(runs, processRun(current))
		}
	}
	s.state.Unlock()
	usage := make([]measured, 0, len(active))
	inRuns := runContainers{owned: map[string]bool{}}
	var failures []error
	for index, current := range active {
		bytes, err := tracker.RSS(runs[index])
		if err := reportable(err); err != nil {
			s.logger.Warn("measuring a run failed", "id", current.id, "error", err.Error())
		}
		usage = append(usage, measured{run: current, bytes: bytes})
		if pressure {
			containers, err := tracker.CoStarted(runs[index])
			failures = append(failures, err)
			for _, container := range containers {
				inRuns.owned[container.ID] = true
			}
			processes, err := tracker.VM.Find(runs[index].ID)
			failures = append(failures, err)
			inRuns.tagged = append(inRuns.tagged, processes...)
		}
	}
	inRuns.err = errors.Join(failures...)
	return usage, inRuns
}
