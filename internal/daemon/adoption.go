package daemon

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
)

// adoptionMark is the file a daemon that ends with runs leaves, so the next one lets their clients announce them
// first, as after a crash.
const adoptionMark = "daemon.runs"

func leaveAdoptionMark(state string, runs int, logger *slog.Logger) {
	path := filepath.Join(state, adoptionMark)
	if err := os.WriteFile(path, []byte(strconv.Itoa(runs)+"\n"), 0o600); err != nil {
		logger.Error("leaving the mark of the active runs failed", "path", path, "error", err.Error())
	}
}

// takeAdoptionMark reports and removes the mark of a daemon that ended with runs.
func takeAdoptionMark(state string, logger *slog.Logger) (bool, error) {
	path := filepath.Join(state, adoptionMark)
	err := os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("heavy: removing %s: %w", path, err)
	}
	logger.Info("the last daemon ended with runs, waiting for their clients", "window", adoptionWindow.String())
	return true, nil
}
