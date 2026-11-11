package lists_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/andreasisaak/heavy/internal/lists"
)

// writeJSON writes content as compact JSON to name in directory and returns the path.
func writeJSON(t *testing.T, directory, name string, content any) string {
	t.Helper()
	data, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("encoding %#v: %v", content, err)
	}
	return writeFile(t, directory, name, string(data))
}

func writeFile(t *testing.T, directory, name, content string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

type object = map[string]any

// test_heavy_lists.py:31
func TestProjectListSplitsItsCommands(t *testing.T) {
	path := writeJSON(t, t.TempDir(), ".heavy.json", object{"commands": []string{"make ci", "npm run test", "pytest"}})

	got, warnings := lists.LoadProjectList(path)

	want := []lists.Entry{{Words: []string{"make", "ci"}}, {Words: []string{"npm", "run", "test"}}, {Words: []string{"pytest"}}}
	if len(warnings) != 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadProjectList = %#v, %q; want %#v without warnings", got, warnings, want)
	}
}

// test_heavy_lists.py:38
func TestEntryObjectCarriesItsExceptions(t *testing.T) {
	path := writeJSON(t, t.TempDir(), ".heavy.json", object{"commands": []any{
		"make ci",
		object{"command": "tests/Complete/checkall.sh", "unless": []string{"CHECKALL_CASES="}},
		object{"command": "make test-complete", "unless": []string{"TEST_WORKERS=12", "--dry-run"}},
	}})

	got, warnings := lists.LoadProjectList(path)

	want := []lists.Entry{
		{Words: []string{"make", "ci"}},
		{Words: []string{"checkall.sh"}, Unless: []string{"CHECKALL_CASES="}},
		{Words: []string{"make", "test-complete"}, Unless: []string{"TEST_WORKERS=12", "--dry-run"}},
	}
	if len(warnings) != 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadProjectList = %#v, %q; want %#v without warnings", got, warnings, want)
	}
}

// test_heavy_lists.py:59
func TestProgramOfAnEntryIsKeptAsItsFileName(t *testing.T) {
	path := writeJSON(t, t.TempDir(), ".heavy.json", object{"commands": []string{"tools/ci/infra-test.sh --full", "./bin/e2e"}})

	got, warnings := lists.LoadProjectList(path)

	want := []lists.Entry{{Words: []string{"infra-test.sh", "--full"}}, {Words: []string{"e2e"}}}
	if len(warnings) != 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadProjectList = %#v, %q; want %#v without warnings", got, warnings, want)
	}
}

func TestEntryObjectCarriesRedPatternsAndFlags(t *testing.T) {
	path := writeJSON(t, t.TempDir(), ".heavy.json", object{"commands": []any{
		object{"command": "make ci", "red": []string{"^First red in "}},
		object{"command": "make ci", "exclusive": true, "restart": true},
		object{"command": "make test", "exclusive": false},
	}})

	got, warnings := lists.LoadProjectList(path)

	want := []lists.Entry{
		{Words: []string{"make", "ci"}, Red: []*regexp.Regexp{regexp.MustCompile("^First red in ")}},
		{Words: []string{"make", "ci"}, Exclusive: true, Restart: true},
		{Words: []string{"make", "test"}},
	}
	if len(warnings) != 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadProjectList = %#v, %q; want %#v without warnings", got, warnings, want)
	}
}
