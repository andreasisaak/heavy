package helper_test

import (
	"os"
	"reflect"
	"testing"

	"github.com/andreasisaak/heavy/internal/helper"
)

// plan 3.1: containers sums the resident memory of the processes per container id; processes outside containers
// count for none
func TestContainersSumsTheMemoryOfTheirProcesses(t *testing.T) {
	root := fixture(t, merge(hostFiles(),
		process("101", []string{"HEAVY_RUN=r1"}, "2048", "0::/docker/"+containerA+"\n"),
		process("102", nil, "1024", "0::/docker/"+containerA+"\n"),
		process("230", nil, "4096", "0::/system.slice/docker-"+containerB+".scope\n"),
		process("1", nil, "8192", "0::/init.scope\n"),
		process("2", nil, "", "0::/\n"),
	))

	response := ask(t, helper.Options{Root: root, PageSize: 4096}, `{"type":"containers"}`)

	want := helper.Response{Usage: &helper.Usage{Containers: []helper.Container{
		{ID: containerA, RSSBytes: 3072 * 1024},
		{ID: containerB, RSSBytes: 4096 * 1024},
	}}}
	if !reflect.DeepEqual(response, want) {
		t.Fatalf("containers = %#v, want %#v", response, want)
	}
}

// cgroup v1 lists one line per hierarchy; the id comes from any of them
func TestContainersReadsCgroupVersionOneLines(t *testing.T) {
	cgroup := "12:memory:/docker/" + containerB + "\n11:cpu,cpuacct:/docker/" + containerB + "\n1:name=systemd:/docker/" + containerB + "\n"
	root := fixture(t, merge(hostFiles(), process("300", nil, "100", cgroup)))

	response := ask(t, helper.Options{Root: root, PageSize: 4096}, `{"type":"containers"}`)

	want := helper.Response{Usage: &helper.Usage{Containers: []helper.Container{{ID: containerB, RSSBytes: 100 * 1024}}}}
	if !reflect.DeepEqual(response, want) {
		t.Fatalf("containers = %#v, want %#v", response, want)
	}
}

func TestContainersWithoutAnyGivesAnEmptyList(t *testing.T) {
	root := fixture(t, merge(hostFiles(), process("1", nil, "8192", "0::/init.scope\n")))

	line := askRaw(t, helper.Options{Root: root, PageSize: 4096}, `{"type":"containers"}`)

	if want := `{"usage":{"containers":[]}}`; line != want {
		t.Fatalf("containers line = %s, want %s", line, want)
	}
}

// plan 3.1, probe 2026-11-08: in OrbStack (cgroup v2, cgroupfs) a container of the helper with --pid=host and its
// own cgroup namespace sees another container's process at 0::/../<id>
func TestContainersReadsTheCgroupPathOrbStackShows(t *testing.T) {
	recorded, err := os.ReadFile("testdata/orbstack-cgroup.txt")
	if err != nil {
		t.Fatal(err)
	}
	const id = "03d3c832f18e7824ae5be8d0dbfd798f1f78104fedd178cb9ee0e5f7c73ded8a"
	root := fixture(t, merge(hostFiles(), process("3633321", []string{"HEAVY_RUN=probe"}, "896", string(recorded))))

	usage := ask(t, helper.Options{Root: root, PageSize: 4096}, `{"type":"containers"}`)
	found := ask(t, helper.Options{Root: root, PageSize: 4096}, `{"type":"find","tag":"probe"}`)

	wantUsage := helper.Response{Usage: &helper.Usage{Containers: []helper.Container{{ID: id, RSSBytes: 896 * 1024}}}}
	wantFound := helper.Response{Found: &helper.Found{Processes: []helper.Process{{PID: 3633321, RSSBytes: 896 * 1024, Container: id}}}}
	if !reflect.DeepEqual(usage, wantUsage) || !reflect.DeepEqual(found, wantFound) {
		t.Fatalf("containers = %#v, find = %#v; want %#v and %#v", usage, found, wantUsage, wantFound)
	}
}
