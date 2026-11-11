package detect_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/andreasisaak/heavy/internal/detect"
	"github.com/andreasisaak/heavy/internal/shell"
	"github.com/andreasisaak/heavy/internal/testkit"
)

const composeLabel = `{{index .Config.Labels "com.docker.compose.project.working_dir"}}`

// fakeDocker writes a docker into directory that records its arguments, prints answer to stdout (status 0)
// or stderr (any other status) and exits with status; it returns the file of the recorded calls.
func fakeDocker(t *testing.T, directory, answer string, status int) string {
	t.Helper()
	calls := filepath.Join(directory, "calls")
	stream := ""
	if status != 0 {
		stream = " >&2"
	}
	testkit.FakeProgram(t, directory, "docker",
		"printf '%s\\n' \"$*\" >> "+shell.Quote(calls)+"\nprintf '%s\\n' "+shell.Quote(answer)+stream+"\nexit "+strconv.Itoa(status))
	return calls
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// logger writes JSON records without their time into a buffer.
func logger() (*slog.Logger, *bytes.Buffer) {
	var buffer bytes.Buffer
	handler := slog.NewJSONHandler(&buffer, &slog.HandlerOptions{ReplaceAttr: func(groups []string, attribute slog.Attr) slog.Attr {
		if attribute.Key == slog.TimeKey && len(groups) == 0 {
			return slog.Attr{}
		}
		return attribute
	}})
	return slog.New(handler), &buffer
}

// test_heavy_detect.py:260
func TestContainerDirectoryReadsTheComposeLabel(t *testing.T) {
	directory := t.TempDir()
	calls := fakeDocker(t, directory, "/sites/main", 0)
	log, records := logger()

	got, found := detect.ContainerLookup(directory+":/usr/bin:/bin", log)("main-web-1")

	if got != "/sites/main" || !found {
		t.Fatalf("lookup = %q, %v; want /sites/main", got, found)
	}
	if recorded := readFile(t, calls); recorded != "inspect --format "+composeLabel+" main-web-1\n" {
		t.Fatalf("docker got %q", recorded)
	}
	if records.Len() != 0 {
		t.Fatalf("logged %q, want nothing", records.String())
	}
}

// test_heavy_detect.py:268, the case of a container without the label
func TestContainerWithoutAComposeLabelHasNoDirectory(t *testing.T) {
	directory := t.TempDir()
	calls := fakeDocker(t, directory, "", 0)
	log, records := logger()

	got, found := detect.ContainerLookup(directory+":/usr/bin:/bin", log)("plain-1")

	if got != "" || found {
		t.Fatalf("lookup = %q, %v; want nothing", got, found)
	}
	if recorded := readFile(t, calls); recorded != "inspect --format "+composeLabel+" plain-1\n" {
		t.Fatalf("docker got %q", recorded)
	}
	if records.Len() != 0 {
		t.Fatalf("logged %q, want nothing", records.String())
	}
}

// test_heavy_detect.py:268, the case of a container docker does not know; the failure goes to the log.
func TestFailedDockerInspectIsLoggedAndFindsNoDirectory(t *testing.T) {
	directory := t.TempDir()
	fakeDocker(t, directory, "Error: No such object: gone-web-1", 1)
	log, records := logger()

	got, found := detect.ContainerLookup(directory+":/usr/bin:/bin", log)("gone-web-1")

	if got != "" || found {
		t.Fatalf("lookup = %q, %v; want nothing", got, found)
	}
	want := `{"level":"WARN","msg":"docker inspect failed, so the container names no compose project","container":"gone-web-1",` +
		`"docker":"` + filepath.Join(directory, "docker") + `","error":"exit status 1","stderr":"Error: No such object: gone-web-1"}` + "\n"
	if records.String() != want {
		t.Fatalf("logged %q, want %q", records.String(), want)
	}
}

func TestMissingDockerIsLoggedAndFindsNoDirectory(t *testing.T) {
	log, records := logger()

	got, found := detect.ContainerLookup(t.TempDir(), log)("main-web-1")

	if got != "" || found {
		t.Fatalf("lookup = %q, %v; want nothing", got, found)
	}
	want := `{"level":"WARN","msg":"found no docker on PATH, so the container names no compose project","container":"main-web-1"}` + "\n"
	if records.String() != want {
		t.Fatalf("logged %q, want %q", records.String(), want)
	}
}

// test_heavy_detect.py:279: a shim is a link to the heavy binary.
func TestContainerDirectorySkipsTheHeavyShims(t *testing.T) {
	plugin := t.TempDir()
	for _, folder := range []string{"bin", "shims", "real"} {
		if err := os.Mkdir(filepath.Join(plugin, folder), 0o755); err != nil {
			t.Fatalf("creating %s: %v", folder, err)
		}
	}
	testkit.FakeProgram(t, filepath.Join(plugin, "bin"), "heavy", "echo shim; exit 1")
	if err := os.Symlink("../bin/heavy", filepath.Join(plugin, "shims", "docker")); err != nil {
		t.Fatalf("linking the shim: %v", err)
	}
	fakeDocker(t, filepath.Join(plugin, "real"), "/sites/main", 0)
	log, _ := logger()
	path := filepath.Join(plugin, "shims") + ":" + filepath.Join(plugin, "real") + ":/usr/bin:/bin"

	got, found := detect.ContainerLookup(path, log)("main-web-1")

	if got != "/sites/main" || !found {
		t.Fatalf("lookup = %q, %v; want /sites/main", got, found)
	}
}

func TestRealProgramSkipsShimsAndDirectoriesWithoutIt(t *testing.T) {
	plugin := t.TempDir()
	for _, folder := range []string{"bin", "shims", "empty", "real"} {
		if err := os.Mkdir(filepath.Join(plugin, folder), 0o755); err != nil {
			t.Fatalf("creating %s: %v", folder, err)
		}
	}
	testkit.FakeProgram(t, filepath.Join(plugin, "bin"), "heavy", "exit 1")
	if err := os.Symlink("../bin/heavy", filepath.Join(plugin, "shims", "make")); err != nil {
		t.Fatalf("linking the shim: %v", err)
	}
	real := testkit.FakeProgram(t, filepath.Join(plugin, "real"), "make", "exit 0")
	path := filepath.Join(plugin, "shims") + "::" + filepath.Join(plugin, "empty") + ":" + filepath.Join(plugin, "real")

	got, found := detect.RealProgram("make", path)
	missing, missingFound := detect.RealProgram("gmake", path)

	if !reflect.DeepEqual([4]any{got, found, missing, missingFound}, [4]any{real, true, "", false}) {
		t.Fatalf("RealProgram = %q, %v and %q, %v; want %q and nothing", got, found, missing, missingFound, real)
	}
}

func TestFailedContainerLookupLeavesTheCallLight(t *testing.T) {
	directory := resolved(t, t.TempDir())
	fakes := filepath.Join(directory, "fakes")
	if err := os.Mkdir(fakes, 0o755); err != nil {
		t.Fatalf("creating %s: %v", fakes, err)
	}
	fakeDocker(t, fakes, "Error: No such object: gone-web-1", 1)
	log, records := logger()
	detector := detect.NewDetector(noPersonal(), detect.ContainerLookup(fakes+":/usr/bin:/bin", log), nil)
	script, err := shell.Parse("docker exec gone-web-1 pnpm exec vitest run")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	found, err := detector.Heavy(script, directory)

	if err != nil || len(found) != 0 {
		t.Fatalf("Heavy = %#v, %v; want no match", found, err)
	}
	want := `{"level":"WARN","msg":"docker inspect failed, so the container names no compose project","container":"gone-web-1",` +
		`"docker":"` + filepath.Join(fakes, "docker") + `","error":"exit status 1","stderr":"Error: No such object: gone-web-1"}` + "\n"
	if records.String() != want {
		t.Fatalf("logged %q, want %q", records.String(), want)
	}
}
