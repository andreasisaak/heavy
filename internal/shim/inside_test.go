package shim_test

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// FR-10, FR-11: inside an active run the shim passes straight on, and docker exec and run carry the run's id and
// cores into the container.
func TestDockerCallInsideARunCarriesTheRun(t *testing.T) {
	f := newFixture(t)
	active, _ := f.activeRun(t, "30.602")
	tag := []string{"-e", "HEAVY_RUN=" + active.ID, "-e", "HEAVY_CPUS=" + strconv.Itoa(active.Budget)}
	cases := []struct {
		name      string
		arguments []string
		want      []string
	}{
		{"compose exec after compose options", []string{"compose", "-f", "x.yml", "exec", "-T", "app", "php", "a"},
			join([]string{"compose", "-f", "x.yml", "exec"}, tag, []string{"-T", "app", "php", "a"})},
		{"run", []string{"run", "--rm", "alpine", "true"}, join([]string{"run"}, tag, []string{"--rm", "alpine", "true"})},
		{"container exec", []string{"container", "exec", "c", "ls"}, join([]string{"container", "exec"}, tag, []string{"c", "ls"})},
		{"ps", []string{"ps"}, []string{"ps"}},
	}
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			got := f.call(t, f.project, f.environment("HEAVY_RUN="+active.ID), "", "docker", current.arguments...)

			want := outcome{stdout: printed("docker", current.want...)}
			if got != want {
				t.Fatalf("docker %q inside run %s = %+v, want %+v", current.arguments, active.ID, got, want)
			}
		})
	}
}

func join(parts ...[]string) []string {
	var joined []string
	for _, part := range parts {
		joined = append(joined, part...)
	}
	return joined
}

// A run the daemon does not report active is no run: the call gets the verdict and waits in the queue.
func TestCallWithTheIdOfAnUnknownRunIsQueued(t *testing.T) {
	f := newFixture(t)
	_, holding := f.activeRun(t, "30.603")

	queued := start(t, filepath.Join(f.shims, "make"), f.project, f.environment("HEAVY_RUN=unknown"), "ci")
	eventually(t, "make ci waits behind the active run", func() bool {
		return strings.Contains(queued.output.String(), "heavy: waiting since ")
	})
	holding.stop(t)

	if status := queued.wait(t); status != 0 {
		t.Fatalf("make ci ended with %d, want 0; output %q", status, queued.output.String())
	}
	note := "heavy: `make ci` is heavy and reached make without the queue, so it waits here in the machine-wide queue. " +
		"Start heavy calls with run_in_background, then the hook queues them up front.\n"
	matchOutput(t, "output", regexp.QuoteMeta(note)+`heavy: waiting since \d{2}:\d{2}:\d{2}, #1 in the queue\n`+
		startedLine("make ci")+regexp.QuoteMeta(printed("make", "ci")), queued.output.String())
}

// NFR-1: a call that only asks for the version or the help is light without reading a list, even where an entry
// names the bare program; any other call of that program waits in the queue.
func TestVersionOrHelpCallPassesWithoutTheLists(t *testing.T) {
	f := newFixture(t)
	f.writeProjectList(t, `{"commands": ["make"]}`)
	var got, want []outcome
	for _, arguments := range [][]string{{"--version"}, {"-v"}, {"-V"}, {"--help"}, {"-h"}, {"--version", "--help"}} {
		got = append(got, f.call(t, f.project, f.environment(), "", "make", arguments...))
		want = append(want, outcome{stdout: printed("make", arguments...)})
	}

	queued := f.call(t, f.project, f.environment(), "", "make", "--version", "lint")

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("version and help calls = %+v, want %+v", got, want)
	}
	note := "heavy: `make` is heavy and reached make without the queue, so it waits here in the machine-wide queue. " +
		"Start heavy calls with run_in_background, then the hook queues them up front.\n"
	if queued.exitStatus != 0 || queued.stderr != note {
		t.Fatalf("make --version lint = %+v, want exit status 0 and stderr %q", queued, note)
	}
	matchOutput(t, "stdout", startedLine("make --version lint")+regexp.QuoteMeta(printed("make", "--version", "lint")), queued.stdout)
}

