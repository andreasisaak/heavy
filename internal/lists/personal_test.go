package lists_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/testkit"
)

// test_heavy_lists.py:107
func TestPersonalListMapsResolvedMainCheckoutsToSwitches(t *testing.T) {
	home := testkit.Home(t)
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatalf("resolving %s: %v", home, err)
	}
	path := writeJSON(t, t.TempDir(), "config.json", object{"projects": object{
		"~/sites/local-dev/example": object{"enabled": false, "commands": []string{"make e2e", "npm run test"}},
		"/sites/only-commands": object{"commands": []any{
			"make check", object{"command": "make test-complete", "unless": []string{"TEST_WORKERS=12"}},
		}},
		"/sites/only-switch": object{"enabled": false},
	}})

	got := lists.LoadPersonal(path)

	want := lists.Personal{
		Projects: map[string]lists.Switch{
			resolvedHome + "/sites/local-dev/example": {Enabled: false, Commands: []lists.Entry{
				{Words: []string{"make", "e2e"}}, {Words: []string{"npm", "run", "test"}},
			}},
			"/sites/only-commands": {Enabled: true, Commands: []lists.Entry{
				{Words: []string{"make", "check"}}, {Words: []string{"make", "test-complete"}, Unless: []string{"TEST_WORKERS=12"}},
			}},
			"/sites/only-switch": {Enabled: false},
		},
		Load: lists.DefaultLoad(),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadPersonal = %#v; want %#v", got, want)
	}
}

// test_heavy_lists.py:175
func TestMissingPersonalListSwitchesNothing(t *testing.T) {
	got := lists.LoadPersonal(filepath.Join(t.TempDir(), "config.json"))

	want := lists.Personal{Projects: map[string]lists.Switch{}, Load: lists.DefaultLoad()}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadPersonal = %#v; want %#v", got, want)
	}
}

func TestDefaultLoadHoldsTheThresholdsOfTheDesign(t *testing.T) {
	want := lists.Load{
		MaxParallel:          3,
		ReserveBytes:         2 << 30,
		VMPressureBytes:      3 << 29,
		VMRecoveredBytes:     5 << 29,
		HostPressurePercent:  10,
		HostRecoveredPercent: 20,
		LoadPerCore:          0.8,
		SwapBytesPer2s:       4 << 20,
	}
	if got := lists.DefaultLoad(); got != want {
		t.Fatalf("DefaultLoad = %#v, want %#v", got, want)
	}
}

func TestPersonalLoadSectionOverridesSomeThresholds(t *testing.T) {
	path := writeJSON(t, t.TempDir(), "config.json", object{"load": object{
		"max_parallel": 2, "reserve_bytes": 0, "host_pressure_percent": 0, "host_recovered_percent": 0, "load_per_core": 1000,
	}})

	got := lists.LoadPersonal(path)

	load := lists.DefaultLoad()
	load.MaxParallel, load.ReserveBytes, load.HostPressurePercent, load.HostRecoveredPercent, load.LoadPerCore = 2, 0, 0, 0, 1000
	load.MaxParallelSet = true
	want := lists.Personal{Projects: map[string]lists.Switch{}, Load: load}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadPersonal = %#v; want %#v", got, want)
	}
}

// unpredicted_peak_bytes names the peak memory to expect of a run without history; without it there is none.
func TestPersonalLoadSectionNamesThePeakOfARunWithoutHistory(t *testing.T) {
	path := writeJSON(t, t.TempDir(), "config.json", object{"load": object{"unpredicted_peak_bytes": 8589934592}})

	got := lists.LoadPersonal(path)

	load := lists.DefaultLoad()
	load.UnpredictedPeakBytes = 8 << 30
	want := lists.Personal{Projects: map[string]lists.Switch{}, Load: load}
	if !reflect.DeepEqual(got, want) || lists.DefaultLoad().UnpredictedPeakBytes != 0 {
		t.Fatalf("LoadPersonal = %#v; want %#v, and no estimate by default", got, want)
	}
}

// A list that names no max_parallel leaves the limit to the machine configuration.
func TestPersonalLoadSectionWithoutMaxParallelSaysSo(t *testing.T) {
	path := writeJSON(t, t.TempDir(), "config.json", object{"load": object{"reserve_bytes": 5}})

	got := lists.LoadPersonal(path)

	load := lists.DefaultLoad()
	load.ReserveBytes = 5
	want := lists.Personal{Projects: map[string]lists.Switch{}, Load: load}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadPersonal = %#v; want %#v", got, want)
	}
}
