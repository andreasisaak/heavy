package lists_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
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

	got, err := lists.LoadProjectList(path)

	want := []lists.Entry{{Words: []string{"make", "ci"}}, {Words: []string{"npm", "run", "test"}}, {Words: []string{"pytest"}}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadProjectList = %#v, %v; want %#v", got, err, want)
	}
}

// test_heavy_lists.py:38
func TestEntryObjectCarriesItsExceptions(t *testing.T) {
	path := writeJSON(t, t.TempDir(), ".heavy.json", object{"commands": []any{
		"make ci",
		object{"command": "tests/Complete/checkall.sh", "unless": []string{"CHECKALL_CASES="}},
		object{"command": "make test-complete", "unless": []string{"TEST_WORKERS=12", "--dry-run"}},
	}})

	got, err := lists.LoadProjectList(path)

	want := []lists.Entry{
		{Words: []string{"make", "ci"}},
		{Words: []string{"checkall.sh"}, Unless: []string{"CHECKALL_CASES="}},
		{Words: []string{"make", "test-complete"}, Unless: []string{"TEST_WORKERS=12", "--dry-run"}},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadProjectList = %#v, %v; want %#v", got, err, want)
	}
}

// test_heavy_lists.py:59
func TestProgramOfAnEntryIsKeptAsItsFileName(t *testing.T) {
	path := writeJSON(t, t.TempDir(), ".heavy.json", object{"commands": []string{"tools/ci/infra-test.sh --full", "./bin/e2e"}})

	got, err := lists.LoadProjectList(path)

	want := []lists.Entry{{Words: []string{"infra-test.sh", "--full"}}, {Words: []string{"e2e"}}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadProjectList = %#v, %v; want %#v", got, err, want)
	}
}

func TestEntryObjectCarriesRedPatternsAndFlags(t *testing.T) {
	path := writeJSON(t, t.TempDir(), ".heavy.json", object{"commands": []any{
		object{"command": "make ci", "red": []string{"^First red in "}},
		object{"command": "make ci", "exclusive": true, "restart": true},
		object{"command": "make test", "exclusive": false},
	}})

	got, err := lists.LoadProjectList(path)

	want := []lists.Entry{
		{Words: []string{"make", "ci"}, Red: []*regexp.Regexp{regexp.MustCompile("^First red in ")}},
		{Words: []string{"make", "ci"}, Exclusive: true, Restart: true},
		{Words: []string{"make", "test"}},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadProjectList = %#v, %v; want %#v", got, err, want)
	}
}

// Shapes of test_heavy_lists.py:66-83 and of plan task 1.4; the message names the file, the form and the content.
func TestProjectListRejectsOtherShapes(t *testing.T) {
	cases := []struct {
		origin  string
		content any
	}{
		{"test_heavy_lists.py:69", []string{"make ci"}},
		{"test_heavy_lists.py:70", object{"commands": "make ci"}},
		{"test_heavy_lists.py:71", object{"commands": []string{"  "}}},
		{"test_heavy_lists.py:72", object{"commands": [][]string{{"make", "ci"}}}},
		{"test_heavy_lists.py:73", object{"command": []string{"make ci"}}},
		{"test_heavy_lists.py:74", object{"commands": []any{object{"command": "checkall.sh"}}}},
		{"test_heavy_lists.py:75", object{"commands": []any{object{"command": "checkall.sh", "unless": []string{}}}}},
		{"test_heavy_lists.py:76", object{"commands": []any{object{"command": "checkall.sh", "unless": "CHECKALL_CASES="}}}},
		{"test_heavy_lists.py:77", object{"commands": []any{object{"command": "checkall.sh", "unless": []string{"SECURITY CASES="}}}}},
		{"test_heavy_lists.py:78", object{"commands": []any{object{"command": "checkall.sh", "unless": []string{""}}}}},
		{"test_heavy_lists.py:79", object{"commands": []any{object{"command": "checkall.sh", "unless": []string{"="}}}}},
		{"test_heavy_lists.py:80", object{"commands": []any{object{"command": "checkall.sh", "unless": []string{"--cases="}}}}},
		{"test_heavy_lists.py:81", object{"commands": []any{object{"command": " ", "unless": []string{"CHECKALL_CASES="}}}}},
		{"test_heavy_lists.py:82", object{"commands": []any{object{"command": "checkall.sh", "unless": []string{"CHECKALL_CASES="}, "note": "x"}}}},
		{"plan 1.4", object{"commands": []any{object{"command": "make ci"}}}},
		{"plan 1.4", object{"commands": []any{object{"command": "make ci", "red": []string{}}}}},
		{"plan 1.4", object{"commands": []any{object{"command": "make ci", "red": []string{""}}}}},
		{"plan 1.4", object{"commands": []any{object{"command": "make ci", "red": "^First"}}}},
		{"plan 1.4", object{"commands": []any{object{"command": "make ci", "exclusive": "yes"}}}},
		{"plan 1.4", object{"commands": []any{object{"command": "make ci", "restart": nil}}}},
		{"plan 1.4", object{"commands": nil}},
		{"plan 1.4", object{}},
		{"plan 1.4", nil},
	}
	for _, current := range cases {
		directory := t.TempDir()
		path := writeJSON(t, directory, ".heavy.json", current.content)
		compact, _ := json.Marshal(current.content)

		_, err := lists.LoadProjectList(path)

		want := "heavy: " + path + ": expected " + lists.ProjectListForm + ", got " + string(compact)
		if err == nil || err.Error() != want {
			t.Errorf("%s: LoadProjectList error = %v, want %q", current.origin, err, want)
		}
	}
}

func TestProjectListWithAPatternThatDoesNotCompileNamesThePattern(t *testing.T) {
	path := writeJSON(t, t.TempDir(), ".heavy.json", object{"commands": []any{object{"command": "make ci", "red": []string{"("}}}})

	_, err := lists.LoadProjectList(path)

	want := "heavy: " + path + ": red pattern ( does not compile: error parsing regexp: missing closing ): `(`"
	if err == nil || err.Error() != want {
		t.Fatalf("LoadProjectList error = %v, want %q", err, want)
	}
}

// test_heavy_lists.py:95
func TestProjectListWithInvalidJSONNamesTheFile(t *testing.T) {
	path := writeFile(t, t.TempDir(), ".heavy.json", `{"commands": ["make ci"],}`)

	_, err := lists.LoadProjectList(path)

	prefix := "heavy: " + path + ": expected " + lists.ProjectListForm + ", got invalid JSON ("
	suffix := `): {"commands": ["make ci"],}`
	if err == nil || !strings.HasPrefix(err.Error(), prefix) || !strings.HasSuffix(err.Error(), suffix) {
		t.Fatalf("LoadProjectList error = %v, want %q … %q", err, prefix, suffix)
	}
}

func TestProjectListThatCannotBeReadNamesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".heavy.json")

	_, err := lists.LoadProjectList(path)

	want := "heavy: open " + path + ": no such file or directory"
	if err == nil || err.Error() != want {
		t.Fatalf("LoadProjectList error = %v, want %q", err, want)
	}
}
