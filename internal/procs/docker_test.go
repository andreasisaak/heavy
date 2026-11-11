package procs_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/procs"
	"github.com/andreasisaak/heavy/internal/testkit"
)

// dockerProgram writes a fake docker that logs its calls and answers ps and inspect with the given output.
func dockerProgram(t *testing.T, ps, inspect string) (*procs.SystemDocker, string) {
	t.Helper()
	folder := t.TempDir()
	log := filepath.Join(folder, "calls.log")
	script := `[ "$1" = warm-up ] && exit 0
printf '%s\n' "$*" >> ` + log + `
case "$1" in
ps) ` + ps + ` ;;
inspect) ` + inspect + ` ;;
pause) if [ "$2" = gone ]; then echo "Error response from daemon: No such container: gone" >&2; exit 1; fi ;;
esac`
	program := testkit.FakeProgram(t, folder, "docker", script)
	_ = exec.Command(program, "warm-up").Run()
	return procs.NewSystemDocker(program, 2*time.Second, 30*time.Second, func() time.Time { return listedAt }), log
}

// listedAt is the time of every listing in the tests that do not move the clock.
var listedAt = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func calls(t *testing.T, log string) []string {
	t.Helper()
	content, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
}

const psFormat = `{{.ID}}{{"\t"}}{{.State}}`

const inspectFormat = `{{.Id}}{{"\t"}}{{.Name}}{{"\t"}}{{index .Config.Labels "com.docker.compose.project.working_dir"}}{{"\t"}}{{.State.StartedAt}}{{"\t"}}{{.State.Paused}}`

func TestSystemDockerListsContainersWithWorkingDirectoryAndStart(t *testing.T) {
	inspect := `printf 'c1\t/shop-db-test-1\t/work/shop\t2026-11-08T00:59:01.123456789Z\tfalse\nc2\t/lonely\t<no value>\t2026-11-07T10:00:00Z\ttrue\n'`
	docker, log := dockerProgram(t, `printf 'c1\trunning\nc2\tpaused\n'`, inspect)

	containers, err := docker.Containers()

	want := []procs.Container{
		{ID: "c1", Name: "shop-db-test-1", WorkingDirectory: "/work/shop", Started: time.Date(2026, 11, 8, 0, 59, 1, 123456789, time.UTC)},
		{ID: "c2", Name: "lonely", WorkingDirectory: "", Started: time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC), Paused: true},
	}
	if err != nil || !reflect.DeepEqual(containers, want) {
		t.Fatalf("Containers = %#v, %v; want %#v", containers, err, want)
	}
	if got, want := calls(t, log), []string{"ps --no-trunc --format " + psFormat, "inspect --format " + inspectFormat + " c1 c2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("docker calls = %q, want %q", got, want)
	}
}

func TestSystemDockerWithoutContainersAsksNoInspect(t *testing.T) {
	docker, log := dockerProgram(t, `true`, `exit 1`)

	containers, err := docker.Containers()

	if err != nil || !reflect.DeepEqual(containers, []procs.Container{}) || !reflect.DeepEqual(calls(t, log), []string{"ps --no-trunc --format " + psFormat}) {
		t.Fatalf("Containers = %#v, %v, calls %q", containers, err, calls(t, log))
	}
}

func TestSystemDockerNamesAFailingList(t *testing.T) {
	docker, _ := dockerProgram(t, `echo "Cannot connect to the Docker daemon" >&2; exit 1`, `true`)

	_, err := docker.Containers()

	if want := "heavy: docker ps failed: exit status 1: Cannot connect to the Docker daemon"; err == nil || err.Error() != want {
		t.Fatalf("Containers error = %v, want %s", err, want)
	}
}

func TestSystemDockerRefusesAGarbledInspectLine(t *testing.T) {
	docker, _ := dockerProgram(t, `printf 'c1\trunning\n'`, `printf 'c1\t/db\t/work\tyesterday\tfalse\n'`)

	_, err := docker.Containers()

	want := `heavy: docker inspect printed "c1\t/db\t/work\tyesterday\tfalse", expected id, name, working directory, start and paused: parsing time "yesterday" as "2006-01-02T15:04:05.999999999Z07:00": cannot parse "yesterday" as "2006"`
	if err == nil || err.Error() != want {
		t.Fatalf("Containers error = %v, want %s", err, want)
	}
}

func TestSystemDockerPausesAndUnpausesByName(t *testing.T) {
	docker, log := dockerProgram(t, `true`, `true`)

	pauseError, unpauseError, goneError := docker.Pause("db-test-1"), docker.Unpause("db-test-1"), docker.Pause("gone")

	if pauseError != nil || unpauseError != nil {
		t.Fatalf("Pause = %v, Unpause = %v", pauseError, unpauseError)
	}
	if want := "heavy: docker pause gone failed: exit status 1: Error response from daemon: No such container: gone"; goneError == nil || goneError.Error() != want {
		t.Fatalf("Pause of a gone container = %v, want %s", goneError, want)
	}
	if got, want := calls(t, log), []string{"pause db-test-1", "unpause db-test-1", "pause gone"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("docker calls = %q, want %q", got, want)
	}
}

