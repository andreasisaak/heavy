package history

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// keptDays is how long history files stay (design G11).
const keptDays = 30

// Clean deletes the history files of days more than 30 days before now.
func Clean(directory string, now time.Time) error {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("heavy: reading the history folder %s: %w", directory, err)
	}
	year, month, day := now.Date()
	cutoff := time.Date(year, month, day, 0, 0, 0, 0, time.Local).AddDate(0, 0, -keptDays)
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		if !strings.HasSuffix(entry.Name(), ".jsonl") {
			return fmt.Errorf("heavy: %s: expected a history file named YYYY-MM-DD.jsonl", path)
		}
		date, err := time.ParseInLocation("2006-01-02", strings.TrimSuffix(entry.Name(), ".jsonl"), time.Local)
		if err != nil {
			return fmt.Errorf("heavy: %s: expected a history file named YYYY-MM-DD.jsonl: %w", path, err)
		}
		if date.Before(cutoff) {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("heavy: deleting the old history file %s: %w", path, err)
			}
		}
	}
	return nil
}
