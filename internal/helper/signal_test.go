package helper_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/helper"
)

type sentSignal struct {
	PID    int
	Signal syscall.Signal
}

type recordingKill struct {
	mutex sync.Mutex
	sent  []sentSignal
	err   func(pid int) error
}

func (kill *recordingKill) Kill(pid int, signal syscall.Signal) error {
	kill.mutex.Lock()
	defer kill.mutex.Unlock()
	kill.sent = append(kill.sent, sentSignal{pid, signal})
	if kill.err != nil {
		return kill.err(pid)
	}
	return nil
}

func writeEnvironment(t *testing.T, root string, pid int, entries ...string) {
	t.Helper()
	directory := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.Join(entries, "\x00") + "\x00"
	if err := os.WriteFile(filepath.Join(directory, "environ"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func signalRequest(t *testing.T, root string, kill func(int, syscall.Signal) error, tag, signal string, pids []int) helper.Response {
	t.Helper()
	line, err := json.Marshal(helper.Request{Type: "signal", Tag: tag, Signal: signal, PIDs: pids})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	options := helper.Options{Root: root, PageSize: 4096, Kill: kill}
	if err := helper.Serve(options, bytes.NewReader(append(line, '\n')), &out); err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}
	decoder := json.NewDecoder(&out)
	decoder.DisallowUnknownFields()
	var response helper.Response
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("decoding response %q: %v", out.String(), err)
	}
	return response
}

func assertSignaled(t *testing.T, response helper.Response, sent, skipped []int) {
	t.Helper()
	if response.Signaled == nil {
		t.Fatalf("response carries no signaled answer: %#v", response)
	}
	if !samePIDs(response.Signaled.Sent, sent) || !samePIDs(response.Signaled.Skipped, skipped) {
		t.Fatalf("signaled differs\n got: sent=%v skipped=%v\nwant: sent=%v skipped=%v",
			response.Signaled.Sent, response.Signaled.Skipped, sent, skipped)
	}
}

func samePIDs(a, b []int) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func assertKills(t *testing.T, kill *recordingKill, expected []sentSignal) {
	t.Helper()
	if len(kill.sent) == 0 && len(expected) == 0 {
		return
	}
	if !reflect.DeepEqual(kill.sent, expected) {
		t.Fatalf("Kill calls differ\n got: %#v\nwant: %#v", kill.sent, expected)
	}
}

func TestSignalSendsToTaggedPIDsOnly(t *testing.T) {
	root := t.TempDir()
	writeEnvironment(t, root, 10, "PATH=/usr/bin", "HEAVY_RUN=run-1", "HOME=/root")
	writeEnvironment(t, root, 11, "HEAVY_RUN=other-run")
	kill := &recordingKill{}
	response := signalRequest(t, root, kill.Kill, "run-1", "STOP", []int{10, 11})
	assertSignaled(t, response, []int{10}, []int{11})
	assertKills(t, kill, []sentSignal{{10, syscall.SIGSTOP}})
}

func TestSignalMapsEveryProtocolName(t *testing.T) {
	cases := map[string]syscall.Signal{
		"STOP": syscall.SIGSTOP, "CONT": syscall.SIGCONT, "TERM": syscall.SIGTERM, "KILL": syscall.SIGKILL,
	}
	for name, signal := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeEnvironment(t, root, 10, "HEAVY_RUN=run-1")
			kill := &recordingKill{}
			response := signalRequest(t, root, kill.Kill, "run-1", name, []int{10})
			assertSignaled(t, response, []int{10}, nil)
			assertKills(t, kill, []sentSignal{{10, signal}})
		})
	}
}

func TestSignalSkipsPIDWithoutProcEntry(t *testing.T) {
	root := t.TempDir()
	writeEnvironment(t, root, 10, "HEAVY_RUN=run-1")
	kill := &recordingKill{}
	response := signalRequest(t, root, kill.Kill, "run-1", "TERM", []int{10, 99})
	assertSignaled(t, response, []int{10}, []int{99})
	assertKills(t, kill, []sentSignal{{10, syscall.SIGTERM}})
}

