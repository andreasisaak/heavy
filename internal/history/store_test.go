package history_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/history"
)

func record(label, checkout string, ended time.Time, peak int64) history.Record {
	return history.Record{
		ID: "r-" + label, Label: label, Command: label + " run", Checkout: checkout, Directory: checkout, Session: "s", Agent: "",
		Since: ended.Add(-time.Minute).Unix(), Started: ended.Add(-30 * time.Second).Unix(), Ended: ended.Unix(),
		ExitStatus: 0, Reason: "passed", PeakRSSBytes: peak, Budget: 18, PausedSeconds: 0, Restarts: 0,
	}
}

func TestAppendWritesOneLinePerRunIntoTheFileOfItsDay(t *testing.T) {
	directory := t.TempDir()
	ended := time.Date(2026, 11, 7, 23, 30, 0, 0, time.Local)

	if err := history.Append(directory, record("ci", "/sites/main", ended, 42)); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(directory, "2026-11-07.jsonl"))
	want := `{"id":"r-ci","label":"ci","command":"ci run","checkout":"/sites/main","directory":"/sites/main","session":"s",` +
		`"agent":"","since":` + itoa(ended.Add(-time.Minute).Unix()) + `,"started":` + itoa(ended.Add(-30*time.Second).Unix()) +
		`,"ended":` + itoa(ended.Unix()) + `,"exit_status":0,"reason":"passed","peak_rss_bytes":42,"budget":18,"paused_seconds":0,"restarts":0}` + "\n"
	if err != nil || string(data) != want {
		t.Fatalf("history file = %q, %v; want %q", data, err, want)
	}
}

func TestPredictTakesTheHighestOfTheLastFivePeaks(t *testing.T) {
	directory := t.TempDir()
	start := time.Date(2026, 11, 6, 10, 0, 0, 0, time.Local)
	peaks := []int64{900, 100, 200, 300, 400, 500}
	for index, peak := range peaks {
		if err := history.Append(directory, record("ci", "/sites/main", start.Add(time.Duration(index)*12*time.Hour), peak)); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}
	if err := history.Append(directory, record("ci", "/sites/other", start, 5000)); err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	if err := history.Append(directory, record("test", "/sites/main", start, 7000)); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	got, known, err := history.Predict(directory, "ci run", "ci", "/sites/main")

	if err != nil || !known || got != 500 {
		t.Fatalf("Predict = %d, %v, %v; want 500 from the last five", got, known, err)
	}
}

func TestPredictWithoutARecordIsUnknown(t *testing.T) {
	got, known, err := history.Predict(filepath.Join(t.TempDir(), "history"), "ci run", "ci", "/sites/main")

	if err != nil || known || got != 0 {
		t.Fatalf("Predict = %d, %v, %v; want unknown", got, known, err)
	}
}

func TestBrokenHistoryLineIsAnErrorWithFileAndLine(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "2026-11-07.jsonl")
	line := strings.TrimSpace(`{"id":"x","label":"ci"}`)
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	_, _, err := history.Predict(directory, "ci run", "ci", "/sites/main")

	want := "heavy: " + path + ":1: expected a history record with every field of design G11, got " + line
	if err == nil || err.Error() != want {
		t.Fatalf("Predict error = %v, want %q", err, want)
	}
}

func TestRecordWithAnUnknownFieldIsAnError(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "2026-11-07.jsonl")
	if err := history.Append(directory, record("ci", "/sites/main", time.Date(2026, 11, 7, 12, 0, 0, 0, time.Local), 1)); err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	data, _ := os.ReadFile(path)
	broken := strings.Replace(strings.TrimSpace(string(data)), `"restarts":0`, `"restarts":0,"extra":1`, 1)
	if err := os.WriteFile(path, []byte(broken+"\n"), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	_, _, err := history.Predict(directory, "ci run", "ci", "/sites/main")

	want := "heavy: " + path + `:1: json: unknown field "extra"`
	if err == nil || err.Error() != want {
		t.Fatalf("Predict error = %v, want %q", err, want)
	}
}

func TestAppendIntoAFolderThatCannotBeCreatedIsAnError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatalf("writing %s: %v", file, err)
	}

	err := history.Append(filepath.Join(file, "history"), record("ci", "/sites/main", time.Now(), 1))

	want := "heavy: creating the history folder " + filepath.Join(file, "history") + ": mkdir " + file + ": not a directory"
	if err == nil || err.Error() != want {
		t.Fatalf("Append error = %v, want %q", err, want)
	}
}

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}

func TestRecordsWithoutAMeasuredPeakPredictNothing(t *testing.T) {
	directory := t.TempDir()
	if err := history.Append(directory, record("ci", "/sites/main", time.Date(2026, 11, 7, 12, 0, 0, 0, time.Local), 0)); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	got, known, err := history.Predict(directory, "ci run", "ci", "/sites/main")

	if err != nil || known || got != 0 {
		t.Fatalf("Predict = %d, %v, %v; want unknown, since no peak was measured", got, known, err)
	}
}

func TestRecordOfALongCommandReadsBack(t *testing.T) {
	directory := t.TempDir()
	long := record("ci", "/sites/main", time.Date(2026, 11, 7, 12, 0, 0, 0, time.Local), 7)
	long.Command = strings.Repeat("x", 200<<10)
	if err := history.Append(directory, long); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	got, known, err := history.Predict(directory, long.Command, "ci", "/sites/main")

	if err != nil || !known || got != 7 {
		t.Fatalf("Predict = %d, %v, %v; want 7", got, known, err)
	}
}

// 2026-11-09: a measurement of seven test servers and `make test-complete TEST_WORKERS=1` share the label
// test-complete; the 10.7 GiB peak of the one kept the light one from ever starting beside another run
func TestPredictPrefersTheRunsOfTheSameCommand(t *testing.T) {
	directory := t.TempDir()
	start := time.Date(2026, 11, 9, 0, 0, 0, 0, time.Local)
	runs := []struct {
		command string
		peak    int64
	}{{"measure seven servers", 10 << 30}, {"make test-complete TEST_WORKERS=1", 600}, {"make test-complete TEST_WORKERS=1", 400}}
	for index, current := range runs {
		entry := record("test-complete", "/sites/main", start.Add(time.Duration(index)*time.Minute), current.peak)
		entry.Command = current.command
		if err := history.Append(directory, entry); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	same, sameKnown, sameErr := history.Predict(directory, "make test-complete TEST_WORKERS=1", "test-complete", "/sites/main")
	other, otherKnown, otherErr := history.Predict(directory, "make test-complete TEST_WORKERS=3", "test-complete", "/sites/main")

	if sameErr != nil || !sameKnown || same != 600 {
		t.Fatalf("Predict of a known command = %d, %v, %v; want 600 from its own runs", same, sameKnown, sameErr)
	}
	if otherErr != nil || !otherKnown || other != 10<<30 {
		t.Fatalf("Predict of a new command = %d, %v, %v; want the 10 GiB of its label", other, otherKnown, otherErr)
	}
}
