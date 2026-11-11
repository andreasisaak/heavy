package detect_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/andreasisaak/heavy/internal/detect"
	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/shell"
)

var exceptions = []any{
	map[string]any{"command": "checkall.sh", "unless": []string{"CHECKALL_CASES="}},
	map[string]any{"command": "make test-complete", "unless": []string{"TEST_WORKERS=12"}},
}

// test_heavy_detect.py:60-84
func TestEntryExceptionFreesTheMarkedCall(t *testing.T) {
	cases := []struct{ heavy, free string }{
		{"tests/Complete/checkall.sh", "CHECK_WORKER=12 CHECKALL_CASES='Apple' tests/Complete/checkall.sh"},
		{"CHECK_WORKER=1 tests/Complete/checkall.sh", "env CHECKALL_CASES=x tests/Complete/checkall.sh"},
		{"CHECKALL_CASES= tests/Complete/checkall.sh", "timeout 600 nice -n 5 env CHECKALL_CASES=x tests/Complete/checkall.sh"},
		{"CHECKALL_CASES='' bash tests/Complete/checkall.sh", "CHECKALL_CASES=Apple bash tests/Complete/checkall.sh"},
		{"bash -c 'tests/Complete/checkall.sh'", "bash -c 'CHECKALL_CASES=Apple tests/Complete/checkall.sh'"},
		{"make test-complete", "make test-complete TEST_WORKERS=12"},
		{`make test-complete TEST_WORKERS="1 2"`, "TEST_WORKERS=12 make test-complete"},
		{"make test-complete TEST_WORKERS=1", "make TEST_WORKERS=12 test-complete"},
		{`make test-complete TEST_WORKERS="1 12"`, `make test-complete TEST_WORKERS="12"`},
		{
			"make test-complete TEST_WORKERS=12 && tests/Complete/checkall.sh",
			"make test-complete TEST_WORKERS=12 && CHECK_WORKER=12 CHECKALL_CASES='Apple|Orange' tests/Complete/checkall.sh",
		},
	}
	main := checkout(t, filepath.Join(t.TempDir(), "main"), exceptions...)
	for _, current := range cases {
		want := "checkall.sh"
		if strings.HasPrefix(current.heavy, "make") && !strings.Contains(current.heavy, "&&") {
			want = "make test-complete"
		}
		heavy := texts(detected(t, current.heavy, main, options{}))
		if len(heavy) == 0 || heavy[len(heavy)-1] != want {
			t.Errorf("heavy %q matched %q, want the last match %q", current.heavy, heavy, want)
		}
		if free := detected(t, current.free, main, options{}); len(free) != 0 {
			t.Errorf("free %q matched %#v, want nothing", current.free, free)
		}
	}
}

// test_heavy_detect.py:87-98 and :101-105
func TestExceptionCountsFromTheEnvironmentAShimSees(t *testing.T) {
	cases := []struct {
		command     string
		environment map[string]string
		want        []string
	}{
		{"make test-complete", map[string]string{"TEST_WORKERS": "12"}, nil},
		{"make test-complete", map[string]string{"TEST_WORKERS": "1 12"}, []string{"make test-complete"}},
		{"make test-complete", map[string]string{}, []string{"make test-complete"}},
		{"tests/Complete/checkall.sh", map[string]string{"CHECKALL_CASES": "Apple"}, nil},
		{"tests/Complete/checkall.sh", map[string]string{"CHECKALL_CASES": ""}, []string{"checkall.sh"}},
	}
	main := checkout(t, filepath.Join(t.TempDir(), "main"), exceptions...)
	for _, current := range cases {
		if got := texts(detected(t, current.command, main, options{environment: current.environment})); !reflect.DeepEqual(got, current.want) {
			t.Errorf("%q with %v matched %q, want %q", current.command, current.environment, got, current.want)
		}
	}
}

// test_heavy_detect.py:108
func TestCatalogueRunnerCountsInAProjectWithAList(t *testing.T) {
	main := checkout(t, filepath.Join(t.TempDir(), "main"))

	got := detected(t, "docker compose exec -T web pnpm exec vitest run --project unit", main, options{})

	want := []detect.Match{{Index: 0, Text: "vitest", Directory: main, ShellDirectory: main}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matched %#v, want %#v", got, want)
	}
}

// test_heavy_detect.py:116
func TestCatalogueRunnerCountsInAProjectOfThePersonalList(t *testing.T) {
	main := checkout(t, filepath.Join(t.TempDir(), "main"), "none")
	personal := noPersonal()
	personal.Projects[main] = lists.Switch{Enabled: true}

	if got := texts(detected(t, "docker compose exec -T app php artisan test", main, options{personal: personal})); !reflect.DeepEqual(got, []string{"artisan test"}) {
		t.Fatalf("matched %q, want artisan test", got)
	}
}

// test_heavy_detect.py:123
func TestCatalogueRunnerPassesWhereHeavyIsOff(t *testing.T) {
	directory := t.TempDir()
	other := checkout(t, filepath.Join(directory, "other"), "none")

	for _, cwd := range []string{other, directory} {
		if got := detected(t, "docker compose exec -T web pnpm exec vitest run", cwd, options{}); len(got) != 0 {
			t.Errorf("in %s matched %#v, want nothing", cwd, got)
		}
	}
}

