package shim_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// holder is a `heavy <command>` client running in the background.
type holder struct {
	command *exec.Cmd
	output  *lockedBuffer
	done    chan struct{}
}

type lockedBuffer struct {
	mutex sync.Mutex
	data  bytes.Buffer
}

func (b *lockedBuffer) Write(chunk []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.data.Write(chunk)
}

func (b *lockedBuffer) String() string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.data.String()
}

// start runs path with the arguments in directory and ends it with SIGTERM at the end of the test, which heavy
// passes on to its command.
func start(t *testing.T, path, directory string, environment []string, arguments ...string) *holder {
	t.Helper()
	command := exec.Command(path, arguments...)
	command.Dir, command.Env = directory, environment
	output := &lockedBuffer{}
	command.Stdout, command.Stderr = output, output
	if err := command.Start(); err != nil {
		t.Fatalf("starting %s %q: %v", path, arguments, err)
	}
	started := &holder{command: command, output: output, done: make(chan struct{})}
	go func() {
		_ = command.Wait()
		close(started.done)
	}()
	t.Cleanup(func() { started.stop(t) })
	return started
}

func (h *holder) stop(t *testing.T) {
	t.Helper()
	select {
	case <-h.done:
		return
	default:
	}
	_ = h.command.Process.Signal(syscall.SIGTERM)
	select {
	case <-h.done:
	case <-time.After(patience):
		_ = h.command.Process.Kill()
		t.Errorf("%s did not end within %s of SIGTERM; output %q", h.command.Args, patience, h.output.String())
	}
}

func (h *holder) wait(t *testing.T) int {
	t.Helper()
	select {
	case <-h.done:
		return h.command.ProcessState.ExitCode()
	case <-time.After(patience):
		t.Fatalf("%s did not end within %s; output %q", h.command.Args, patience, h.output.String())
		return -1
	}
}

// activeRun starts `heavy sleep <seconds>` in the project and returns its entry once the daemon reports it running,
// and its client.
func (f *fixture) activeRun(t *testing.T, seconds string) (entry, *holder) {
	t.Helper()
	client := start(t, f.heavy, f.project, f.environment(), "sleep", seconds)
	var found entry
	eventually(t, "heavy sleep "+seconds+" runs", func() bool {
		for _, candidate := range f.status(t) {
			if candidate.Command == "sleep "+seconds && candidate.State == "running" {
				found = candidate
				return true
			}
		}
		return false
	})
	return found, client
}

// entry is one run of `heavy --status --json`.
type entry struct {
	State     string `json:"state"`
	Directory string `json:"directory"`
	Command   string `json:"command"`
	ID        string `json:"id"`
	Budget    int    `json:"budget"`
	Position  int    `json:"position"`
}

func (f *fixture) status(t *testing.T) []entry {
	t.Helper()
	result := run(t, f.heavy, f.scratch, f.environment(), "", "--status", "--json")
	if result.exitStatus != 0 {
		t.Fatalf("heavy --status --json = %+v", result)
	}
	var entries []entry
	if err := json.Unmarshal([]byte(result.stdout), &entries); err != nil {
		t.Fatalf("heavy --status --json printed %q: %v", result.stdout, err)
	}
	return entries
}

func eventually(t *testing.T, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(patience)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("not within %s: %s", patience, description)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// stopDaemon ends the daemon of the state directory, if one runs, and waits for its end.
func stopDaemon(t *testing.T, state string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(state, "daemon.lock"))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Errorf("reading daemon.lock: %v", err)
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil || syscall.Kill(pid, syscall.SIGTERM) != nil {
		return
	}
	deadline := time.Now().Add(10 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Errorf("daemon %d did not end within 10 s of SIGTERM", pid)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}
