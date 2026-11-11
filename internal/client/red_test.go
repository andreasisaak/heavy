package client_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const redLine = "First red in PHPUnit at 12:00:00: a"

// listRed puts the entry for the program redtest into the personal list for the checkout.
func listRed(t *testing.T, checkout, entry string) {
	t.Helper()
	configuration := filepath.Join(os.Getenv("HOME"), ".config", "heavy", "config.json")
	content := fmt.Sprintf(`{"projects": {%q: {"enabled": true, "commands": [%s]}}, "load": {"host_pressure_percent": 0, "host_recovered_percent": 0, "reserve_bytes": 0, "load_per_core": 1000, "swap_bytes_per_2s": 1000000000000}}`, checkout, entry)
	if err := os.WriteFile(configuration, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// plan 3.4: a line that matches a red pattern of the entry ends the run; heavy exits 1 within 11 s and names the line
// last, and the history counts the run as red
func TestRedLineOnStdoutStopsTheRun(t *testing.T) {
	for name, target := range map[string]string{"stdout": "", "stderr": " >&2"} {
		t.Run(name, func(t *testing.T) {
			e := setup(t)
			repo := repository(t)
			listRed(t, repo, `{"command": "redtest", "red": ["^First red in "]}`)
			e.fake(t, "redtest", `printf 'ok\n`+redLine+`\n'`+target+`; exec sleep 100.41`)
			killMarked(t, "100.41")
			begin := time.Now()

			run := e.start(t, repo, "redtest")
			status := run.wait(t)

			lines := strings.Split(strings.TrimSuffix(run.stdout.String(), "\n"), "\n")
			pattern := `^heavy: red at \d{2}:\d{2}:\d{2}, stopped the run: ` + regexp.QuoteMeta(redLine) + `$`
			if status != 1 || time.Since(begin) > 11*time.Second || !regexp.MustCompile(pattern).MatchString(lines[len(lines)-1]) {
				t.Fatalf("exit %d after %s, stdout %q; want 1 within 11 s and the red line last", status, time.Since(begin), run.stdout.String())
			}
			files, _ := filepath.Glob(filepath.Join(e.state, "history", "*.jsonl"))
			data, _ := os.ReadFile(files[0])
			if !strings.Contains(string(data), `"reason":"red"`) {
				t.Fatalf("history %s, want the run counted as red", data)
			}
		})
	}
}

// plan 3.4: the patterns are anchored as written, so a line with the words further in stops nothing
func TestLineThatOnlyHoldsThePatternFurtherInStopsNothing(t *testing.T) {
	e := setup(t)
	repo := repository(t)
	listRed(t, repo, `{"command": "redtest", "red": ["^First red in "]}`)
	e.fake(t, "redtest", `printf 'x`+redLine+`\n'; exit 0`)

	run := e.start(t, repo, "redtest")

	if status := run.wait(t); status != 0 || strings.Contains(run.stdout.String(), "heavy: red at") {
		t.Fatalf("exit %d, stdout %q; want 0 without a red stop", status, run.stdout.String())
	}
}

// plan 3.4: an entry without red patterns never stops a run
func TestEntryWithoutRedPatternsNeverStops(t *testing.T) {
	e := setup(t)
	repo := repository(t)
	listRed(t, repo, `"redtest"`)
	e.fake(t, "redtest", `printf '`+redLine+`\n'; exit 0`)

	run := e.start(t, repo, "redtest")

	if status := run.wait(t); status != 0 || strings.Contains(run.stdout.String(), "heavy: red at") {
		t.Fatalf("exit %d, stdout %q, stderr %q; want 0 without a red stop", status, run.stdout.String(), run.stderr.String())
	}
}

// plan 3.4: progress without a line end passes through at once while the patterns watch the output
func TestProgressWithoutALineEndPassesThroughAtOnce(t *testing.T) {
	e := setup(t)
	repo := repository(t)
	listRed(t, repo, `{"command": "redtest", "red": ["^First red in "]}`)
	e.fake(t, "redtest", `printf '10%%\r20%%\r'; exec sleep 100.42`)
	killMarked(t, "100.42")

	run := e.start(t, repo, "redtest")

	run.waitStdout(t, `10%\r20%\r$`)
	if run.finished() {
		t.Fatalf("the run ended before its progress showed")
	}
}

// critic 3.4: a command that prints a red line and ends at once still ends red, whichever the client sees first
func TestRedLineRightBeforeTheEndStillEndsRed(t *testing.T) {
	e := setup(t)
	repo := repository(t)
	listRed(t, repo, `{"command": "redtest", "red": ["^First red in "]}`)
	e.fake(t, "redtest", `printf '`+redLine+`\n'; exit 0`)

	for attempt := 0; attempt < 10; attempt++ {
		run := e.start(t, repo, "redtest")
		if status := run.wait(t); status != 1 || !strings.Contains(run.stdout.String(), "stopped the run: "+redLine+"\n") {
			t.Fatalf("attempt %d: exit %d, stdout %q; want 1 and the red line", attempt, status, run.stdout.String())
		}
	}
	files, _ := filepath.Glob(filepath.Join(e.state, "history", "*.jsonl"))
	data, _ := os.ReadFile(files[0])
	if got := strings.Count(string(data), `"reason":"red"`); got != 10 {
		t.Fatalf("history %s holds %d red records, want 10", data, got)
	}
}
