package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/testkit"
)

const (
	// fakeClaudeChild names the file the fake Claude Code process writes the PID of its monitor to.
	fakeClaudeChild = "HEAVY_FAKE_CLAUDE_CHILD"
	// fakeClaudeHeavy names the heavy binary the fake Claude Code process starts as its monitor.
	fakeClaudeHeavy = "HEAVY_FAKE_CLAUDE_HEAVY"
)

// TestFakeClaudeCodeProcess checks nothing itself: started by startUnderClaude as a copy of this test binary named
// claude, it runs heavy --monitor as its child, writes the child's PID and ends with the child's exit status.
func TestFakeClaudeCodeProcess(t *testing.T) {
	pidFile := os.Getenv(fakeClaudeChild)
	if pidFile == "" {
		t.Skip("runs only as the fake Claude Code process of startUnderClaude")
	}
	child := exec.Command(os.Getenv(fakeClaudeHeavy), "--monitor")
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := os.WriteFile(pidFile+".new", []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := os.Rename(pidFile+".new", pidFile); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	err := child.Wait()
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		os.Exit(exitError.ExitCode())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

// fakeClaude is a process named claude, as the kernel names it, with heavy --monitor as its child.
type fakeClaude struct {
	process *exec.Cmd
	pidFile string
	stdout  *lockedBuffer
	stderr  *lockedBuffer
}

func startUnderClaude(t *testing.T) *fakeClaude {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	copyFile(t, executable, filepath.Join(folder, "claude"))
	claude := &fakeClaude{pidFile: filepath.Join(folder, "monitor.pid"), stdout: &lockedBuffer{}, stderr: &lockedBuffer{}}
	claude.process = exec.Command(filepath.Join(folder, "claude"), "-test.run=^TestFakeClaudeCodeProcess$")
	claude.process.Env = append(os.Environ(), fakeClaudeChild+"="+claude.pidFile, fakeClaudeHeavy+"="+testkit.BuildBinary(t))
	claude.process.Stdout, claude.process.Stderr = claude.stdout, claude.stderr
	if err := claude.process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if data, err := os.ReadFile(claude.pidFile); err == nil {
			if pid, err := strconv.Atoi(string(data)); err == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
		_ = claude.process.Process.Kill()
	})
	return claude
}

// monitorPID waits for the fake Claude Code to name the PID of its monitor.
func (c *fakeClaude) monitorPID(t *testing.T) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(c.pidFile); err == nil {
			pid, err := strconv.Atoi(string(data))
			if err != nil {
				t.Fatal(err)
			}
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the fake Claude Code named no monitor within 5s; stderr %q", c.stderr.String())
	return 0
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	source, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	target, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(target, source); err != nil {
		t.Fatal(err)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
}
