package scheduler_test

import (
	"reflect"
	"testing"

	"github.com/andreasisaak/heavy/internal/scheduler"
)

func known(run scheduler.Run, bytes int64) scheduler.Run {
	return with(run, func(r *scheduler.Run) { r.Predicted, r.PredictedBytes = true, bytes })
}

// Holds names for every waiting run what keeps it from starting now, in the words `heavy --status` shows; a run
// that starts with this decision has no entry.
var holdCases = []struct {
	name  string
	state scheduler.State
	m     scheduler.Measurements
	cfg   scheduler.Config
	want  map[string]string
}{
	{"an idle queue holds no run: the first starts, the next waits for the next decision",
		scheduler.State{Runs: []scheduler.Run{waiting("A", "c1", 1, 0), waiting("B", "c2", 2, gibibyte)}}, neutral(), config(),
		map[string]string{"B": "after run A, which starts now"}},
	{"memory, with the numbers: the peak of the history, the reserve and what is free",
		scheduler.State{Runs: []scheduler.Run{known(running("A", "c1", 1, 60), gibibyte), waiting("B", "c2", 2, 9*gibibyte)}}, neutral(), config(),
		map[string]string{"B": "memory: needs 9.0 GiB and 2.0 GiB reserve, 10.0 GiB are free"}},
	{"memory on an estimate, with what the running runs without history may still take",
		scheduler.State{Runs: []scheduler.Run{peaked(running("A", "c1", 1, 2), gibibyte), waiting("B", "c2", 2, 0)}},
		neutral(vm(56 * gibibyte)), config(estimate(28 * gibibyte)),
		map[string]string{"B": "memory: needs 28.0 GiB (estimate) and 2.0 GiB reserve, 29.0 GiB are free: 56.0 GiB less 27.0 GiB the running runs without history may still take"}},
	{"without history and without an estimate a run waits for an idle queue",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c2", 2, 0)}}, neutral(), config(),
		map[string]string{"B": "no history: starts when no run is active; unpredicted_peak_bytes in the personal list lets it start beside others"}},
	{"load",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c2", 2, gibibyte)}}, neutral(load(15.25)), config(),
		map[string]string{"B": "load: 15.2 on 18 cores, starts below 14.4"}},
	{"memory of the host",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c2", 2, gibibyte)}}, neutral(host(12)), config(),
		map[string]string{"B": "host memory: 12 % free, starts at 20 %"}},
	{"swap",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c2", 2, gibibyte)}}, neutral(swap(6 << 20)), config(),
		map[string]string{"B": "swap: 6.0 MiB written per 2 s, starts below 4.0 MiB"}},
	{"several thresholds at once",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c2", 2, 9*gibibyte)}}, neutral(load(18), host(12)), config(),
		map[string]string{"B": "memory: needs 9.0 GiB and 2.0 GiB reserve, 10.0 GiB are free; host memory: 12 % free, starts at 20 %; load: 18.0 on 18 cores, starts below 14.4"}},
	{"the same checkout",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c1", 2, gibibyte)}}, neutral(), config(),
		map[string]string{"B": "same worktree as run A"}},
	{"max_parallel",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), running("B", "c2", 2, 50), running("C", "c3", 3, 40), waiting("D", "c4", 4, gibibyte)}}, neutral(), config(),
		map[string]string{"D": "max_parallel: 3 of 3 runs are active"}},
	{"an exclusive run is active",
		scheduler.State{Runs: []scheduler.Run{exclusive(running("A", "c1", 1, 60)), waiting("B", "c2", 2, gibibyte)}}, neutral(), config(),
		map[string]string{"B": "run A is exclusive"}},
	{"an exclusive run waits for a machine without runs",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), exclusive(waiting("B", "c2", 2, gibibyte))}}, neutral(), config(),
		map[string]string{"B": "exclusive: starts when no run is active"}},
	{"a run paused for memory holds every start",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 600), paused("P", "c3", 2, 300, 100, scheduler.PausedForMemory), waiting("B", "c2", 3, gibibyte)}}, neutral(), config(),
		map[string]string{"B": "run P is paused for memory"}},
	{"memory pressure holds every start beside an active run",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 600), waiting("B", "c2", 3, gibibyte)}}, neutral(pressure(seconds(10))), config(),
		map[string]string{"B": "memory pressure: no run starts beside others until memory has recovered"}},
	{"a run that has waited longer than the backfill time for room holds the runs behind it",
		scheduler.State{Runs: []scheduler.Run{known(running("A", "c1", 1, 60), gibibyte),
			with(waiting("B", "c2", 2, 9*gibibyte), func(r *scheduler.Run) { r.Arrived = ago(901) }), waiting("C", "c3", 3, gibibyte)}}, neutral(), config(),
		map[string]string{"B": "memory: needs 9.0 GiB and 2.0 GiB reserve, 10.0 GiB are free", "C": "behind run B, which has waited 15m0s for room"}},
	{"a run that fits passes one that does not, and the others wait for the next decision",
		scheduler.State{Runs: []scheduler.Run{known(running("A", "c1", 1, 60), gibibyte), waiting("B", "c2", 2, 9*gibibyte), waiting("C", "c3", 3, gibibyte), waiting("D", "c4", 4, gibibyte)}}, neutral(), config(),
		map[string]string{"B": "memory: needs 9.0 GiB and 2.0 GiB reserve, 10.0 GiB are free", "D": "after run C, which starts now"}},
	{"beside the runs of other users alone a run without history starts on the reserve",
		scheduler.State{Runs: []scheduler.Run{waiting("B", "c2", 2, 0)}}, neutral(others(2), vm(gibibyte)), config(),
		map[string]string{"B": "memory: needs 2.0 GiB reserve, 1.0 GiB are free"}},
}

func TestHoldsNameWhatKeepsEachWaitingRun(t *testing.T) {
	for _, current := range holdCases {
		t.Run(current.name, func(t *testing.T) {
			got := scheduler.Holds(current.state, current.m, current.cfg, now)

			if !reflect.DeepEqual(got, current.want) {
				t.Fatalf("Holds = %q\nwant    %q", got, current.want)
			}
		})
	}
}

// The reasons and the decision come from the same rules: in every case of the decision tests, a run has no hold
// exactly when the decision starts it.
func TestARunHasNoHoldExactlyWhenTheDecisionStartsIt(t *testing.T) {
	type scene struct {
		name  string
		state scheduler.State
		m     scheduler.Measurements
		cfg   scheduler.Config
	}
	var scenes []scene
	for _, current := range append(decideCases, estimateCases...) {
		scenes = append(scenes, scene{current.name, current.state, current.m, current.cfg})
	}
	for _, current := range holdCases {
		scenes = append(scenes, scene{current.name, current.state, current.m, current.cfg})
	}
	for _, current := range scenes {
		t.Run(current.name, func(t *testing.T) {
			started := map[string]bool{}
			for _, action := range scheduler.Decide(current.state, current.m, current.cfg, now) {
				started[action.ID] = action.Kind == scheduler.Start
			}
			holds := scheduler.Holds(current.state, current.m, current.cfg, now)

			for _, run := range current.state.Runs {
				_, held := holds[run.ID]
				if waits := run.Status == scheduler.Waiting; waits && held == started[run.ID] || !waits && held {
					t.Fatalf("run %s: started %v, hold %q; all holds %q", run.ID, started[run.ID], holds[run.ID], holds)
				}
			}
		})
	}
}
