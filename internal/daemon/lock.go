package daemon

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// errHeld says another daemon holds the lock.
var errHeld = errors.New("another daemon holds the lock")

// acquireLock takes the lock of the one daemon and writes this daemon's pid into it. While another daemon
// holds it, the error is errHeld and holder is that daemon's pid, 0 when it wrote none yet.
func acquireLock(path string) (lock *os.File, holder int, err error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, 0, fmt.Errorf("heavy: opening the daemon lock: %w", err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		content, _ := os.ReadFile(path)
		file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			holder, _ := strconv.Atoi(strings.TrimSpace(string(content)))
			return nil, holder, errHeld
		}
		return nil, 0, fmt.Errorf("heavy: locking %s: %w", path, err)
	}
	if err := file.Truncate(0); err != nil {
		file.Close()
		return nil, 0, fmt.Errorf("heavy: clearing %s: %w", path, err)
	}
	if _, err := file.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0); err != nil {
		file.Close()
		return nil, 0, fmt.Errorf("heavy: writing the pid into %s: %w", path, err)
	}
	return file, 0, nil
}

// lockFree reports whether no daemon holds the lock at path, so a client may start one.
func lockFree(path string) (bool, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return false, fmt.Errorf("heavy: opening the daemon lock: %w", err)
	}
	defer file.Close()
	err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("heavy: locking %s: %w", path, err)
	}
	return true, unix.Flock(int(file.Fd()), unix.LOCK_UN)
}
