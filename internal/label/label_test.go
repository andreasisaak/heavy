package label_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/andreasisaak/heavy/internal/detect"
	"github.com/andreasisaak/heavy/internal/label"
	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/shell"
	"github.com/andreasisaak/heavy/internal/testkit"
)

func checkout(t *testing.T, path string, commands ...string) string {
	t.Helper()
	if output, err := exec.Command("git", "init", "-q", path).CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v\n%s", path, err, output)
	}
	data, err := json.Marshal(map[string][]string{"commands": append([]string{}, commands...)})
	if err != nil {
		t.Fatalf("encoding the list: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, ".heavy.json"), data, 0o644); err != nil {
		t.Fatalf("writing the list: %v", err)
	}
	return path
}

// labelOf names the run of a call split as the shell splits it, with no container known.
func labelOf(t *testing.T, command, cwd string, personal lists.Personal) string {
	t.Helper()
	script, err := shell.Parse(command)
	if err != nil {
		t.Fatalf("Parse(%q) failed: %v", command, err)
	}
	detector := detect.NewDetector(personal, func(name string) (string, bool) {
		t.Fatalf("asked for the directory of container %s", name)
		return "", false
	}, nil)
	name, err := label.Label(script.Commands[0].Raw, cwd, detector)
	if err != nil {
		t.Fatalf("Label(%q) failed: %v", command, err)
	}
	return name
}

func noPersonal() lists.Personal {
	return lists.Personal{Projects: map[string]lists.Switch{}, Load: lists.DefaultLoad()}
}

// test_heavy_label.py:29-46
func TestHeavyCommandIsNamedByWhatMatched(t *testing.T) {
	main := checkout(t, filepath.Join(t.TempDir(), "main"), "make ci", "make test-complete", "infra-test.sh")
	if err := os.Mkdir(filepath.Join(main, "frontend"), 0o755); err != nil {
		t.Fatalf("creating frontend: %v", err)
	}
	cases := []struct{ command, want string }{
		{"make ci", "ci"},
		{"make test-complete TEST_WORKERS=1", "test-complete"},
		{"gmake ci", "ci"},
		{"tools/ci/infra-test.sh --all", "infra-test.sh"},
		{"docker compose run --rm -T frontend sh -c 'pnpm exec vitest run --project unit'", "vitest"},
		{"docker compose exec -T app php artisan test", "artisan test"},
		{"pnpm typecheck", "pnpm typecheck"},
		{"/bin/zsh -c 'cd frontend && make ci; echo done'", "ci"},
	}
	for _, current := range cases {
		if got := labelOf(t, current.command, main, noPersonal()); got != current.want {
			t.Errorf("label of %q = %q, want %q", current.command, got, current.want)
		}
	}
}

// test_heavy_label.py:49-60
func TestOtherCommandIsNamedByItsMakeTargetOrProgram(t *testing.T) {
	directory := t.TempDir()
	cases := []struct{ command, want string }{
		{"make -C /sites/other -j 4 lint FIX=1", "lint"},
		{"make", "make"},
		{"env CHECK_WORKER=1 tests/Complete/checkall.sh", "checkall.sh"},
		{"nice -n 10 sleep 25", "sleep"},
		{"status", "status"},
	}
	for _, current := range cases {
		if got := labelOf(t, current.command, directory, noPersonal()); got != current.want {
			t.Errorf("label of %q = %q, want %q", current.command, got, current.want)
		}
	}
}

func TestCallOfOnlyWrappersIsNamedByItsFirstWord(t *testing.T) {
	detector := detect.NewDetector(noPersonal(), nil, nil)

	got, err := label.Label([]string{"nice", "-n", "10"}, t.TempDir(), detector)

	if err != nil || got != "nice" {
		t.Fatalf("Label = %q, %v; want nice", got, err)
	}
}

// test_heavy_label.py:63: 0.6 printed the name from a command line of its own; the Go client calls Label with
// the personal list of the home directory, so this case loads it from there.
func TestLabelUsesThePersonalListOfTheHome(t *testing.T) {
	main := checkout(t, filepath.Join(t.TempDir(), "main"))
	testkit.Home(t)
	path, err := lists.PersonalPath()
	if err != nil {
		t.Fatalf("PersonalPath failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}
	resolved, err := filepath.EvalSymlinks(main)
	if err != nil {
		t.Fatalf("resolving %s: %v", main, err)
	}
	data, _ := json.Marshal(map[string]any{"projects": map[string]any{resolved: map[string]any{"commands": []string{"make ci"}}}})
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	personal := lists.LoadPersonal(path)

	if got := labelOf(t, "make ci", main, personal); got != "ci" {
		t.Fatalf("label = %q, want ci", got)
	}
}
