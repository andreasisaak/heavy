package stophook

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// repeatWindow is how long after a denied stop the same stop passes.
const repeatWindow = 600 * time.Second

// repeatedStop reports whether the stop of task repeats one denied at most 10 minutes ago, and then removes the
// marker of that denial; otherwise it writes the marker of this stop, the Unix time, to stops/<task>.
func repeatedStop(stops, task string, now time.Time) (bool, error) {
	if err := os.MkdirAll(stops, 0o700); err != nil {
		return false, fmt.Errorf("heavy: creating the stop markers directory: %w", err)
	}
	marker := filepath.Join(stops, task)
	content, err := os.ReadFile(marker)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return false, fmt.Errorf("heavy: reading the stop marker: %w", err)
	default:
		denied, err := strconv.ParseInt(string(content), 10, 64)
		if err != nil {
			return false, fmt.Errorf("heavy: %s: expected the Unix time of the denied stop, got %q; remove the file to reset it", marker, content)
		}
		if now.Sub(time.Unix(denied, 0)) <= repeatWindow {
			if err := os.Remove(marker); err != nil {
				return false, fmt.Errorf("heavy: removing the stop marker: %w", err)
			}
			return true, nil
		}
	}
	if err := os.WriteFile(marker, []byte(strconv.FormatInt(now.Unix(), 10)), 0o600); err != nil {
		return false, fmt.Errorf("heavy: writing the stop marker: %w", err)
	}
	return false, nil
}
