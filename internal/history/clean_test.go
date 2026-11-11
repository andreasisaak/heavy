package history_test

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/history"
)

func TestCleanDeletesFilesOlderThanThirtyDays(t *testing.T) {
	directory := t.TempDir()
	now := time.Date(2026, 11, 8, 9, 0, 0, 0, time.Local)
	for _, days := range []int{31, 30, 29, 0} {
		name := now.AddDate(0, 0, -days).Format("2006-01-02") + ".jsonl"
		if err := os.WriteFile(filepath.Join(directory, name), nil, 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	if err := history.Clean(directory, now); err != nil {
		t.Fatalf("Clean failed: %v", err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("reading %s: %v", directory, err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	want := []string{"2026-10-09.jsonl", "2026-10-10.jsonl", "2026-11-08.jsonl"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("history holds %q, want %q", names, want)
	}
}

func TestCleanOfAMissingFolderDoesNothing(t *testing.T) {
	if err := history.Clean(filepath.Join(t.TempDir(), "history"), time.Now()); err != nil {
		t.Fatalf("Clean failed: %v", err)
	}
}

func TestCleanOfAFileWithoutADateIsAnError(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "notes.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	err := history.Clean(directory, time.Now())

	want := `heavy: ` + path + `: expected a history file named YYYY-MM-DD.jsonl: parsing time "notes" as "2006-01-02": cannot parse "notes" as "2006"`
	if err == nil || err.Error() != want {
		t.Fatalf("Clean error = %v, want %q", err, want)
	}
}

// critic 2.5: a dated entry without the suffix names the expected name and nothing else
func TestCleanOfADatedFileWithoutTheSuffixIsAnError(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "2026-01-01")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}

	err := history.Clean(directory, time.Now())

	want := `heavy: ` + path + `: expected a history file named YYYY-MM-DD.jsonl`
	if err == nil || err.Error() != want {
		t.Fatalf("Clean error = %v, want %q", err, want)
	}
}
