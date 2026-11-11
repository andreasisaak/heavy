package scheduler_test

import (
	"reflect"
	"testing"

	"github.com/andreasisaak/heavy/internal/scheduler"
)

// V2 (2026-11-08): a run beside which nothing is active always starts with all cores; waiting would free nothing,
// since heavy itself runs nothing (the Mac swapped 83 MiB per 2 s with OrbStack awake)
func TestALoneRunStartsWhateverTheMachineLooksLike(t *testing.T) {
	alone := scheduler.State{Runs: []scheduler.Run{waiting("A", "c1", 1, 0)}}
	for name, m := range map[string]scheduler.Measurements{
		"swap out":         neutral(swap(100 << 20)),
		"load per core":    neutral(load(2 * 18)),
		"host memory":      neutral(host(5)),
		"memory pressure":  neutral(vm(gibibyte), pressure(seconds(30))),
		"large prediction": neutral(vm(2 * gibibyte)),
	} {
		t.Run(name, func(t *testing.T) {
			state := alone
			if name == "large prediction" {
				state = scheduler.State{Runs: []scheduler.Run{waiting("A", "c1", 1, 8*gibibyte)}}
			}

			got := scheduler.Decide(state, m, config(), now)

			want := []scheduler.Action{{Kind: scheduler.Start, ID: "A", Cores: 18}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Decide = %+v, want %+v", got, want)
			}
		})
	}
}

// V2: the conditions still hold back a second run
func TestASecondRunStillWaitsForSwapToCalmDown(t *testing.T) {
	state := scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c2", 2, gibibyte)}}

	if got := scheduler.Decide(state, neutral(swap(100<<20)), config(), now); got != nil {
		t.Fatalf("Decide = %+v, want nothing", got)
	}
}
