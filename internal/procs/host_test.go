//go:build darwin

package procs_test

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/procs"
)

// group starts sh with two sleeps in a process group of its own and returns the group and the PIDs in it.
func group(t *testing.T) (int, []int) {
	t.Helper()
	leader, pids, _ := groupWithCommand(t)
	return leader, pids
}

func groupWithCommand(t *testing.T) (int, []int, *exec.Cmd) {
	t.Helper()
	command := exec.Command("sh", "-c", "sleep 31.701 & sleep 31.702 & wait")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	leader := command.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(-leader, syscall.SIGKILL)
		_ = command.Wait()
	})
	for attempt := 0; attempt < 100; attempt++ {
		output, _ := exec.Command("pgrep", "-g", strconv.Itoa(leader)).Output()
		if fields := strings.Fields(string(output)); len(fields) == 3 {
			pids := make([]int, len(fields))
			for index, field := range fields {
				pids[index], _ = strconv.Atoi(field)
			}
			return leader, pids, command
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("group %d did not start its two sleeps", leader)
	return 0, nil, nil
}

// rssOf sums what ps shows for each PID, in bytes.
func rssOf(t *testing.T, pids []int) int64 {
	t.Helper()
	var total int64
	for _, pid := range pids {
		output, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			t.Fatalf("ps -p %d: %v", pid, err)
		}
		kilobytes, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		total += kilobytes * 1024
	}
	return total
}

func TestSystemHostMeasuresTheWholeGroup(t *testing.T) {
	leader, pids := group(t)

	rss, err := procs.SystemHost{}.RSS(leader)

	if want := rssOf(t, pids); err != nil || rss != want {
		t.Fatalf("RSS(%d) = %d, %v; want %d, the sum of %v", leader, rss, err, want, pids)
	}
}

func TestSystemHostSignalsTheGroupAndSeesItEnd(t *testing.T) {
	leader, pids := group(t)
	host := procs.SystemHost{}

	if err := host.Signal(leader, syscall.SIGSTOP); err != nil {
		t.Fatalf("SIGSTOP: %v", err)
	}
	for _, pid := range pids {
		if state := stateOf(t, pid); state != "T" {
			t.Fatalf("process %d is in state %s after SIGSTOP, want T", pid, state)
		}
	}
	alive := host.Alive(leader)
	if err := host.Signal(leader, syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for host.Alive(leader) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !alive || host.Alive(leader) {
		t.Fatalf("Alive before the kill %v, after %v; want true, then false", alive, host.Alive(leader))
	}
}

func TestSystemHostOfAGoneGroupMeasuresNothing(t *testing.T) {
	leader, _, command := groupWithCommand(t)
	if err := syscall.Kill(-leader, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	host := procs.SystemHost{}
	for attempt := 0; attempt < 250 && host.Alive(leader); attempt++ {
		time.Sleep(20 * time.Millisecond)
	}

	rss, rssError := host.RSS(leader)
	signalError := host.Signal(leader, syscall.SIGCONT)

	want := "heavy: sending SIGCONT to process group " + strconv.Itoa(leader) + ": no such process"
	if rss != 0 || rssError != nil || signalError == nil || signalError.Error() != want {
		t.Fatalf("RSS = %d, %v; Signal = %v, want %q", rss, rssError, signalError, want)
	}
}

func TestSystemHostWithoutPsIsAnError(t *testing.T) {
	leader, _ := group(t)
	t.Setenv("PATH", t.TempDir())

	_, err := procs.SystemHost{}.RSS(leader)

	if want := `heavy: ps for process group ` + strconv.Itoa(leader) + `: exec: "ps": executable file not found in $PATH`; err == nil || err.Error() != want {
		t.Fatalf("RSS error = %v, want %s", err, want)
	}
}

func TestSystemHostNamesTheDaemonsOwnGroup(t *testing.T) {
	if got, want := (procs.SystemHost{}).OwnGroup(), syscall.Getpgrp(); got != want {
		t.Fatalf("OwnGroup = %d, want %d", got, want)
	}
}

func stateOf(t *testing.T, pid int) string {
	t.Helper()
	for attempt := 0; attempt < 100; attempt++ {
		output, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			t.Fatalf("ps -p %d: %v", pid, err)
		}
		if state := strings.TrimSpace(string(output))[:1]; state == "T" {
			return state
		}
		time.Sleep(20 * time.Millisecond)
	}
	return "not T"
}

// The fact Terminate relies on: macOS refuses a signal to a group of zombies with EPERM, not ESRCH.
func TestSystemHostSignalToAGroupOfZombiesIsRefused(t *testing.T) {
	leader, _ := group(t)
	if err := syscall.Kill(-leader, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 100; attempt++ {
		output, _ := exec.Command("ps", "-o", "stat=", "-g", strconv.Itoa(leader)).Output()
		if strings.TrimSpace(string(output)) == "Z" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	err := procs.SystemHost{}.Signal(leader, syscall.SIGCONT)

	if !errors.Is(err, syscall.EPERM) {
		t.Fatalf("SIGCONT to the zombie group %d = %v, want EPERM", leader, err)
	}
}
