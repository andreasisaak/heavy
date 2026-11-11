package herdr

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"syscall"
)

// versionLine starts the line of the manifest that names the heavy version that wrote it.
const versionLine = "version = "

// manifestVersion reads the version from the line `version = "1.0.1"` heavy writes into the manifest at path.
func manifestVersion(path string, data []byte) ([]int, error) {
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, versionLine) {
			continue
		}
		var version string
		if json.Unmarshal([]byte(strings.TrimPrefix(line, versionLine)), &version) == nil {
			if numbers, valid := versionNumbers(version); valid {
				return numbers, nil
			}
		}
		return nil, fmt.Errorf(`heavy: %s: expected a line version = "<numbers separated by dots>", got %s`, path, line)
	}
	return nil, fmt.Errorf(`heavy: %s: expected a line version = "<numbers separated by dots>", got no version line`, path)
}

// commandLine starts the lines of the manifest that name a command; the first runs the heavy that wrote it.
const commandLine = "command = "

// manifestHeavy reads the heavy binary from the first line `command = ["/…/bin/heavy", "--herdr-refresh"]` heavy
// writes into the manifest at path; heavy's TOML strings are JSON strings too.
func manifestHeavy(path string, data []byte) (string, error) {
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, commandLine) {
			continue
		}
		var command []string
		if json.Unmarshal([]byte(strings.TrimPrefix(line, commandLine)), &command) == nil && len(command) > 0 && command[0] != "" {
			return command[0], nil
		}
		return "", fmt.Errorf(`heavy: %s: expected a line command = ["<heavy binary>", ...], got %s`, path, line)
	}
	return "", fmt.Errorf(`heavy: %s: expected a line command = ["<heavy binary>", ...], got no command line`, path)
}

// gone reports whether the heavy binary at path no longer exists, as after Claude Code removed its version.
func gone(path string) (bool, error) {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		return true, nil
	}
	return false, fmt.Errorf("heavy: checking the heavy binary of the herdr plugin: %w", err)
}