// Without a daemon HEAVY_RUN names no run, and the shim starts no daemon to ask.
func TestCallWithARunIdButNoDaemonStartsNone(t *testing.T) {
	f := newFixture(t)
	arguments := []string{"compose", "exec", "-T", "web", "cat"}

	got := f.call(t, f.project, f.environment("HEAVY_RUN=gone"), "", "docker", arguments...)

	want := outcome{stdout: printed("docker", arguments...)}
	if got != want {
		t.Fatalf("docker %q with HEAVY_RUN=gone = %+v, want %+v", arguments, got, want)
	}
	if entries, err := os.ReadDir(f.state); err != nil || len(entries) != 0 {
		t.Fatalf("state directory holds %v (%v), want nothing", entries, err)
	}
}

// V15: without a daemon a heavy call that carries a run id passes straight through, since its run may be waiting
// to announce itself to the next daemon; queueing it could wait behind its own run.
func TestHeavyCallWithARunIdButNoDaemonPassesThrough(t *testing.T) {
	f := newFixture(t)

	got := f.call(t, f.project, f.environment("HEAVY_RUN=between-daemons"), "", "make", "ci")

	if want := (outcome{stdout: printed("make", "ci")}); got != want {
		t.Fatalf("make ci with HEAVY_RUN=between-daemons = %+v, want %+v", got, want)
	}
	if entries, err := os.ReadDir(f.state); err != nil || len(entries) != 0 {
		t.Fatalf("state directory holds %v (%v), want nothing", entries, err)
	}
}

// A daemon of another protocol fails the call with its message instead of guessing the run.
func TestCallInsideARunFailsWhenTheDaemonSpeaksAnotherProtocol(t *testing.T) {
	f := newFixture(t)
	listener, err := net.Listen("unix", filepath.Join(f.state, "daemon.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		if _, err := bufio.NewReader(connection).ReadString('\n'); err != nil {
			return
		}
		fmt.Fprintf(connection, `{"protocol": %d, "version": "", "stamp": ""}`+"\n", protocol.Version+1)
	}()

	got := f.call(t, f.project, f.environment("HEAVY_RUN=r1"), "", "make", "lint")

	want := outcome{exitStatus: 1, stderr: fmt.Sprintf("heavy: the running daemon speaks protocol %d, this heavy %d; restart the session\n",
		protocol.Version+1, protocol.Version)}
	if got != want {
		t.Fatalf("make lint = %+v, want %+v", got, want)
	}
}

// A broken personal list fails the call as a broken project list does.
func TestBrokenPersonalListFailsTheCall(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(f.home, ".config", "heavy", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"projects": []}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got := f.call(t, f.project, f.environment(), "", "make", "lint")

	want := outcome{exitStatus: 1, stderr: "heavy: " + path + ": expected " + lists.PersonalForm + `, got {"projects":[]}` + "\n"}
	if got != want {
		t.Fatalf("make lint with a broken personal list = %+v, want %+v", got, want)
	}
}

// A real program the system cannot start fails the call as the shell would: 127 for a missing interpreter.
func TestRealProgramThatCannotStartFailsTheCall(t *testing.T) {
	f := newFixture(t)
	real := filepath.Join(f.fakes, "make")
	if err := os.WriteFile(real, []byte("#!/nonexistent/interpreter\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := f.call(t, f.scratch, f.environment(), "", "make", "lint")

	want := outcome{exitStatus: 127, stderr: "heavy: starting " + real + " failed: no such file or directory\n"}
	if got != want {
		t.Fatalf("make lint = %+v, want %+v", got, want)
	}
}

// A docker call whose container names no compose project because docker inspect fails counts in its cwd alone,
// and the shim says why.
func TestFailingContainerLookupIsReported(t *testing.T) {
	f := newFixture(t)
	arguments := []string{"exec", "box", "vitest", "run"}

	got := f.call(t, f.scratch, f.environment("FAKE_STATUS=1"), "", "docker", arguments...)

	if got.exitStatus != 1 || got.stdout != printed("docker", arguments...) {
		t.Fatalf("docker %q = %+v, want exit status 1 of the fake and its output", arguments, got)
	}
	matchOutput(t, "stderr", `level=WARN msg="docker inspect failed, so the container names no compose project" container=box docker=`+
		regexp.QuoteMeta(filepath.Join(f.fakes, "docker"))+` error="exit status 1" stderr=""`+"\n", got.stderr)
}
