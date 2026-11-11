package lists_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/andreasisaak/heavy/internal/lists"
)

// checkout creates a git repository at path and returns its resolved path.
func checkout(t *testing.T, path string) string {
	t.Helper()
	if output, err := exec.Command("git", "init", "-q", path).CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v\n%s", path, err, output)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolving %s: %v", path, err)
	}
	return resolved
}

func projectOf(t *testing.T, directory string, personal lists.Personal) lists.Project {
	t.Helper()
	project, err := lists.ProjectOf(directory, personal)
	if err != nil {
		t.Fatalf("ProjectOf(%s) failed: %v", directory, err)
	}
	return project
}

func noPersonal() lists.Personal {
	return lists.Personal{Projects: map[string]lists.Switch{}, Load: lists.DefaultLoad()}
}

// test_heavy_lists.py:137
func TestProjectListTurnsTheCatalogueOn(t *testing.T) {
	main := checkout(t, filepath.Join(t.TempDir(), "main"))
	writeJSON(t, main, ".heavy.json", object{"commands": []string{"make ci"}})

	if got := projectOf(t, filepath.Join(main, "sub"), noPersonal()); !reflect.DeepEqual(got, lists.Project{}) {
		t.Fatalf("project of a missing directory = %#v, want heavy off", got)
	}
	if err := os.Mkdir(filepath.Join(main, "sub"), 0o755); err != nil {
		t.Fatalf("creating sub: %v", err)
	}
	want := lists.Project{Catalogue: true, Commands: []lists.Entry{{Words: []string{"make", "ci"}}}}
	if got := projectOf(t, filepath.Join(main, "sub"), noPersonal()); !reflect.DeepEqual(got, want) {
		t.Fatalf("project of sub = %#v, want %#v", got, want)
	}
}

// test_heavy_lists.py:146
func TestEmptyProjectListTurnsTheCatalogueOnAlone(t *testing.T) {
	main := checkout(t, filepath.Join(t.TempDir(), "main"))
	writeJSON(t, main, ".heavy.json", object{"commands": []string{}})

	if got := projectOf(t, main, noPersonal()); !reflect.DeepEqual(got, lists.Project{Catalogue: true}) {
		t.Fatalf("project = %#v, want the catalogue alone", got)
	}
}

// test_heavy_lists.py:153
func TestPersonalEntryTurnsTheCatalogueOnWithoutAProjectList(t *testing.T) {
	main := checkout(t, filepath.Join(t.TempDir(), "main"))
	personal := noPersonal()
	personal.Projects[main] = lists.Switch{Enabled: true, Commands: []lists.Entry{{Words: []string{"make", "e2e"}}}}

	want := lists.Project{Catalogue: true, Commands: []lists.Entry{{Words: []string{"make", "e2e"}}}}
	if got := projectOf(t, main, personal); !reflect.DeepEqual(got, want) {
		t.Fatalf("project = %#v, want %#v", got, want)
	}
}

// test_heavy_lists.py:161
func TestPersonalSwitchTurnsProjectListAndCatalogueOff(t *testing.T) {
	main := checkout(t, filepath.Join(t.TempDir(), "main"))
	writeJSON(t, main, ".heavy.json", object{"commands": []string{"make ci"}})
	personal := noPersonal()
	personal.Projects[main] = lists.Switch{Enabled: false, Commands: []lists.Entry{{Words: []string{"make", "e2e"}}}}

	want := lists.Project{Catalogue: false, Commands: []lists.Entry{{Words: []string{"make", "e2e"}}}}
	if got := projectOf(t, main, personal); !reflect.DeepEqual(got, want) {
		t.Fatalf("project = %#v, want %#v", got, want)
	}
}

// test_heavy_lists.py:170
func TestCheckoutWithoutAnyListHasHeavyOff(t *testing.T) {
	directory := t.TempDir()
	main := checkout(t, filepath.Join(directory, "main"))

	for _, path := range []string{main, directory} {
		if got := projectOf(t, path, noPersonal()); !reflect.DeepEqual(got, lists.Project{}) {
			t.Errorf("project of %s = %#v, want heavy off", path, got)
		}
	}
}

func TestPersonalCommandsAddToTheProjectList(t *testing.T) {
	main := checkout(t, filepath.Join(t.TempDir(), "main"))
	writeJSON(t, main, ".heavy.json", object{"commands": []string{"make ci"}})
	personal := noPersonal()
	personal.Projects[main] = lists.Switch{Enabled: true, Commands: []lists.Entry{{Words: []string{"make", "e2e"}}}}

	want := lists.Project{Catalogue: true, Commands: []lists.Entry{{Words: []string{"make", "ci"}}, {Words: []string{"make", "e2e"}}}}
	if got := projectOf(t, main, personal); !reflect.DeepEqual(got, want) {
		t.Fatalf("project = %#v, want %#v", got, want)
	}
}

func TestWorktreeFindsItsMainCheckout(t *testing.T) {
	directory := t.TempDir()
	main := checkout(t, filepath.Join(directory, "main"))
	git := func(arguments ...string) {
		command := exec.Command("git", append([]string{"-C", main, "-c", "user.name=Example", "-c", "user.email=example@example.com"}, arguments...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %q: %v\n%s", arguments, err, output)
		}
	}
	git("commit", "-q", "--allow-empty", "-m", "init")
	git("worktree", "add", "-q", "-b", "feature", filepath.Join(directory, "main-feature"))
	worktree := filepath.Join(main+"-feature", "sub")
	if err := os.Mkdir(worktree, 0o755); err != nil {
		t.Fatalf("creating %s: %v", worktree, err)
	}

	got, found, err := lists.FindCheckout(worktree)

	want := lists.Checkout{Toplevel: main + "-feature", Main: main}
	if err != nil || !found || got != want {
		t.Fatalf("FindCheckout(%s) = %#v, %v, %v; want %#v", worktree, got, found, err, want)
	}
}

func TestGitMissingFromPathIsAnError(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("PATH", t.TempDir())

	_, err := lists.ProjectOf(directory, noPersonal())

	want := "heavy: git failed in " + directory + `: exec: "git": executable file not found in $PATH`
	if err == nil || err.Error() != want {
		t.Fatalf("ProjectOf error = %v, want %q", err, want)
	}
}

func TestBrokenProjectListFailsTheProject(t *testing.T) {
	main := checkout(t, filepath.Join(t.TempDir(), "main"))
	path := writeJSON(t, main, ".heavy.json", object{"commands": []int{1}})

	_, err := lists.ProjectOf(main, noPersonal())

	want := "heavy: " + path + ": expected " + lists.ProjectListForm + ", got {\"commands\":[1]}"
	if err == nil || err.Error() != want {
		t.Fatalf("ProjectOf error = %v, want %q", err, want)
	}
}
