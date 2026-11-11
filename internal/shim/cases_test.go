package shim_test

import (
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/shim"
)

// The cases of hooks/test_heavy_shim.py with their inputs and expectations. Where 0.6 printed the lines of its lock
// ("waiting since", "lock acquired at … running: …"), heavy now prints the started line of its queue; a run that
// finds the queue empty prints no waiting line.

var heavyCall = []string{"compose", "exec", "-T", "web", "pnpm", "exec", "vitest", "run"}

const netNote = "heavy: `vitest` is heavy and reached docker without the queue, so it waits here in the machine-wide queue. " +
	"Start heavy calls with run_in_background, then the hook queues them up front.\n"

// startedLine matches the line of a run that starts at once.
func startedLine(command string) string {
	return `heavy: started at \d{2}:\d{2}:\d{2} with \d+ cores \(HEAVY_CPUS\), running: ` + regexp.QuoteMeta(command) + "\n"
}

func matchOutput(t *testing.T, what, pattern, got string) {
	t.Helper()
	if !regexp.MustCompile(`\A` + pattern + `\z`).MatchString(got) {
		t.Fatalf("%s = %q, want a match of %q", what, got, pattern)
	}
}

// test_heavy_shim.py:76
func TestLightCallReachesTheRealProgramUnchanged(t *testing.T) {
	f := newFixture(t)
	arguments := []string{"ps", "--format", "{{.Names}}", "a b", "", "*"}

	got := f.call(t, f.project, f.environment("FAKE_STATUS=3"), "", "docker", arguments...)

	want := outcome{exitStatus: 3, stdout: printed("docker", arguments...)}
	if got != want {
		t.Fatalf("docker %q = %+v, want %+v", arguments, got, want)
	}
}

// test_heavy_shim.py:84
func TestLightCallKeepsItsStandardInput(t *testing.T) {
	f := newFixture(t)
	arguments := []string{"compose", "exec", "-T", "web", "cat"}

	got := f.call(t, f.project, f.environment("FAKE_STDIN=1"), "data\n", "docker", arguments...)

	want := outcome{stdout: printed("docker", arguments...) + "data\n"}
	if got != want {
		t.Fatalf("docker %q = %+v, want %+v", arguments, got, want)
	}
}

// test_heavy_shim.py:185
func TestHeavyCallWhereHeavyIsOffPasses(t *testing.T) {
	f := newFixture(t)

	got := f.call(t, f.scratch, f.environment(), "", "docker", heavyCall...)

	want := outcome{stdout: printed("docker", heavyCall...)}
	if got != want {
		t.Fatalf("docker %q outside a checkout = %+v, want %+v", heavyCall, got, want)
	}
}

// test_heavy_shim.py:191
func TestMissingRealProgramFailsWithItsName(t *testing.T) {
	f := newFixture(t)
	environment := []string{"PATH=" + f.shims + ":/usr/bin:/bin", "HOME=" + f.home, "HEAVY_STATE_DIRECTORY=" + f.state}

	got := f.call(t, f.project, environment, "", "bunx", "vitest", "run")

	want := outcome{exitStatus: 127, stderr: "heavy: found no bunx on PATH outside the heavy shims (" + f.shims + ")\n"}
	if got != want {
		t.Fatalf("bunx vitest run = %+v, want %+v", got, want)
	}
}

// test_heavy_shim.py:198: 0.6 ended with the Python traceback, whose last line was `ValueError: <path>: expected …`.
func TestBrokenProjectListFailsTheCall(t *testing.T) {
	f := newFixture(t)
	f.writeProjectList(t, `{"commands": "make ci"}`)

	got := f.call(t, f.project, f.environment(), "", "make", "ci")

	path := filepath.Join(f.project, ".heavy.json")
	want := outcome{exitStatus: 1, stderr: "heavy: " + path + ": expected " + lists.ProjectListForm + `, got {"commands":"make ci"}` + "\n"}
	if got != want {
		t.Fatalf("make ci with a broken list = %+v, want %+v", got, want)
	}
}

// test_heavy_shim.py:210: the shims are links to the heavy binary, which tells the program by the link's name.
func TestEveryShimStartsItsOwnProgram(t *testing.T) {
	f := newFixture(t)
	names := []string{"bun", "bunx", "composer", "docker", "gmake", "make", "npm", "npx", "pnpm", "pnpx", "uv", "uvx", "yarn"}
	var got, want []string
	for _, name := range names {
		got = append(got, f.call(t, f.scratch, f.environment(), "", name, "--version").stdout)
		want = append(want, printed(name, "--version"))
	}

	if !reflect.DeepEqual(shim.Programs, names) {
		t.Fatalf("shim.Programs = %q, want %q", shim.Programs, names)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("outputs = %q, want %q", got, want)
	}
}
