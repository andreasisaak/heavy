package helper_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/andreasisaak/heavy/internal/helper"
)

// plan 3.1: a sample from the fixture /proc gives the exact values
func TestSampleReadsTheMachineWideFiles(t *testing.T) {
	root := fixture(t, hostFiles())

	response := ask(t, helper.Options{Root: root, PageSize: 4096}, `{"type":"sample"}`)

	want := helper.Response{Sample: &helper.Sample{
		MemAvailableBytes: 3145728 * 1024, SwapOutBytes: 2500 * 4096, Load1: 3.52, Cores: 4,
		Pressure: &helper.Pressure{
			Memory: helper.Stall{Some: 1.5, Full: 0.75}, CPU: helper.Stall{Some: 12}, IO: helper.Stall{Some: 0.25, Full: 0.1},
		},
	}}
	if !reflect.DeepEqual(response, want) {
		t.Fatalf("sample = %#v, want %#v", response, want)
	}
}

// plan 3.1: without /proc/pressure the pressure stays empty and the sample is no error
func TestSampleWithoutPressureLeavesItOut(t *testing.T) {
	files := hostFiles()
	for _, name := range []string{"pressure/memory", "pressure/cpu", "pressure/io"} {
		delete(files, name)
	}
	root := fixture(t, files)

	line := askRaw(t, helper.Options{Root: root, PageSize: 16384}, `{"type":"sample"}`)

	want := `{"sample":{"mem_available_bytes":3221225472,"swap_out_bytes":40960000,"load1":3.52,"cores":4}}`
	if line != want {
		t.Fatalf("sample line = %s, want %s", line, want)
	}
}

func TestSampleWithoutMeminfoIsAnError(t *testing.T) {
	files := hostFiles()
	delete(files, "meminfo")
	root := fixture(t, files)

	response := ask(t, helper.Options{Root: root, PageSize: 4096}, `{"type":"sample"}`)

	want := helper.Response{Error: "heavy: reading " + filepath.Join(root, "meminfo") + ": open " + filepath.Join(root, "meminfo") + ": no such file or directory"}
	if !reflect.DeepEqual(response, want) {
		t.Fatalf("sample = %#v, want %#v", response, want)
	}
}

func TestSampleWithoutMemAvailableNamesTheMissingLine(t *testing.T) {
	files := hostFiles()
	files["meminfo"] = "MemTotal:       25165824 kB\n"
	root := fixture(t, files)

	response := ask(t, helper.Options{Root: root, PageSize: 4096}, `{"type":"sample"}`)

	want := helper.Response{Error: "heavy: " + filepath.Join(root, "meminfo") + ": expected a line MemAvailable: <n> kB, found none"}
	if !reflect.DeepEqual(response, want) {
		t.Fatalf("sample = %#v, want %#v", response, want)
	}
}

func TestSampleWithAGarbledLoadIsAnError(t *testing.T) {
	files := hostFiles()
	files["loadavg"] = "high 2.10 1.75 4/512 98765\n"
	root := fixture(t, files)

	response := ask(t, helper.Options{Root: root, PageSize: 4096}, `{"type":"sample"}`)

	want := helper.Response{Error: "heavy: " + filepath.Join(root, "loadavg") + `: expected the load of one minute first, got "high 2.10 1.75 4/512 98765\n"`}
	if !reflect.DeepEqual(response, want) {
		t.Fatalf("sample = %#v, want %#v", response, want)
	}
}

func TestSampleWithAPressureFileMissingIsAnError(t *testing.T) {
	files := hostFiles()
	delete(files, "pressure/io")
	root := fixture(t, files)

	response := ask(t, helper.Options{Root: root, PageSize: 4096}, `{"type":"sample"}`)

	path := filepath.Join(root, "pressure", "io")
	want := helper.Response{Error: "heavy: reading " + path + ": open " + path + ": no such file or directory"}
	if !reflect.DeepEqual(response, want) {
		t.Fatalf("sample = %#v, want %#v", response, want)
	}
}
