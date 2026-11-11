package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Identity names the build of a heavy: the plugin version and the stamp of its sources (bin/.stamp).
type Identity struct {
	Version string
	Stamp   string
}

// manifest is the part of .claude-plugin/plugin.json heavy reads.
type manifest struct {
	Version string `json:"version"`
}

// LocalIdentity reads the identity of the heavy binary at executable, which sits in <plugin>/bin. A binary
// outside a plugin, such as one built for a test, has an empty identity.
func LocalIdentity(executable string) (Identity, error) {
	root := filepath.Dir(filepath.Dir(executable))
	var identity Identity
	manifestPath := filepath.Join(root, ".claude-plugin", "plugin.json")
	data, err := os.ReadFile(manifestPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return Identity{}, fmt.Errorf("heavy: %w", err)
	default:
		var content manifest
		if err := json.Unmarshal(data, &content); err != nil {
			return Identity{}, fmt.Errorf("heavy: %s: expected a version string: %w", manifestPath, err)
		}
		identity.Version = content.Version
	}
	stamp, err := os.ReadFile(filepath.Join(root, "bin", ".stamp"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return Identity{}, fmt.Errorf("heavy: %w", err)
	default:
		identity.Stamp = strings.TrimSpace(string(stamp))
	}
	return identity, nil
}

// Newer reports whether this identity should take over from the daemon's: a higher version, or the same
// version built from other sources when that build is the one installed now, whose stamp the daemon's bin/.stamp
// holds; a heavy started before a rebuild keeps its old stamp and takes over nothing. A version that is no dotted
// number takes over nothing.
func (i Identity) Newer(daemon Identity, installedStamp string) bool {
	mine, valid := versionNumbers(i.Version)
	theirs, theirsValid := versionNumbers(daemon.Version)
	if !valid || !theirsValid {
		return false
	}
	for index := 0; index < len(mine) && index < len(theirs); index++ {
		if mine[index] != theirs[index] {
			return mine[index] > theirs[index]
		}
	}
	if len(mine) != len(theirs) {
		return len(mine) > len(theirs)
	}
	return i.Stamp != "" && i.Stamp != daemon.Stamp && i.Stamp == installedStamp
}

// versionNumbers splits a version into its numbers; an empty version has none and sorts first.
func versionNumbers(version string) ([]int, bool) {
	if version == "" {
		return nil, true
	}
	var numbers []int
	for _, part := range strings.Split(version, ".") {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return nil, false
		}
		numbers = append(numbers, number)
	}
	return numbers, true
}
