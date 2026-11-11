package testkit

import (
	"os"
	"testing"
)

// StateDir creates an empty state directory, points HEAVY_STATE_DIRECTORY at it for the test
// and returns its path, so no test touches the real queue in ~/.cache/heavy.
// The directory sits directly in the temporary directory instead of t.TempDir(): the path of
// the daemon socket inside it has to stay below the 104 bytes macOS allows for socket paths.
func StateDir(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp("", "heavy-state-")
	if err != nil {
		t.Fatalf("creating a state directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Errorf("removing state directory %s: %v", directory, err)
		}
	})
	t.Setenv("HEAVY_STATE_DIRECTORY", directory)
	return directory
}

// Home creates an empty home directory, points HOME at it for the test and returns its path,
// so no test reads the personal list ~/.config/heavy/config.json of the user.
func Home(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	t.Setenv("HOME", directory)
	return directory
}
