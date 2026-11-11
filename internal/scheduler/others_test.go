package scheduler_test

import (
	"reflect"
	"testing"

	"github.com/andreasisaak/heavy/internal/scheduler"
)

func others(count int) func(*scheduler.Measurements) {
	return func(m *scheduler.Measurements) { m.OtherActive = count }
}

// The runs of the other users of the machine count as load: a run starts unchecked only when no run is active on
// the whole machine.
var otherCases = []struct {
	name  string
	state scheduler.State
	m     scheduler.Measurements
	want  []scheduler.Action
}{
	{"beside runs of others a run without history starts on a calm machine, with the cores the load leaves",
		scheduler.State{Runs: []scheduler.Run{waiting("A", "c1", 1, 0)}}, neutral(others(2), load(6.8)),
		[]scheduler.Action{{Kind: scheduler.Start, ID: "A", Cores: 11}}},
	{"beside runs of others a run waits while the load per core is over the threshold",
		scheduler.State{Runs: []scheduler.Run{waiting("A", "c1", 1, 0)}}, neutral(others(1), load(14.4)), nil},
	{"beside runs of others a run starts right below the load threshold",
		scheduler.State{Runs: []scheduler.Run{waiting("A", "c1", 1, 0)}}, neutral(others(1), load(14.3)),
		[]scheduler.Action{{Kind: scheduler.Start, ID: "A", Cores: 3}}},
	{"beside runs of others a run waits while the reserve would not stay free",
		scheduler.State{Runs: []scheduler.Run{waiting("A", "c1", 1, 3*gibibyte)}}, neutral(others(1), vm(4*gibibyte+gibibyte/2)), nil},
	{"beside runs of others a run without history needs the reserve alone",
		scheduler.State{Runs: []scheduler.Run{waiting("A", "c1", 1, 0)}}, neutral(others(1), vm(2*gibibyte-1)), nil},
	{"beside runs of others a run waits while the machine swaps",
		scheduler.State{Runs: []scheduler.Run{waiting("A", "c1", 1, 0)}}, neutral(others(1), swap(4<<20)), nil},
	{"beside runs of others a run waits while the host has too little free",
		scheduler.State{Runs: []scheduler.Run{waiting("A", "c1", 1, 0)}}, neutral(others(1), host(19)), nil},
	{"an exclusive run waits for a machine without runs",
		scheduler.State{Runs: []scheduler.Run{exclusive(waiting("A", "c1", 1, gibibyte))}}, neutral(others(1)), nil},
	{"an exclusive run starts on a machine without runs",
		scheduler.State{Runs: []scheduler.Run{exclusive(waiting("A", "c1", 1, gibibyte))}}, neutral(others(0), load(17)),
		[]scheduler.Action{{Kind: scheduler.Start, ID: "A", Cores: 18}}},
	{"beside an own run a run without history still waits",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c2", 2, 0)}}, neutral(others(1)), nil},
	{"under pressure a lone run beside runs of others waits for the reserve",
		scheduler.State{Runs: []scheduler.Run{waiting("A", "c1", 1, 0)}}, neutral(others(1), vm(gibibyte), pressure(seconds(10))), nil},
}

func TestDecideCountsTheRunsOfOtherUsers(t *testing.T) {
	for _, current := range otherCases {
		t.Run(current.name, func(t *testing.T) {
			got := scheduler.Decide(current.state, current.m, config(), now)

			if !reflect.DeepEqual(got, current.want) {
				t.Fatalf("Decide = %+v, want %+v", got, current.want)
			}
		})
	}
}
