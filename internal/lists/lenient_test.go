package lists_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/andreasisaak/heavy/internal/lists"
)

func unknownKey(path, key string) string {
	return "heavy: " + path + ": unknown key \"" + key + "\", ignored; a newer heavy may know it"
}

func unusable(path, key, value, expected, instead string) string {
	return "heavy: " + path + ": cannot use \"" + key + "\": " + value + ", expected " + expected + "; " + instead
}

// A list written for a newer heavy holds keys this heavy does not know: it reads the rest and says which keys it
// left out. This is what keeps a session of an older version working beside a newer one.
func TestPersonalListWithKeysOfANewerHeavyLoadsTheRest(t *testing.T) {
	main := t.TempDir()
	path := writeJSON(t, t.TempDir(), "config.json", object{
		"load":     object{"max_parallel": 5, "peak_of_tomorrow_bytes": 1},
		"projects": object{main: object{"commands": []any{"make e2e", object{"command": "make ci", "priority": 3}}, "owner": "me"}},
		"theme":    "dark",
	})

	got := lists.LoadPersonal(path)

	load := lists.DefaultLoad()
	load.MaxParallel, load.MaxParallelSet = 5, true
	resolved, _ := filepath.EvalSymlinks(main)
	want := lists.Personal{
		Projects: map[string]lists.Switch{resolved: {Enabled: true, Commands: []lists.Entry{{Words: []string{"make", "e2e"}}, {Words: []string{"make", "ci"}}}}},
		Load:     load,
		Warnings: []string{
			unknownKey(path, "load.peak_of_tomorrow_bytes"),
			unknownKey(path, "projects."+main+".commands[1].priority"),
			unknownKey(path, "projects."+main+".owner"),
			unknownKey(path, "theme"),
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadPersonal =\n%#v\nwant\n%#v", got, want)
	}
}

// A value heavy cannot use costs only what it stands for: the threshold keeps its default, the entry or the
// project is left out, and the list counts as broken, so the daemon keeps the thresholds it had.
func TestPersonalListWithValuesItCannotUseFallsBackPerValue(t *testing.T) {
	main := t.TempDir()
	resolved, _ := filepath.EvalSymlinks(main)
	defaults := lists.DefaultLoad()
	projects := func(entry lists.Switch) map[string]lists.Switch { return map[string]lists.Switch{resolved: entry} }
	none := map[string]lists.Switch{}
	cases := []struct {
		name     string
		content  any
		projects map[string]lists.Switch
		warning  func(path string) string
	}{
		{"limit of zero", object{"load": object{"max_parallel": 0}}, none,
			func(path string) string {
				return unusable(path, "load.max_parallel", "0", "a whole number from 1", "heavy uses its default")
			}},
		{"limit with a fraction", object{"load": object{"max_parallel": 2.5}}, none,
			func(path string) string {
				return unusable(path, "load.max_parallel", "2.5", "a whole number from 1", "heavy uses its default")
			}},
		{"negative bytes", object{"load": object{"reserve_bytes": -1}}, none,
			func(path string) string {
				return unusable(path, "load.reserve_bytes", "-1", "whole bytes from 0", "heavy uses its default")
			}},
		{"percent over 100", object{"load": object{"host_pressure_percent": 101}}, none,
			func(path string) string {
				return unusable(path, "load.host_pressure_percent", "101", "a whole number from 0 to 100", "heavy uses its default")
			}},
		{"negative load", object{"load": object{"load_per_core": -0.5}}, none,
			func(path string) string {
				return unusable(path, "load.load_per_core", "-0.5", "a number from 0", "heavy uses its default")
			}},
		{"load that is no object", object{"load": nil}, none,
			func(path string) string {
				return unusable(path, "load", "null", "an object", "heavy uses the default thresholds")
			}},
		{"projects that are no object", object{"projects": []string{"/sites/main"}}, none,
			func(path string) string {
				return unusable(path, "projects", `["/sites/main"]`, "an object", "heavy reads no project from it")
			}},
		{"switch with a word for enabled", object{"projects": object{main: object{"enabled": "no"}}}, none,
			func(path string) string {
				return unusable(path, "projects."+main+".enabled", `"no"`, "true or false", "heavy leaves this project out")
			}},
		{"commands that are no list", object{"projects": object{main: object{"commands": "make ci"}}}, projects(lists.Switch{Enabled: true}),
			func(path string) string {
				return unusable(path, "projects."+main+".commands", `"make ci"`, "a list", "heavy reads no command from it")
			}},
		{"exception with a space", object{"projects": object{main: object{"commands": []any{object{"command": "checkall.sh", "unless": []string{"TWO WORDS"}}, "make ci"}}}},
			projects(lists.Switch{Enabled: true, Commands: []lists.Entry{{Words: []string{"make", "ci"}}}}),
			func(path string) string {
				return unusable(path, "projects."+main+".commands[0]", `{"command":"checkall.sh","unless":["TWO WORDS"]}`, "a command as in .heavy.json", "heavy leaves this command out")
			}},
	}
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			path := writeJSON(t, t.TempDir(), "config.json", current.content)

			got := lists.LoadPersonal(path)

			want := lists.Personal{Projects: current.projects, Load: defaults, Warnings: []string{current.warning(path)}, Broken: true}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("LoadPersonal =\n%#v\nwant\n%#v", got, want)
			}
		})
	}
}

