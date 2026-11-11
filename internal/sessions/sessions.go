// Package sessions holds the registration of each Claude Code session under the PID of its Claude Code process,
// $STATE/sessions/<claude-pid>.json. The SessionStart hook writes it; the monitor and the daemon read it to find
// the current session of a process, which /clear changes (design G9).
package sessions

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Registration is one session: its id, working directory, main checkout and the time of its start.
type Registration struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	Checkout  string `json:"checkout"`
	Time      int64  `json:"time"`
}

// Path is the registration file of a Claude Code PID.
func Path(stateDirectory string, claudePID int) string {
	return filepath.Join(stateDirectory, "sessions", strconv.Itoa(claudePID)+".json")
}

// Write replaces the registration in one step, so a reader never sees half of it.
func Write(stateDirectory string, claudePID int, registration Registration) error {
	folder := filepath.Join(stateDirectory, "sessions")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return fmt.Errorf("heavy: registering the session in %s: %w", folder, err)
	}
	data, err := json.Marshal(registration)
	if err != nil {
		return fmt.Errorf("heavy: encoding the session registration: %w", err)
	}
	path := Path(stateDirectory, claudePID)
	if err := os.WriteFile(path+".new", data, 0o600); err != nil {
		return fmt.Errorf("heavy: registering the session in %s: %w", path, err)
	}
	if err := os.Rename(path+".new", path); err != nil {
		return fmt.Errorf("heavy: registering the session in %s: %w", path, err)
	}
	return nil
}

// Read is the registration of a Claude Code PID, decoded strictly.
func Read(stateDirectory string, claudePID int) (Registration, error) {
	path := Path(stateDirectory, claudePID)
	data, err := os.ReadFile(path)
	if err != nil {
		return Registration{}, fmt.Errorf("heavy: reading the session registration %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var registration Registration
	if err := decoder.Decode(&registration); err != nil {
		return Registration{}, fmt.Errorf("heavy: reading the session registration %s: %w", path, err)
	}
	return registration, nil
}

// Prune removes the registrations of Claude Code processes that ended and returns their PIDs; files that are no
// registration stay. A session start calls it, so the folder does not grow with every session.
func Prune(stateDirectory string, alive func(pid int) bool) ([]int, error) {
	folder := filepath.Join(stateDirectory, "sessions")
	entries, err := os.ReadDir(folder)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("heavy: listing the session registrations in %s: %w", folder, err)
	}
	var removed []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil || !strings.HasSuffix(entry.Name(), ".json") || alive(pid) {
			continue
		}
		if err := os.Remove(filepath.Join(folder, entry.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return removed, fmt.Errorf("heavy: removing the registration of ended pid %d: %w", pid, err)
		}
		removed = append(removed, pid)
	}
	return removed, nil
}

// Alive reports whether the process lives; one of another user, which no signal of this user reaches, lives too.
func Alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
