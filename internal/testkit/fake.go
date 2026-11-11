package testkit

import (
	"os"
	"path/filepath"
	"testing"
)

// FakeProgram writes an executable /bin/sh script named name into directory and returns its path.
// The script runs the given body, so a test decides what the fake prints and how it exits.
func FakeProgram(t *testing.T, directory, name, script string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatalf("writing fake program %s: %v", path, err)
	}
	return path
}
