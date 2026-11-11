// Package slots limits the runs of a whole machine across the daemons of its users: the machine configuration
// names how many runs may be active at once and a folder with one lock file per slot, and each daemon holds the
// lock of one slot for each of its active runs. The kernel frees the lock of a daemon that dies.
package slots

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// form is the shape the machine configuration must have, as error messages name it.
const form = `{"max_parallel": 8, "slots": "/var/lib/heavy/slots"} with both keys, "max_parallel" a whole number from 1 and "slots" an absolute folder`

// Config is the machine configuration: at most MaxParallel runs are active on the machine, over all users, and
// Folder holds the slot files. Path is the file it came from.
type Config struct {
	Path        string
	MaxParallel int
	Folder      string
}

// ConfigPath is $HEAVY_MACHINE_CONFIG, or /etc/heavy/config.json.
func ConfigPath() string {
	if path := os.Getenv("HEAVY_MACHINE_CONFIG"); path != "" {
		return path
	}
	return "/etc/heavy/config.json"
}

// Load reads the machine configuration; found is false without the file, which leaves each user's own limit alone.
func Load(path string) (config Config, found bool, err error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, fmt.Errorf("heavy: reading the machine configuration %s: %w", path, err)
	}
	var document struct {
		MaxParallel *int    `json:"max_parallel"`
		Slots       *string `json:"slots"`
	}
	// A key this heavy does not know is left out: a newer heavy on the same machine may know it.
	decoder := json.NewDecoder(bytes.NewReader(content))
	shape := fmt.Errorf("heavy: %s: expected %s, got %s", path, form, bytes.TrimSpace(content))
	if err := decoder.Decode(&document); err != nil {
		return Config{}, false, shape
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Config{}, false, shape
	}
	if document.MaxParallel == nil || *document.MaxParallel < 1 || document.Slots == nil || !filepath.IsAbs(*document.Slots) {
		return Config{}, false, shape
	}
	return Config{Path: path, MaxParallel: *document.MaxParallel, Folder: *document.Slots}, true, nil
}
