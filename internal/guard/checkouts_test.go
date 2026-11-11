package guard_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/andreasisaak/heavy/internal/testkit"
)

// temporaryDirectory is an empty directory whose path holds only characters the shell takes as they are.
func temporaryDirectory(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp("", "guard-")
	if err != nil {
		t.Fatalf("creating a temporary directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Errorf("removing %s: %v", directory, err)
		}
	})
	return directory
}

func resolved(t *testing.T, path string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolving %s: %v", path, err)
	}
	return real
}

func git(t *testing.T, arguments ...string) {
	t.Helper()
	command := exec.Command("/usr/bin/git", append([]string{"-c", "user.name=Example", "-c", "user.email=example@example.com"}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %q failed: %v\n%s", arguments, err, output)
	}
}

// checkouts creates a main checkout and a worktree of the branch feature, neither with a project list.
func checkouts(t *testing.T) (main, worktree string) {
	t.Helper()
	testkit.Home(t)
	base := temporaryDirectory(t)
	main, worktree = filepath.Join(base, "main"), filepath.Join(base, "main-feature")
	git(t, "init", "-q", main)
	git(t, "-C", main, "commit", "-q", "--allow-empty", "-m", "init")
	git(t, "-C", main, "worktree", "add", "-q", "-b", "feature", worktree)
	return resolved(t, main), resolved(t, worktree)
}

// listed creates both checkouts listing make ci and make test, as when the branch carries the project list.
func listed(t *testing.T) (main, worktree string) {
	t.Helper()
	main, worktree = checkouts(t)
	for _, checkout := range []string{main, worktree} {
		projectList(t, checkout, "make ci", "make test")
	}
	return main, worktree
}

func projectList(t *testing.T, checkout string, commands ...string) {
	t.Helper()
	data, err := json.Marshal(map[string][]string{"commands": commands})
	if err != nil {
		t.Fatalf("encoding the project list: %v", err)
	}
	if err := os.WriteFile(filepath.Join(checkout, ".heavy.json"), data, 0o644); err != nil {
		t.Fatalf("writing the project list: %v", err)
	}
}