func TestSignalRequiresTheExactTag(t *testing.T) {
	cases := map[string][]string{
		"longer tag value": {"HEAVY_RUN=run-12"},
		"shorter tag":      {"HEAVY_RUN=run-"},
		"other variable":   {"XHEAVY_RUN=run-1"},
		"empty environ":    {},
		"tag as value":     {"NOTE=HEAVY_RUN=run-1"},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeEnvironment(t, root, 10, entries...)
			kill := &recordingKill{}
			response := signalRequest(t, root, kill.Kill, "run-1", "KILL", []int{10})
			assertSignaled(t, response, nil, []int{10})
			assertKills(t, kill, nil)
		})
	}
}

func TestSignalCountsESRCHAsSkipped(t *testing.T) {
	root := t.TempDir()
	writeEnvironment(t, root, 10, "HEAVY_RUN=run-1")
	writeEnvironment(t, root, 11, "HEAVY_RUN=run-1")
	kill := &recordingKill{err: func(pid int) error {
		if pid == 10 {
			return syscall.ESRCH
		}
		return nil
	}}
	response := signalRequest(t, root, kill.Kill, "run-1", "CONT", []int{10, 11})
	assertSignaled(t, response, []int{11}, []int{10})
	assertKills(t, kill, []sentSignal{{10, syscall.SIGCONT}, {11, syscall.SIGCONT}})
}

func TestSignalWithoutPIDsAnswersEmptySentAndSkipped(t *testing.T) {
	kill := &recordingKill{}
	response := signalRequest(t, t.TempDir(), kill.Kill, "run-1", "STOP", nil)
	assertSignaled(t, response, nil, nil)
	assertKills(t, kill, nil)
}

func startSleep(t *testing.T) int {
	t.Helper()
	command := exec.Command("sleep", "300")
	if err := command.Start(); err != nil {
		t.Fatalf("starting sleep: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(command.Process.Pid, syscall.SIGCONT)
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	return command.Process.Pid
}

func processState(t *testing.T, pid int) string {
	t.Helper()
	output, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		t.Fatalf("ps for %d: %v", pid, err)
	}
	return strings.TrimSpace(string(output))
}

func waitForState(t *testing.T, pid int, stopped bool) string {
	t.Helper()
	var state string
	for attempt := 0; attempt < 100; attempt++ {
		state = processState(t, pid)
		if strings.HasPrefix(state, "T") == stopped {
			return state
		}
		time.Sleep(20 * time.Millisecond)
	}
	return state
}

func TestSignalStopsOnlyTheProcessThatStillCarriesTheTag(t *testing.T) {
	root := t.TempDir()
	first := startSleep(t)
	second := startSleep(t)
	writeEnvironment(t, root, first, "HEAVY_RUN=run-1")
	writeEnvironment(t, root, second, "HEAVY_RUN=run-1")
	// The second process loses the tag between find and signal.
	writeEnvironment(t, root, second, "HEAVY_RUN=run-2")

	response := signalRequest(t, root, syscall.Kill, "run-1", "STOP", []int{first, second})
	assertSignaled(t, response, []int{first}, []int{second})

	if state := waitForState(t, first, true); !strings.HasPrefix(state, "T") {
		t.Fatalf("tagged process %d state = %q, want stopped (T)", first, state)
	}
	if state := processState(t, second); strings.HasPrefix(state, "T") {
		t.Fatalf("process %d without the tag state = %q, want it not stopped", second, state)
	}

	response = signalRequest(t, root, syscall.Kill, "run-1", "CONT", []int{first, second})
	assertSignaled(t, response, []int{first}, []int{second})
	if state := waitForState(t, first, false); strings.HasPrefix(state, "T") {
		t.Fatalf("tagged process %d state = %q, want it continued", first, state)
	}
}