// probe 2026-11-08: a container that ends between docker ps and docker inspect fails the inspect; the list is taken
// again once
func TestSystemDockerListsAgainWhenAContainerEndsMeanwhile(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "inspected")
	inspect := `if [ -f ` + marker + ` ]; then printf 'c1\t/db\t/work\t2026-11-08T00:59:01Z\tfalse\n'; else touch ` + marker + `; echo "error: no such object: c9" >&2; exit 1; fi`
	docker, log := dockerProgram(t, `printf 'c1\trunning\nc9\trunning\n'`, inspect)

	containers, err := docker.Containers()

	want := []procs.Container{{ID: "c1", Name: "db", WorkingDirectory: "/work", Started: time.Date(2026, 11, 8, 0, 59, 1, 0, time.UTC)}}
	if err != nil || !reflect.DeepEqual(containers, want) || len(calls(t, log)) != 4 {
		t.Fatalf("Containers = %#v, %v, calls %q; want the second list", containers, err, calls(t, log))
	}
}

// steppingDocker is a fake docker whose ps prints the file ps, so a test changes the containers between two listings,
// and whose inspect answers for each id it gets from the file inspect-<id>.
func steppingDocker(t *testing.T, clock *time.Time) (*procs.SystemDocker, string, string) {
	t.Helper()
	folder := t.TempDir()
	log := filepath.Join(folder, "calls.log")
	script := `[ "$1" = warm-up ] && exit 0
printf '%s\n' "$*" >> ` + log + `
case "$1" in
ps) cat ` + filepath.Join(folder, "ps") + ` ;;
inspect) shift 3; for id in "$@"; do cat ` + folder + `/inspect-"$id"; done ;;
esac`
	program := testkit.FakeProgram(t, folder, "docker", script)
	_ = exec.Command(program, "warm-up").Run()
	for id, line := range map[string]string{
		"c1": "c1\t/db\t/work\t2026-11-09T10:00:00Z\tfalse\n",
		"c2": "c2\t/app\t/work\t2026-11-09T10:01:00Z\tfalse\n",
		"c3": "c3\t/spa\t/work\t2026-11-09T11:59:00Z\tfalse\n",
	} {
		if err := os.WriteFile(filepath.Join(folder, "inspect-"+id), []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return procs.NewSystemDocker(program, 2*time.Second, 30*time.Second, func() time.Time { return *clock }), folder, log
}

func listing(t *testing.T, folder, ps string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(folder, "ps"), []byte(ps), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 2026-11-09: under load 30, docker inspect over the 70 containers of the machine took longer than 5 s 60 times in
// a night, and the run's memory in the VM went unmeasured; a listing inspects only the containers it has not seen
func TestSystemDockerInspectsOnlyTheContainersItHasNotSeen(t *testing.T) {
	clock := listedAt
	docker, folder, log := steppingDocker(t, &clock)
	listing(t, folder, "c1\trunning\nc2\trunning\n")
	if _, err := docker.Containers(); err != nil {
		t.Fatal(err)
	}
	listing(t, folder, "c1\tpaused\nc3\trunning\n")
	clock = clock.Add(10 * time.Second)

	containers, err := docker.Containers()

	want := []procs.Container{
		{ID: "c1", Name: "db", WorkingDirectory: "/work", Started: time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC), Paused: true},
		{ID: "c3", Name: "spa", WorkingDirectory: "/work", Started: time.Date(2026, 11, 9, 11, 59, 0, 0, time.UTC)},
	}
	if err != nil || !reflect.DeepEqual(containers, want) {
		t.Fatalf("Containers = %#v, %v; want %#v", containers, err, want)
	}
	ps := "ps --no-trunc --format " + psFormat
	if got, want := calls(t, log), []string{ps, "inspect --format " + inspectFormat + " c1 c2", ps, "inspect --format " + inspectFormat + " c3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("docker calls = %q, want %q", got, want)
	}
}

// a container restarted under its id gets a new start; each container is inspected again after 30 s
func TestSystemDockerInspectsEveryContainerAgainAfterThirtySeconds(t *testing.T) {
	clock := listedAt
	docker, folder, log := steppingDocker(t, &clock)
	listing(t, folder, "c1\trunning\nc2\trunning\n")
	if _, err := docker.Containers(); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(30 * time.Second)

	if _, err := docker.Containers(); err != nil {
		t.Fatal(err)
	}

	ps := "ps --no-trunc --format " + psFormat
	if got, want := calls(t, log), []string{ps, "inspect --format " + inspectFormat + " c1 c2", ps, "inspect --format " + inspectFormat + " c1 c2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("docker calls = %q, want %q", got, want)
	}
}
