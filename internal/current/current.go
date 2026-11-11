// Package current keeps $STATE/current, a link to the plugin folder of the newest heavy on this machine. The
// SessionStart hook puts $STATE/current/shims in front of PATH: a path without a version in it, so a session that
// runs over an update of heavy reaches the shims of the new version without a restart. The plugin folder itself
// carries the version in its path (…/cache/heavy/heavy/<version>), and a session never gets a new PATH.
package current

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// Shims is the shims folder behind the link.
func Shims(state string) string {
	return filepath.Join(state, "current", "shims")
}

// Point lets the link name the plugin folder root when root holds a newer heavy than the folder the link names,
// when the link names no plugin folder any more, or, at a session start, when root holds another build of the
// same version. An older heavy never takes the link back, and a hook that runs with every call (starting false)
// takes it only for a newer version, so two builds of one version do not take it from each other call by call.
func Point(state, root string, starting bool) error {
	// A heavy outside a plugin folder, such as one built for a test, has no shims to offer.
	if _, err := os.Stat(filepath.Join(root, "shims")); err != nil {
		return nil
	}
	link := filepath.Join(state, "current")
	linked, err := os.Readlink(link)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return replace(state, link, root)
	case errors.Is(err, syscall.EINVAL):
		return fmt.Errorf("heavy: %s is no link to a plugin folder of heavy; remove it, heavy makes the link again", link)
	case err != nil:
		return fmt.Errorf("heavy: reading the link to the plugin folder of heavy: %w", err)
	case linked == root:
		return nil
	}
	if _, err := os.Stat(filepath.Join(linked, "shims")); errors.Is(err, fs.ErrNotExist) {
		return replace(state, link, root)
	}
	mine, err := protocol.LocalIdentity(filepath.Join(root, "bin", "heavy"))
	if err != nil {
		return err
	}
	theirs, err := protocol.LocalIdentity(filepath.Join(linked, "bin", "heavy"))
	if err != nil {
		return replace(state, link, root)
	}
	installed := ""
	if starting {
		installed = mine.Stamp
	}
	if mine.Newer(theirs, installed) {
		return replace(state, link, root)
	}
	return nil
}

// replace puts the link in place in one step, so a shell that looks a program up never finds the path without it.
func replace(state, link, root string) error {
	if err := os.MkdirAll(state, 0o700); err != nil {
		return fmt.Errorf("heavy: creating the state directory %s: %w", state, err)
	}
	temporary := link + ".new." + strconv.Itoa(os.Getpid())
	if err := os.Symlink(root, temporary); err != nil {
		return fmt.Errorf("heavy: linking %s to %s: %w", temporary, root, err)
	}
	if err := os.Rename(temporary, link); err != nil {
		os.Remove(temporary)
		return fmt.Errorf("heavy: moving the link to %s into place: %w", root, err)
	}
	return nil
}
