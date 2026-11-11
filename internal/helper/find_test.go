package helper_test

import (
	"reflect"
	"testing"

	"github.com/andreasisaak/heavy/internal/helper"
)

// findFixture holds three processes, two of them with the tag r1, one with r10, which only starts like it, and the
// entries of /proc that are no processes.
func findFixture(t *testing.T) string {
	t.Helper()
	return fixture(t, merge(hostFiles(),
		process("101", []string{"PATH=/bin", "HEAVY_RUN=r1", "HEAVY_CPUS=4"}, "2048", "0::/docker/"+containerA+"\n"),
		process("57", []string{"HEAVY_RUN=r1"}, "1024", "0::/\n"),
		process("230", []string{"HEAVY_RUN=r10"}, "4096", "0::/docker/"+containerA+"\n"),
		process("231", []string{"PATH=/bin", "NOT_HEAVY_RUN=r1"}, "512", "0::/docker/"+containerB+"\n"),
		map[string]string{"self/status": "Name:\thelper\n", "sys/kernel/pid_max": "4194304\n"},
	))
}

// plan 3.1: fixture /proc with three processes, two with the tag: find gives exactly these two with their RSS
func TestFindGivesTheTaggedProcessesWithTheirMemory(t *testing.T) {
	root := findFixture(t)

	response := ask(t, helper.Options{Root: root, PageSize: 4096}, `{"type":"find","tag":"r1"}`)

	want := helper.Response{Found: &helper.Found{Processes: []helper.Process{
		{PID: 57, RSSBytes: 1024 * 1024, Container: ""},
		{PID: 101, RSSBytes: 2048 * 1024, Container: containerA},
	}}}
	if !reflect.DeepEqual(response, want) {
		t.Fatalf("find = %#v, want %#v", response, want)
	}
}

func TestFindOfATagNobodyCarriesGivesAnEmptyList(t *testing.T) {
	root := findFixture(t)

	line := askRaw(t, helper.Options{Root: root, PageSize: 4096}, `{"type":"find","tag":"r2"}`)

	if want := `{"found":{"processes":[]}}`; line != want {
		t.Fatalf("find line = %s, want %s", line, want)
	}
}

// a kernel thread has no VmRSS line; it counts with 0 bytes
func TestFindCountsAProcessWithoutResidentMemoryAsZero(t *testing.T) {
	root := fixture(t, merge(hostFiles(), process("12", []string{"HEAVY_RUN=r1"}, "", "0::/\n")))

	response := ask(t, helper.Options{Root: root, PageSize: 4096}, `{"type":"find","tag":"r1"}`)

	want := helper.Response{Found: &helper.Found{Processes: []helper.Process{{PID: 12, RSSBytes: 0, Container: ""}}}}
	if !reflect.DeepEqual(response, want) {
		t.Fatalf("find = %#v, want %#v", response, want)
	}
}

// a process that ends between the listing of /proc and the reading of its files is left out
func TestFindLeavesOutAProcessThatEndedMeanwhile(t *testing.T) {
	files := merge(hostFiles(), process("101", []string{"HEAVY_RUN=r1"}, "2048", "0::/\n"), process("102", []string{"HEAVY_RUN=r1"}, "2048", "0::/\n"))
	delete(files, "102/status")
	root := fixture(t, files)

	response := ask(t, helper.Options{Root: root, PageSize: 4096}, `{"type":"find","tag":"r1"}`)

	want := helper.Response{Found: &helper.Found{Processes: []helper.Process{{PID: 101, RSSBytes: 2048 * 1024, Container: ""}}}}
	if !reflect.DeepEqual(response, want) {
		t.Fatalf("find = %#v, want %#v", response, want)
	}
}

func TestFindWithoutATagIsAnError(t *testing.T) {
	root := findFixture(t)

	response := ask(t, helper.Options{Root: root, PageSize: 4096}, `{"type":"find"}`)

	if want := (helper.Response{Error: "heavy: find needs a tag"}); !reflect.DeepEqual(response, want) {
		t.Fatalf("find = %#v, want %#v", response, want)
	}
}