// test_heavy_detect.py:130
func TestListedEntryNamesTheMatchBeforeTheCatalogue(t *testing.T) {
	main := checkout(t, filepath.Join(t.TempDir(), "main"), "make ci", "docker compose exec api uv run pytest")

	got := texts(detected(t, "make lint && make ci; docker compose exec -T api uv run pytest -x", main, options{}))

	if want := []string{"make ci", "docker compose exec api uv run pytest"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("matched %q, want %q", got, want)
	}
}

// test_heavy_detect.py:139
func TestGmakeMatchesAMakeEntry(t *testing.T) {
	main := checkout(t, filepath.Join(t.TempDir(), "main"), "make ci")

	if got := texts(detected(t, "gmake ci", main, options{})); !reflect.DeepEqual(got, []string{"make ci"}) {
		t.Fatalf("matched %q, want make ci", got)
	}
}

// test_heavy_detect.py:145; the keywords are syntax now, so no start index is left to move.
func TestCommandAfterAShellKeywordCounts(t *testing.T) {
	main := checkout(t, filepath.Join(t.TempDir(), "main"))

	got := detected(t, "if pnpm test; then echo green; fi", main, options{})

	want := []detect.Match{{Index: 0, Text: "pnpm test", Directory: main, ShellDirectory: main}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matched %#v, want %#v", got, want)
	}
}

// test_heavy_detect.py:153
func TestCdNamesTheProjectOfLaterCommands(t *testing.T) {
	directory := resolved(t, t.TempDir())
	main := checkout(t, filepath.Join(directory, "main"))

	got := detected(t, "cd "+main+" && pnpm exec vitest run", directory, options{})

	want := []detect.Match{{Index: 1, Text: "vitest", Directory: main, ShellDirectory: main}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matched %#v, want %#v", got, want)
	}
}

// test_heavy_detect.py:161; the commands of the string follow it in the script and match on their own too.
func TestListEntryInsideAShellStringCounts(t *testing.T) {
	directory := resolved(t, t.TempDir())
	main := checkout(t, filepath.Join(directory, "main"), "make ci")
	makeCi := entry(t, main, "make", "ci")

	got := detected(t, "bash -c 'cd "+main+" && make ci'", directory, options{})

	want := []detect.Match{
		{Index: 0, Text: "make ci", Directory: main, ShellDirectory: directory, Entry: makeCi},
		{Index: 2, Text: "make ci", Directory: main, ShellDirectory: main, Entry: makeCi},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matched %#v, want %#v", got, want)
	}
}

