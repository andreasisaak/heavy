package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/client"
	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/shell"
	"github.com/andreasisaak/heavy/internal/testkit"
)

const hookContext = "Queued behind heavy: `make ci`. " +
	"The hook put `heavy` in front of each heavy command and sent the command to the background, " +
	"so it starts as soon as this machine has room for it. %s " +
	"You will be woken when your run ends: do not poll the queue, and do not stop the run to start it again. " +
	"The heavy monitor of the session reports the start, the first red line and the end of the run with its exit status, " +
	"so build no monitor, tail or watch loop of your own for it. " +
	"Until then keep working without asking the user for approval. " +
	"Change only files the run does not read, or it checks a different state than the one you commit: " +
	"a backend test does not read frontend files, and reading, planning the next task and reviewing the diff change nothing. " +
	"End your turn only when no such work is left. " +
	"A run that names one to three test files stays outside the queue. " +
	"Running: %s"

type hookAnswer struct {
	HookSpecificOutput struct {
		HookEventName            string                     `json:"hookEventName"`
		PermissionDecision       string                     `json:"permissionDecision,omitempty"`
		PermissionDecisionReason string                     `json:"permissionDecisionReason,omitempty"`
		UpdatedInput             map[string]json.RawMessage `json:"updatedInput,omitempty"`
		AdditionalContext        string                     `json:"additionalContext,omitempty"`
	} `json:"hookSpecificOutput"`
}

// listedCheckout is a git repository whose .heavy.json lists make ci; HOME and the state directory are temporary,
// and without CLAUDE_PLUGIN_DATA the guard generates no herdr plugin.
func listedCheckout(t *testing.T) (checkout, state string) {
	t.Helper()
	testkit.Home(t)
	t.Setenv("CLAUDE_PLUGIN_DATA", "")
	state = testkit.StateDir(t)
	checkout = t.TempDir()
	if output, err := exec.Command("/usr/bin/git", "init", "-q", checkout).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	if err := os.WriteFile(filepath.Join(checkout, ".heavy.json"), []byte(`{"commands": ["make ci"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(checkout)
	if err != nil {
		t.Fatal(err)
	}
	return resolved, state
}

func guardHook(t *testing.T, command, cwd string) (hookAnswer, string, int) {
	t.Helper()
	input, err := json.Marshal(map[string]any{
		"tool_name": "Bash", "cwd": cwd, "session_id": "example-session", "tool_input": map[string]string{"command": command},
	})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exitStatus := runGuardHook(bytes.NewReader(input), &stdout, &stderr)
	var answer hookAnswer
	decoder := json.NewDecoder(&stdout)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&answer); err != nil {
		t.Fatalf("the hook wrote no answer (%v), stderr %q", err, stderr.String())
	}
	return answer, stderr.String(), exitStatus
}

func rewrittenWith(t *testing.T, status string) hookAnswer {
	t.Helper()
	binary, err := ownBinary()
	if err != nil {
		t.Fatal(err)
	}
	command := "{ " + shell.Quote(binary) + ` make ci; }; exit_status=$?; echo "heavy $exit_status"; exit $exit_status`
	var want hookAnswer
	want.HookSpecificOutput.HookEventName = "PreToolUse"
	encoded, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	want.HookSpecificOutput.UpdatedInput = map[string]json.RawMessage{"command": encoded, "run_in_background": json.RawMessage("true")}
	// No monitor of the session is connected in these tests, so the context says how to start one (FR-29).
	want.HookSpecificOutput.AdditionalContext = fmt.Sprintf(hookContext, status, command) +
		" No heavy monitor is connected for this session; start Monitor with: heavy --monitor --session example-session"
	return want
}

func TestGuardHookPutsThisHeavyInFrontOfAHeavyCommand(t *testing.T) {
	checkout, _ := listedCheckout(t)

	answer, stderr, exitStatus := guardHook(t, "make ci", checkout)

	want := rewrittenWith(t, "No other heavy run is active, your run starts right away.")
	if !reflect.DeepEqual(answer, want) || stderr != "" || exitStatus != 0 {
		t.Fatalf("guard hook = %+v, %q, %d\nwant %+v, \"\", 0", answer, stderr, exitStatus, want)
	}
}

// test_heavy_guard.py:590: the queue comes from the running daemon.
func TestGuardHookNamesTheRunOfTheRunningDaemon(t *testing.T) {
	checkout, state := listedCheckout(t)
	t.Cleanup(func() { stopDaemon(t, state) })
	directory := resolvedTemp(t)
	run := exec.Command(testkit.BuildBinary(t), "sleep", "30")
	run.Dir, run.Env = directory, append(os.Environ(), "HEAVY_STATE_DIRECTORY="+state, "HEAVY_SESSION=example-other")
	run.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := run.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-run.Process.Pid, syscall.SIGKILL)
		_ = run.Wait()
	})
	waitForRunning(t, state)

	answer, stderr, exitStatus := guardHook(t, "make ci", checkout)

	want := rewrittenWith(t, "Running now: `sleep 30` in "+directory+" for 0 min.")
	if !reflect.DeepEqual(answer, want) || stderr != "" || exitStatus != 0 {
		t.Fatalf("guard hook = %+v, %q, %d\nwant %+v, \"\", 0", answer, stderr, exitStatus, want)
	}
}

// test_heavy_guard.py:600: a queue the hook cannot read denies the call (design G15).
func TestGuardHookDeniesWhenTheDaemonAnswersNoHello(t *testing.T) {
	checkout, state := listedCheckout(t)
	listener, err := net.Listen("unix", filepath.Join(state, "daemon.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		_, _ = bufio.NewReader(connection).ReadString('\n')
		_, _ = connection.Write([]byte("garbage\n"))
	}()

	answer, stderr, exitStatus := guardHook(t, "make ci", checkout)

	var want hookAnswer
	want.HookSpecificOutput.HookEventName = "PreToolUse"
	want.HookSpecificOutput.PermissionDecision = "deny"
	want.HookSpecificOutput.PermissionDecisionReason = fmt.Sprintf("heavy: reading the daemon's hello: heavy: expected a hello of protocol %d, "+
		"got garbage: invalid character 'g' looking for beginning of value", protocol.Version)
	if !reflect.DeepEqual(answer, want) || stderr != "" || exitStatus != 0 {
		t.Fatalf("guard hook = %+v, %q, %d\nwant %+v, \"\", 0", answer, stderr, exitStatus, want)
	}
}

func resolvedTemp(t *testing.T) string {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return directory
}

func waitForRunning(t *testing.T, state string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		status, found, err := client.StatusIfRunning(daemon.ConnectOptions{StateDirectory: state})
		if err == nil && found && len(status.Runs) == 1 && status.Runs[0].State == "running" {
			return
		}
	}
	t.Fatal("heavy sleep 30 did not run within 10 s")
}

func stopDaemon(t *testing.T, state string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(state, "daemon.lock"))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Errorf("reading the daemon lock: %v", err)
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		t.Errorf("the daemon lock holds %q, expected a pid", content)
		return
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Errorf("daemon %d did not end within 10 s of SIGTERM", pid)
}
