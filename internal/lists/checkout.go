package lists

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Checkout is the root of a git checkout and its main checkout, the same for all its worktrees.
type Checkout struct {
	Toplevel string
	Main     string
}

// FindCheckout returns the checkout that holds directory; found is false for a directory that
// does not exist or lies in no git checkout. git missing or failing to start is an error.
func FindCheckout(directory string) (checkout Checkout, found bool, err error) {
	if information, err := os.Stat(directory); err != nil || !information.IsDir() {
		return Checkout{}, false, nil
	}
	// The directory goes in as the working directory instead of `git -C`: a git wrapper on PATH sees rev-parse
	// first and can pass it on at once, as the safety wrapper in ~/.local/bin does without starting Python.
	command := exec.Command("git", "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
	command.Dir = directory
	output, err := command.Output()
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return Checkout{}, false, nil
	}
	if err != nil {
		return Checkout{}, false, failure("git failed in %s: %v", directory, err)
	}
	lines := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
	if len(lines) != 2 {
		return Checkout{}, false, failure("git rev-parse in %s printed %q, expected the toplevel and the common directory", directory, output)
	}
	return Checkout{Toplevel: realPath(lines[0]), Main: realPath(filepath.Dir(lines[1]))}, true, nil
}

// realPath resolves the symbolic links of the part of path that exists and keeps the rest, as os.path.realpath did.
func realPath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	var missing []string
	for current := absolute; ; current = filepath.Dir(current) {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			return filepath.Join(append([]string{resolved}, missing...)...)
		}
		if filepath.Dir(current) == current {
			return absolute
		}
		missing = append([]string{filepath.Base(current)}, missing...)
	}
}
