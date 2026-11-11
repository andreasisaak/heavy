package detect_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/andreasisaak/heavy/internal/detect"
	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/shell"
)

// checkout creates a git repository at path with a .heavy.json of the given commands, none for "none",
// and returns its resolved path.
func checkout(t *testing.T, path string, commands ...any) string {
	t.Helper()
	if output, err := exec.Command("git", "init", "-q", path).CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v\n%s", path, err, output)
	}
	if len(commands) != 1 || commands[0] != "none" {
		if commands == nil {
			commands = []any{}
		}
		data, err := json.Marshal(map[string]any{"commands": commands})
		if err != nil {
			t.Fatalf("encoding the list: %v", err)
		}
		if err := os.WriteFile(filepath.Join(path, ".heavy.json"), data, 0o644); err != nil {
			t.Fatalf("writing the list: %v", err)
		}
	}
	return resolved(t, path)
}

func resolved(t *testing.T, path string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolving %s: %v", path, err)
	}
	return real
}

func noPersonal() lists.Personal {
	return lists.Personal{Projects: map[string]lists.Switch{}, Load: lists.DefaultLoad()}
}

// options of a detection; containers maps container names to their compose working directory.
type options struct {
	personal    lists.Personal
	containers  map[string]string
	environment map[string]string
}

// detected runs the detection on a call as the guard would, with fake container labels.
func detected(t *testing.T, command, cwd string, given options) []detect.Match {
	t.Helper()
	script, err := shell.Parse(command)
	if err != nil {
		t.Fatalf("Parse(%q) failed: %v", command, err)
	}
	personal := given.personal
	if personal.Projects == nil {
		personal = noPersonal()
	}
	detector := detect.NewDetector(personal, func(name string) (string, bool) {
		directory, found := given.containers[name]
		return directory, found
	}, given.environment)
	found, err := detector.Heavy(script, cwd)
	if err != nil {
		t.Fatalf("Heavy(%q) failed: %v", command, err)
	}
	return found
}

func texts(found []detect.Match) []string {
	var matched []string
	for _, match := range found {
		matched = append(matched, match.Text)
	}
	return matched
}

func directories(found []detect.Match) []string {
	var matched []string
	for _, match := range found {
		matched = append(matched, match.Directory)
	}
	return matched
}

// entry returns the entry of the project at directory that has the given words, for an expected match.
func entry(t *testing.T, directory string, words ...string) *lists.Entry {
	t.Helper()
	project, err := lists.ProjectOf(directory, noPersonal())
	if err != nil {
		t.Fatalf("ProjectOf(%s) failed: %v", directory, err)
	}
	for index := range project.Commands {
		if equalWords(project.Commands[index].Words, words) {
			return &project.Commands[index]
		}
	}
	t.Fatalf("no entry %q in %s", words, directory)
	return nil
}

func equalWords(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
