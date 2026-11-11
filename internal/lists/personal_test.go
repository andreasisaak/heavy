package lists_test

import (
	"encoding/json"
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

	got, err := lists.LoadPersonal(path)

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
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadPersonal = %#v, %v; want %#v", got, err, want)
	}
}

// test_heavy_lists.py:175
func TestMissingPersonalListSwitchesNothing(t *testing.T) {
	got, err := lists.LoadPersonal(filepath.Join(t.TempDir(), "config.json"))

	want := lists.Personal{Projects: map[string]lists.Switch{}, Load: lists.DefaultLoad()}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadPersonal = %#v, %v; want %#v", got, err, want)
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

	got, err := lists.LoadPersonal(path)

	load := lists.DefaultLoad()
	load.MaxParallel, load.ReserveBytes, load.HostPressurePercent, load.HostRecoveredPercent, load.LoadPerCore = 2, 0, 0, 0, 1000
	load.MaxParallelSet = true
	want := lists.Personal{Projects: map[string]lists.Switch{}, Load: load}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadPersonal = %#v, %v; want %#v", got, err, want)
	}
}

// A list that names no max_parallel leaves the limit to the machine configuration.
func TestPersonalLoadSectionWithoutMaxParallelSaysSo(t *testing.T) {
	path := writeJSON(t, t.TempDir(), "config.json", object{"load": object{"reserve_bytes": 5}})

	got, err := lists.LoadPersonal(path)

	load := lists.DefaultLoad()
	load.ReserveBytes = 5
	want := lists.Personal{Projects: map[string]lists.Switch{}, Load: load}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadPersonal = %#v, %v; want %#v", got, err, want)
	}
}

// Shapes of test_heavy_lists.py:179-188 and of plan task 1.4.
func TestPersonalListRejectsOtherShapes(t *testing.T) {
	cases := []struct {
		origin  string
		content any
	}{
		{"test_heavy_lists.py:182", object{"projects": []string{"/sites/main"}}},
		{"test_heavy_lists.py:183", object{"/sites/main": object{"commands": []string{"make ci"}}}},
		{"test_heavy_lists.py:184", object{"projects": object{"/sites/main": object{"commands": "make ci"}}}},
		{"test_heavy_lists.py:185", object{"projects": object{"/sites/main": object{"enabled": "no"}}}},
		{"test_heavy_lists.py:186", object{"projects": object{"/sites/main": object{"enable": false}}}},
		{"test_heavy_lists.py:187", object{"projects": object{"/sites/main": object{"commands": []any{
			object{"command": "checkall.sh", "unless": []string{"SECURITY CASES"}},
		}}}}},
		{"plan 1.4", object{"projects": nil}},
		{"plan 1.4", object{"load": object{"max_parallel": 0}}},
		{"plan 1.4", object{"load": object{"max_parallel": 2.5}}},
		{"plan 1.4", object{"load": object{"reserve_bytes": -1}}},
		{"plan 1.4", object{"load": object{"host_pressure_percent": 101}}},
		{"plan 1.4", object{"load": object{"load_per_core": -0.5}}},
		{"plan 1.4", object{"load": object{"swap": 1}}},
		{"plan 1.4", object{"load": nil}},
		{"plan 1.4", []string{}},
	}
	for _, current := range cases {
		path := writeJSON(t, t.TempDir(), "config.json", current.content)
		compact, _ := json.Marshal(current.content)

		_, err := lists.LoadPersonal(path)

		want := "heavy: " + path + ": expected " + lists.PersonalForm + ", got " + string(compact)
		if err == nil || err.Error() != want {
			t.Errorf("%s: LoadPersonal error = %v, want %q", current.origin, err, want)
		}
	}
}
