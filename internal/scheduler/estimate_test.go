package scheduler_test

import (
	"reflect"
	"testing"

	"github.com/andreasisaak/heavy/internal/scheduler"
)

// estimate sets unpredicted_peak_bytes, the expected peak of a run without history.
func estimate(bytes int64) func(*scheduler.Config) {
	return func(cfg *scheduler.Config) { cfg.Load.UnpredictedPeakBytes = bytes }
}

// peaked is a running run without history whose memory has reached the given peak so far.
func peaked(run scheduler.Run, bytes int64) scheduler.Run {
	return with(run, func(r *scheduler.Run) { r.PeakBytes = bytes })
}

func start(id string, cores int) []scheduler.Action {
	return []scheduler.Action{{Kind: scheduler.Start, ID: id, Cores: cores}}
}

// A run without history starts beside other runs of the same queue when unpredicted_peak_bytes names what to expect
// of it, and the machine has room for that; without the key it starts only alone, as before.
var estimateCases = []struct {
	name  string
	state scheduler.State
	m     scheduler.Measurements
	cfg   scheduler.Config
	want  []scheduler.Action
}{
	{"without the key a run without history waits for an idle queue",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c2", 2, 0)}}, neutral(), config(), nil},
	{"with the key a run without history starts beside a run with history",
		scheduler.State{Runs: []scheduler.Run{with(running("A", "c1", 1, 60), func(r *scheduler.Run) { r.Predicted, r.PredictedBytes = true, gibibyte }),
			waiting("B", "c2", 2, 0)}},
		neutral(vm(6 * gibibyte)), config(estimate(4 * gibibyte)), start("B", 18)},
	{"the estimate and the reserve must fit into the free memory",
		scheduler.State{Runs: []scheduler.Run{with(running("A", "c1", 1, 60), func(r *scheduler.Run) { r.Predicted, r.PredictedBytes = true, gibibyte }),
			waiting("B", "c2", 2, 0)}},
		neutral(vm(6*gibibyte - 1)), config(estimate(4 * gibibyte)), nil},
	{"two runs without history, room for one: the active one still claims the part of the estimate it has not used",
		scheduler.State{Runs: []scheduler.Run{peaked(running("A", "c1", 1, 2), gibibyte), waiting("B", "c2", 2, 0)}},
		neutral(vm(9*gibibyte - 1)), config(estimate(4 * gibibyte)), nil},
	{"two runs without history, room for both",
		scheduler.State{Runs: []scheduler.Run{peaked(running("A", "c1", 1, 2), gibibyte), waiting("B", "c2", 2, 0)}},
		neutral(vm(9 * gibibyte)), config(estimate(4 * gibibyte)), start("B", 18)},
	{"an active run that has used more than the estimate claims nothing more",
		scheduler.State{Runs: []scheduler.Run{peaked(running("A", "c1", 1, 600), 7*gibibyte), waiting("B", "c2", 2, 0)}},
		neutral(vm(6 * gibibyte)), config(estimate(4 * gibibyte)), start("B", 18)},
	{"a paused run without history keeps its claim",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 600), peaked(paused("P", "c3", 2, 300, 100, scheduler.PausedByUser), gibibyte), waiting("B", "c2", 3, 0)}},
		neutral(vm(13*gibibyte - 1)), config(estimate(4 * gibibyte)), nil},
	{"a run with history also leaves room for the unused estimate of an active run without history",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 2), waiting("B", "c2", 2, 3*gibibyte)}},
		neutral(vm(9*gibibyte - 1)), config(estimate(4 * gibibyte)), nil},
	{"with the key the load threshold still holds",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c2", 2, 0)}},
		neutral(load(14.4)), config(estimate(gibibyte)), nil},
	{"with the key the swap threshold still holds",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c2", 2, 0)}},
		neutral(swap(4 << 20)), config(estimate(gibibyte)), nil},
	{"with the key the free memory of the host still holds",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c2", 2, 0)}},
		neutral(host(19)), config(estimate(gibibyte)), nil},
	{"with the key an exclusive run still starts only alone",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), exclusive(waiting("B", "c2", 2, 0))}},
		neutral(), config(estimate(gibibyte)), nil},
	{"with the key an active exclusive run still blocks",
		scheduler.State{Runs: []scheduler.Run{exclusive(running("A", "c1", 1, 60)), waiting("B", "c2", 2, 0)}},
		neutral(), config(estimate(gibibyte)), nil},
	{"with the key one run per worktree still holds",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c1", 2, 0)}},
		neutral(), config(estimate(gibibyte)), nil},
	{"with the key the limit of runs still holds",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 90), running("B", "c2", 2, 60), running("C", "c3", 3, 30), waiting("D", "c4", 4, 0)}},
		neutral(vm(100 * gibibyte)), config(estimate(gibibyte)), nil},
	{"with the key a run without history that does not fit lets a fitting run behind it pass",
		scheduler.State{Runs: []scheduler.Run{peaked(running("A", "c1", 1, 600), 8*gibibyte), waiting("B", "c2", 2, 0), waiting("C", "c3", 3, gibibyte)}},
		neutral(vm(8 * gibibyte)), config(estimate(8 * gibibyte)), start("C", 18)},
	{"beside runs of other users alone the estimate counts too",
		scheduler.State{Runs: []scheduler.Run{waiting("B", "c2", 1, 0)}},
		neutral(others(1), vm(6*gibibyte-1)), config(estimate(4 * gibibyte)), nil},
	{"alone on the machine a run without history starts unchecked, with or without the key",
		scheduler.State{Runs: []scheduler.Run{waiting("B", "c2", 1, 0)}},
		neutral(vm(gibibyte)), config(estimate(4 * gibibyte)), start("B", 18)},
}

func TestDecideAdmitsRunsWithoutHistoryOnAnEstimate(t *testing.T) {
	for _, current := range estimateCases {
		t.Run(current.name, func(t *testing.T) {
			got := scheduler.Decide(current.state, current.m, current.cfg, now)

			if !reflect.DeepEqual(got, current.want) {
				t.Fatalf("Decide = %+v, want %+v", got, current.want)
			}
		})
	}
}
