package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/client"
	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/testkit"
)

const (
	stopSession = "session-stop-hook"
	stopTask    = "bq7heavy1"
)

// stopInput is the input of a TaskStop of the task the guard queued, with a transcript that records it.
func stopInput(t *testing.T) string {
	t.Helper()
	transcript := filepath.Join(t.TempDir(), "session.jsonl")
	lines := `{"type":"attachment","attachment":{"type":"hook_additional_context","toolUseID":"toolu_ci","content":["Queued behind heavy: x"]}}` + "\n" +
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_ci","content":"Command running in background with ID: ` + stopTask + `. Output is being written to: /tmp/ci.output"}]}}` + "\n"
	if err := os.WriteFile(transcript, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(map[string]any{
		"session_id": stopSession, "transcript_path": transcript, "hook_event_name": "PreToolUse",
		"tool_name": "TaskStop", "tool_input": map[string]string{"task_id": stopTask},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func stopHook(input string, stdout *bytes.Buffer) outcome {
	var stderr bytes.Buffer
	exitStatus := runStopHook(strings.NewReader(input), stdout, &stderr)
	return outcome{exitStatus: exitStatus, stdout: stdout.String(), stderr: stderr.String()}
}

// plan 4.2: without a daemon no run of the session waits or runs, so the stop passes
func TestStopHookLetsTheStopPassWithoutADaemon(t *testing.T) {
	testkit.StateDir(t)
	testkit.Home(t)

	got := stopHook(stopInput(t), &bytes.Buffer{})

	if want := (outcome{}); got != want {
		t.Fatalf("heavy --hook stop = %+v, want %+v", got, want)
	}
}

// plan 4.2: the hook reads the queue of the running daemon and keeps its marker in the state directory
func TestStopHookDeniesTheFirstStopOfARunningRunOfTheSession(t *testing.T) {
	state := testkit.StateDir(t)
	home := testkit.Home(t)
	limits := `{"load": {"host_pressure_percent": 0, "host_recovered_percent": 0, "reserve_bytes": 0, "load_per_core": 1000, "swap_bytes_per_2s": 1000000000000}}`
	if err := os.MkdirAll(filepath.Join(home, ".config", "heavy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "heavy", "config.json"), []byte(limits), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HEAVY_SESSION", stopSession)
	t.Setenv("HERDR_PANE_ID", "")
	startRunningSleep(t, state, "30.4242")
	before := time.Now().Unix()

	got := stopHook(stopInput(t), &bytes.Buffer{})

	want := outcome{stdout: `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"Task ` + stopTask +
		" is a heavy run of this session: `sleep 30.4242` runs for 0 min. Stopping it throws away the minutes it has run and makes room for the next run. " +
		"A waiting run reads the files only when it starts, so it checks a fix made while it waits without a restart. " +
		`Stop it only if its command is wrong or the user asked for it; then stop it again within 10 minutes, and that stop passes."}}` + "\n"}
	if got != want {
		t.Fatalf("heavy --hook stop = %+v, want %+v", got, want)
	}
	content, err := os.ReadFile(filepath.Join(state, "stops", stopTask))
	if err != nil {
		t.Fatalf("reading the stop marker: %v", err)
	}
	if marked, err := strconv.ParseInt(string(content), 10, 64); err != nil || marked < before || marked > time.Now().Unix() {
		t.Fatalf("stop marker = %q, want the Unix time of the stop", content)
	}
}

// plan 4.2: a denial the hook cannot write blocks the stop through exit status 2 with the problem on stderr
func TestStopHookBlocksTheStopWhenItCannotWriteTheDenial(t *testing.T) {
	testkit.StateDir(t)
	missing := filepath.Join(t.TempDir(), "missing.jsonl")
	input := `{"session_id":"s","transcript_path":"` + missing + `","tool_name":"TaskStop","tool_input":{"task_id":"b1"}}`
	var stderr bytes.Buffer

	exitStatus := runStopHook(strings.NewReader(input), failingWriter{}, &stderr)

	if got, want := (outcome{exitStatus: exitStatus, stderr: stderr.String()}), (outcome{exitStatus: 2, stderr: "heavy: writing the hook answer: example write failure\n"}); got != want {
		t.Fatalf("heavy --hook stop = %+v, want %+v", got, want)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("example write failure") }

// startRunningSleep starts `heavy sleep <seconds>`, waits until the daemon reports it running, and ends the run
// and the daemon after the test.
func startRunningSleep(t *testing.T, state, seconds string) {
	t.Helper()
	command := exec.Command(testkit.BuildBinary(t), "sleep", seconds)
	command.Dir = t.TempDir()
	if err := command.Start(); err != nil {
		t.Fatalf("starting heavy sleep %s: %v", seconds, err)
	}
	ended := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(ended)
	}()
	t.Cleanup(func() {
		_ = command.Process.Signal(syscall.SIGTERM)
		select {
		case <-ended:
		case <-time.After(10 * time.Second):
			t.Errorf("heavy sleep %s did not end within 10 s after SIGTERM", seconds)
		}
		endDaemon(t, state)
	})
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		status, _, err := client.StatusIfRunning(daemon.ConnectOptions{StateDirectory: state})
		if err != nil {
			t.Fatalf("reading the queue: %v", err)
		}
		if len(status.Runs) == 1 && status.Runs[0].State == "running" && status.Runs[0].Session == stopSession {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("heavy sleep %s did not run for session %s within 15 s", seconds, stopSession)
}

// endDaemon ends the daemon of the state directory with SIGTERM.
func endDaemon(t *testing.T, state string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(state, "daemon.lock"))
	if err != nil {
		t.Errorf("reading daemon.lock: %v", err)
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		t.Errorf("daemon.lock holds %q, expected one pid", content)
		return
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Errorf("daemon %d did not end within 10 s after SIGTERM", pid)
}