func TestPersonalListThatIsNoObjectOrNoJSONOrNoFileSwitchesNothing(t *testing.T) {
	folder := t.TempDir()
	cases := map[string]struct{ path, warning string }{
		"a list":       {writeFile(t, t.TempDir(), "config.json", `[]`), "expected a JSON object, got []; heavy reads nothing from it"},
		"invalid JSON": {writeFile(t, t.TempDir(), "config.json", `{"load": {"max_parallel": 4},}`), "invalid JSON (invalid character '}' looking for beginning of object key string); heavy reads nothing from it"},
		"a folder":     {folder, "read " + folder + ": is a directory; heavy reads nothing from it"},
	}
	for name, current := range cases {
		t.Run(name, func(t *testing.T) {
			got := lists.LoadPersonal(current.path)

			want := lists.Personal{Projects: map[string]lists.Switch{}, Load: lists.DefaultLoad(), Warnings: []string{"heavy: " + current.path + ": " + current.warning}, Broken: true}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("LoadPersonal =\n%#v\nwant\n%#v", got, want)
			}
		})
	}
}

// The same holds for .heavy.json, which a colleague with a newer heavy may have committed.
func TestProjectListWithKeysOfANewerHeavyAndBrokenEntriesLoadsTheRest(t *testing.T) {
	path := writeJSON(t, t.TempDir(), ".heavy.json", object{
		"commands": []any{
			"make ci",
			object{"command": "make test", "red": []string{"^First red", "("}, "weight": 2},
			object{"command": " "},
			[]string{"make", "lint"},
			object{"command": "make e2e", "exclusive": "yes"},
		},
		"version": 2,
	})

	entries, warnings := lists.LoadProjectList(path)

	wantEntries := []lists.Entry{
		{Words: []string{"make", "ci"}},
		{Words: []string{"make", "test"}, Red: []*regexp.Regexp{regexp.MustCompile("^First red")}},
	}
	wantWarnings := []string{
		unusable(path, "commands[1].red[1]", `"("`, "an RE2 pattern (error parsing regexp: missing closing ): `(`)", "heavy leaves this pattern out"),
		unknownKey(path, "commands[1].weight"),
		unusable(path, "commands[2]", `{"command":" "}`, "a command as in .heavy.json", "heavy leaves this command out"),
		unusable(path, "commands[3]", `["make","lint"]`, "a command as in .heavy.json", "heavy leaves this command out"),
		unusable(path, "commands[4]", `{"command":"make e2e","exclusive":"yes"}`, "a command as in .heavy.json", "heavy leaves this command out"),
		unknownKey(path, "version"),
	}
	if !reflect.DeepEqual(entries, wantEntries) || !reflect.DeepEqual(warnings, wantWarnings) {
		t.Fatalf("LoadProjectList =\n%#v\n%q\nwant\n%#v\n%q", entries, warnings, wantEntries, wantWarnings)
	}
}

func TestProjectListWithoutCommandsOrThatIsNoObjectListsNone(t *testing.T) {
	cases := map[string]struct{ content, warning string }{
		"no commands":          {`{}`, `cannot use "commands": nothing, expected a list; heavy reads no command from it`},
		"commands as a string": {`{"commands": "make ci"}`, `cannot use "commands": "make ci", expected a list; heavy reads no command from it`},
		"a list":               {`["make ci"]`, `expected a JSON object, got ["make ci"]; heavy reads nothing from it`},
		"invalid JSON":         {`{"commands": ["make ci"],}`, `invalid JSON (invalid character '}' looking for beginning of object key string); heavy reads nothing from it`},
	}
	for name, current := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeFile(t, t.TempDir(), ".heavy.json", current.content)

			entries, warnings := lists.LoadProjectList(path)

			if want := []string{"heavy: " + path + ": " + current.warning}; len(entries) != 0 || !reflect.DeepEqual(warnings, want) {
				t.Fatalf("LoadProjectList = %#v, %q; want no command and %q", entries, warnings, want)
			}
		})
	}
}

// A project with a list heavy cannot read is still a project with heavy on: its catalogue counts.
func TestProjectWithABrokenListKeepsTheCatalogueAndSaysWhatItLeftOut(t *testing.T) {
	main := checkout(t, filepath.Join(t.TempDir(), "main"))
	path := writeJSON(t, main, ".heavy.json", object{"commands": []any{1, "make ci"}})

	project, err := lists.ProjectOf(main, noPersonal())

	want := lists.Project{Catalogue: true, Commands: []lists.Entry{{Words: []string{"make", "ci"}}},
		Warnings: []string{unusable(path, "commands[0]", "1", "a command as in .heavy.json", "heavy leaves this command out")}}
	if err != nil || !reflect.DeepEqual(project, want) {
		t.Fatalf("ProjectOf = %#v, %v; want %#v", project, err, want)
	}
}

func TestProjectListThatCannotBeReadListsNone(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".heavy.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}

	entries, warnings := lists.LoadProjectList(path)

	if want := []string{"heavy: " + path + ": read " + path + ": is a directory; heavy reads nothing from it"}; len(entries) != 0 || !reflect.DeepEqual(warnings, want) {
		t.Fatalf("LoadProjectList = %#v, %q; want no command and %q", entries, warnings, want)
	}
}
