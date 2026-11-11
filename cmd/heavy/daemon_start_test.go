package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/andreasisaak/heavy/internal/testkit"
)

// heavy-37 2026-11-08: Claude Code ends a stopped task together with every process below it by parent pid, so a
// daemon that stays the child of the heavy that started it ends with that heavy's monitor, run or session
func TestTheDaemonIsNoChildOfTheHeavyThatStartsIt(t *testing.T) {
	state := testkit.StateDir(t)
	t.Cleanup(func() { stopDaemon(t, state) })
	run := exec.Command(testkit.BuildBinary(t), "sleep", "30")
	run.Dir, run.Env = resolvedTemp(t), append(os.Environ(), "HEAVY_STATE_DIRECTORY="+state, "HEAVY_SESSION=example-starter")
	run.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := run.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-run.Process.Pid, syscall.SIGKILL)
		_ = run.Wait()
	})
	waitForRunning(t, state)

	daemon := lockedPID(t, state)
	parent := parentOf(t, daemon)

	if parent == run.Process.Pid || parent == 0 {
		t.Fatalf("the daemon %d has the parent %d, want another than the run %d that started it", daemon, parent, run.Process.Pid)
	}
}

func lockedPID(t *testing.T, state string) int {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(state, "daemon.lock"))
	if err != nil {
		t.Fatalf("reading the daemon lock: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		t.Fatalf("the daemon lock holds %q, expected a pid", content)
	}
	return pid
}

func parentOf(t *testing.T, pid int) int {
	t.Helper()
	output, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		t.Fatalf("ps -o ppid= -p %d: %v", pid, err)
	}
	parent, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		t.Fatalf("ps named the parent %q, expected a pid", output)
	}
	return parent
}