func TestMatchOfAnObjectEntryCarriesItsPatternsAndFlags(t *testing.T) {
	main := checkout(t, filepath.Join(t.TempDir(), "main"), map[string]any{"command": "make ci", "red": []string{"^First red in "}, "exclusive": true})

	got := detected(t, "make ci", main, options{})

	want := []detect.Match{{Index: 0, Text: "make ci", Directory: main, ShellDirectory: main, Entry: &lists.Entry{
		Words: []string{"make", "ci"}, Red: []*regexp.Regexp{regexp.MustCompile("^First red in ")}, Exclusive: true,
	}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matched %#v, want %#v", got, want)
	}
}

// test_heavy_detect.py:176-184
func TestComposeOptionsNameTheProject(t *testing.T) {
	directory := resolved(t, t.TempDir())
	main := checkout(t, filepath.Join(directory, "main"))
	for _, option := range []string{"--project-directory " + main, "--project-directory=" + main, "-f " + main + "/compose.yaml", "--file " + main + "/compose.yaml"} {
		command := "docker compose " + option + " run --rm --no-deps web pnpm exec vitest run"

		got := detected(t, command, directory, options{})

		want := []detect.Match{{Index: 0, Text: "vitest", Directory: main, ShellDirectory: directory}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q matched %#v, want %#v", command, got, want)
		}
	}
}

// test_heavy_detect.py:187
func TestComposeFollowOptionAfterTheSubcommandNamesNoProject(t *testing.T) {
	directory := resolved(t, t.TempDir())
	main := checkout(t, filepath.Join(directory, "main"))

	got := detected(t, "docker compose exec web sh -c 'tail -f "+directory+"/log; pnpm test'", main, options{})

	want := []detect.Match{{Index: 0, Text: "pnpm test", Directory: main, ShellDirectory: main}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matched %#v, want %#v", got, want)
	}
}

// test_heavy_detect.py:195
func TestComposeEnvironmentNamesTheProject(t *testing.T) {
	directory := resolved(t, t.TempDir())
	main := checkout(t, filepath.Join(directory, "main"))
	for _, environment := range []map[string]string{
		{"COMPOSE_PROJECT_DIRECTORY": main},
		{"COMPOSE_FILE": main + "/compose.yaml:" + directory + "/extra.yaml"},
	} {
		got := directories(detected(t, "docker compose exec web pnpm test", directory, options{environment: environment}))

		if !reflect.DeepEqual(got, []string{main}) {
			t.Errorf("with %v matched in %q, want %s", environment, got, main)
		}
	}
}

// test_heavy_detect.py:203
func TestDirectoryOptionsNameTheProject(t *testing.T) {
	directory := resolved(t, t.TempDir())
	main := checkout(t, filepath.Join(directory, "main"), "make ci")
	for _, command := range []string{"pnpm -C %s test", "pnpm --dir=%s test", "uv --directory %s run pytest", "composer -d %s test", "make -C %s ci"} {
		call := strings.ReplaceAll(command, "%s", main)

		if got := directories(detected(t, call, directory, options{})); !reflect.DeepEqual(got, []string{main}) {
			t.Errorf("%q matched in %q, want %s", call, got, main)
		}
	}
}

// test_heavy_detect.py:213
func TestDirectoryFromAVariableKeepsTheKnownOne(t *testing.T) {
	main := checkout(t, filepath.Join(t.TempDir(), "main"))
	for _, command := range []string{
		"D=/sites/other; docker compose --project-directory $D exec -T web pnpm build",
		"docker compose -f ${ROOT}/compose.yaml exec -T web pnpm build",
		"pnpm -C $WEB build",
		"cd $D && pnpm build",
	} {
		found := detected(t, command, main, options{})

		if got := [2][]string{texts(found), directories(found)}; !reflect.DeepEqual(got, [2][]string{{"pnpm build"}, {main}}) {
			t.Errorf("%q matched %q, want pnpm build in %s", command, got, main)
		}
	}
}

// test_heavy_detect.py:228
func TestDockerExecContainerNamesTheProjectByItsComposeLabel(t *testing.T) {
	directory := resolved(t, t.TempDir())
	main := checkout(t, filepath.Join(directory, "main"))
	command := "docker exec -w /app/apps/web main-web-1 sh -c 'pnpm exec vitest run --project unit'"

	got := detected(t, command, directory, options{containers: map[string]string{"main-web-1": main}})

	want := []detect.Match{{Index: 0, Text: "vitest", Directory: main, ShellDirectory: directory}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matched %#v, want %#v", got, want)
	}
}

// test_heavy_detect.py:237
func TestDockerExecContainerOfAProjectWithHeavyOffPasses(t *testing.T) {
	directory := resolved(t, t.TempDir())
	other := checkout(t, filepath.Join(directory, "other"), "none")
	containers := map[string]string{"other-web-1": other}

	for _, command := range []string{"docker exec other-web-1 pnpm exec vitest run", "docker exec gone-web-1 pnpm exec vitest run"} {
		if got := detected(t, command, directory, options{containers: containers}); len(got) != 0 {
			t.Errorf("%q matched %#v, want nothing", command, got)
		}
	}
}

// test_heavy_detect.py:244
func TestLightCallsAndCallsThroughHeavyPass(t *testing.T) {
	main := checkout(t, filepath.Join(t.TempDir(), "main"), "make ci")

	for _, command := range []string{"pnpm exec vitest run tests/a.spec.ts", "heavy make ci", "/opt/heavy/bin/heavy pnpm test", "echo make ci"} {
		if got := detected(t, command, main, options{}); len(got) != 0 {
			t.Errorf("%q matched %#v, want nothing", command, got)
		}
	}
}

// test_heavy_detect.py:291
func TestShellThatOnlyReadsAScriptRunsNothing(t *testing.T) {
	main := checkout(t, filepath.Join(t.TempDir(), "main"), "checkall.sh", "infra-test.sh", "make ci")

	for _, command := range []string{
		"bash -n tests/Complete/checkall.sh",
		"sh -n tools/ci/infra-test.sh",
		"bash -xn tests/Complete/checkall.sh",
		"bash -o noexec tests/Complete/checkall.sh",
		"bash -n -c 'make ci'",
	} {
		if got := detected(t, command, main, options{}); len(got) != 0 {
			t.Errorf("%q matched %#v, want nothing", command, got)
		}
	}
}

// test_heavy_detect.py:307
func TestShellThatTracesAScriptRunsIt(t *testing.T) {
	main := checkout(t, filepath.Join(t.TempDir(), "main"), "checkall.sh")

	if got := texts(detected(t, "bash -x tests/Complete/checkall.sh", main, options{})); !reflect.DeepEqual(got, []string{"checkall.sh"}) {
		t.Fatalf("matched %q, want checkall.sh", got)
	}
}

func TestBrokenListFailsTheDetection(t *testing.T) {
	main := checkout(t, filepath.Join(t.TempDir(), "main"))
	if err := os.WriteFile(filepath.Join(main, ".heavy.json"), []byte(`{"commands": [1]}`), 0o644); err != nil {
		t.Fatalf("writing the list: %v", err)
	}
	detector := detect.NewDetector(noPersonal(), nil, nil)

	_, err := detector.Heavy(shell.FromArgv([]string{"make", "ci"}), main)

	want := "heavy: " + filepath.Join(main, ".heavy.json") + ": expected " + lists.ProjectListForm + `, got {"commands":[1]}`
	if err == nil || err.Error() != want {
		t.Fatalf("Heavy error = %v, want %q", err, want)
	}
}
