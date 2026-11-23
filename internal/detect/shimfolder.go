package detect

import (
	"os"
	"path/filepath"
)

// ShimFolder reports whether a folder holds the shims of a heavy: links to a binary named heavy, as since 1.0.0, or
// the script heavy-shim of heavy 0.x. A link counts by the name it points at, so the shims of a version whose
// binary is gone count too. A folder that is gone or cannot be read holds none.
func ShimFolder(directory string) bool {
	if _, err := os.Stat(filepath.Join(directory, zeroShimScript)); err == nil {
		return true
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		if target, err := os.Readlink(filepath.Join(directory, entry.Name())); err == nil && filepath.Base(target) == "heavy" {
			return true
		}
	}
	return false
}
