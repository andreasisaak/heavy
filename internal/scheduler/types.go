// Package scheduler decides what the daemon does with the queue (design G4 and G5): which waiting run starts,
// which running run pauses under memory pressure, which paused run resumes, and which run ends to start again.
// Decide is a pure function without I/O; the daemon measures, calls it every 2 s and carries out its actions.
package scheduler

import (
	"time"

	"github.com/andreasisaak/heavy/internal/lists"
)

// Status says where a run stands.
type Status int

const (
	// Waiting runs wait in the queue.
	Waiting Status = iota
	// Running runs run their command.
	Running
	// Paused runs are stopped with SIGSTOP; they count as active.
	Paused
)

// PauseReason says why a run is paused.
type PauseReason string

const (
	// NotPaused is the reason of a run that is not paused.
	NotPaused PauseReason = ""
	// PausedForMemory runs resume once memory has recovered.
	PausedForMemory PauseReason = "memory"
	// PausedByUser runs stay paused until the user resumes them.
	PausedByUser PauseReason = "user"
)

// Run is a run the daemon knows.
type Run struct {
	ID string
	// Checkout is the git worktree the run works in (its toplevel); one run per worktree is active at a time, so
	// worktrees of one repository run side by side.
	Checkout string
	Status   Status
	// Order sorts the queue: the smaller order waits ahead. Ties go by Arrived.
	Order float64
	// Arrived is when the run began to wait.
	Arrived time.Time
	// Started is when the run started; zero for a waiting run.
	Started time.Time
	// Predicted is false for a run without history; PredictedBytes is then 0.
	Predicted bool
	// PredictedBytes is the expected peak memory of the run (design G11).
	PredictedBytes int64
	Exclusive      bool
	Restart        bool
	// PausedReason is NotPaused unless Status is Paused.
	PausedReason PauseReason
	// PausedAt is when the run was paused; zero unless Status is Paused.
	PausedAt time.Time
}

// State is the queue and the times of the daemon's last resume and last restart.
type State struct {
	Runs []Run
	// LastResume is when the daemon last resumed a run paused for memory; zero for never.
	LastResume time.Time
	// LastRestart is when the daemon last ended a run to start it again; zero for never.
	LastRestart time.Time
}

// Measurements are what the sampler saw at the time of a decision.
type Measurements struct {
	// Cores are the cores of the docker VM, or of the host when the VM is not measured.
	Cores int
	// Load1 is the load average over one minute, of the VM or else the host.
	Load1 float64
	// VMMeasured is false when docker is not reachable: then the host's memory stands in for the VM's.
	VMMeasured bool
	// VMAvailableBytes is MemAvailable of the docker VM.
	VMAvailableBytes int64
	// HostFreePercent is kern.memorystatus_level of the host.
	HostFreePercent int
	// HostMemoryBytes is hw.memsize of the host.
	HostMemoryBytes int64
	// SwapOutBytesPer2s is the memory swapped out per 2 s, averaged over the last 30 s, the larger of VM and host.
	SwapOutBytesPer2s int64
	// PressureFor is how long memory pressure has held without a break: VM below vm_pressure_bytes or host below
	// host_pressure_percent. 0 without pressure.
	PressureFor time.Duration
	// RecoveredFor is how long memory has stayed recovered without a break: VM at least vm_recovered_bytes and
	// host at least host_recovered_percent. 0 when not recovered.
	RecoveredFor time.Duration
	// OtherActive counts the runs the daemons of other users have active on this machine, by the slots they hold;
	// 0 without a machine configuration.
	OtherActive int
}

// Config holds the thresholds of the personal list and the holding times of design G5.
type Config struct {
	Load lists.Load
	// PressureHold: pressure must hold this long before a pause, and pauses come at least this far apart.
	PressureHold time.Duration
	// RecoveryHold: recovery must hold this long before a resume, and resumes come at least this far apart.
	RecoveryHold time.Duration
	// RestartAfter: pressure that holds this long after the last memory pause, and after the last restart, ends a
	// run with restart.
	RestartAfter time.Duration
	// BackfillFor: a waiting run that does not fit lets the fitting runs behind it pass until it has waited this long.
	BackfillFor time.Duration
}

// DefaultConfig returns the thresholds of design G4 and G5 with the holding times 10 s, 30 s and 60 s, and lets runs
// pass a blocked one for 15 minutes.
func DefaultConfig(load lists.Load) Config {
	return Config{Load: load, PressureHold: 10 * time.Second, RecoveryHold: 30 * time.Second, RestartAfter: 60 * time.Second,
		BackfillFor: 15 * time.Minute}
}

// Kind is what an action does.
type Kind string

const (
	// Start starts a waiting run with Cores cores (HEAVY_CPUS).
	Start Kind = "start"
	// Pause pauses a running run for Reason.
	Pause Kind = "pause"
	// Resume resumes a paused run.
	Resume Kind = "resume"
	// Terminate ends a run's processes; a Requeue for the same run follows it.
	Terminate Kind = "terminate"
	// Requeue puts a terminated run back into the queue with Order, ahead of every waiting run.
	Requeue Kind = "requeue"
)

// Action is one thing the daemon does to a run.
type Action struct {
	Kind   Kind
	ID     string
	Cores  int
	Reason PauseReason
	Order  float64
}
