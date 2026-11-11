package foreign_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/foreign"
	"github.com/andreasisaak/heavy/internal/testkit"
)

// psFormat is the format the docker ps call has to ask for, as the plan names it.
const psFormat = `{{.ID}}\t{{.Names}}\t{{.Label "com.docker.compose.service"}}\t{{.Label "com.docker.compose.project.working_dir"}}`

// fakeDocker writes a docker that records its arguments one per line and then runs answer; it starts the program once,
// because macOS checks a new executable on its first start.
func fakeDocker(t *testing.T, answer string) (program, arguments string) {
	t.Helper()
	folder := t.TempDir()
	arguments = filepath.Join(folder, "arguments")
	script := `[ "$1" = warm-up ] && exit 0
printf '%s\n' "$@" > ` + arguments + `
` + answer
	program = testkit.FakeProgram(t, folder, "docker", script)
	_ = exec.Command(program, "warm-up").Run()
	return program, arguments
}

func recordedArguments(t *testing.T, path string) []string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the arguments of the fake docker: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
}

func TestListParsesTheContainersOfDockerPs(t *testing.T) {
	answer := `printf '%s\t%s\t%s\t%s\n' ` + identifier('a') + ` shopfront-spa-watch-1 spa-watch /sites/shopfront-csv-export ` +
		identifier('b') + ` lonely '' ''`
	program, arguments := fakeDocker(t, answer)

	containers, err := foreign.List(program, 2*time.Second)

	want := []foreign.Container{
		{ID: identifier('a'), Name: "shopfront-spa-watch-1", Service: "spa-watch", WorkingDirectory: "/sites/shopfront-csv-export"},
		{ID: identifier('b'), Name: "lonely"},
	}
	if err != nil || !reflect.DeepEqual(containers, want) {
		t.Fatalf("List = %#v, %v; want %#v", containers, err, want)
	}
	if got, want := recordedArguments(t, arguments), []string{"ps", "--no-trunc", "--format", psFormat}; !reflect.DeepEqual(got, want) {
		t.Fatalf("docker got %q, want %q", got, want)
	}
}

func TestListOfNoContainersIsEmpty(t *testing.T) {
	program, _ := fakeDocker(t, `true`)

	containers, err := foreign.List(program, 2*time.Second)

	if err != nil || !reflect.DeepEqual(containers, []foreign.Container{}) {
		t.Fatalf("List = %#v, %v; want an empty list", containers, err)
	}
}

func TestListNamesALineWithoutFourFields(t *testing.T) {
	program, _ := fakeDocker(t, `printf 'abc\tname\tservice\n'`)

	containers, err := foreign.List(program, 2*time.Second)

	want := `heavy: docker ps printed "abc\tname\tservice", expected 4 tab-separated fields (id, name, compose service, compose working directory), got 3`
	if containers != nil || err == nil || err.Error() != want {
		t.Fatalf("List = %#v, %v; want the error %s", containers, err, want)
	}
}

func TestFailingDockerPsGivesItsErrorAndTheUnattributedRecord(t *testing.T) {
	program, _ := fakeDocker(t, `echo "Cannot connect to the Docker daemon" >&2; exit 1`)
	log, records := logger()

	containers, err := foreign.List(program, 2*time.Second)
	if err == nil {
		t.Fatalf("List = %#v without an error, want the failure of docker ps", containers)
	}
	foreign.LogUnattributed(log, err)

	want := "heavy: docker ps failed: exit status 1: Cannot connect to the Docker daemon"
	if containers != nil || err.Error() != want {
		t.Fatalf("List = %#v, %v; want the error %s", containers, err, want)
	}
	record := `{"level":"WARN","msg":"foreign load not attributed","error":"` + want + `"}` + "\n"
	if records.String() != record {
		t.Fatalf("logged %q, want %q", records.String(), record)
	}
}
