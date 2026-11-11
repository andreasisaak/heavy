package client_test

import (
	"encoding/json"
	"github.com/andreasisaak/heavy/internal/testkit"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// pausedEntry is the part of a status entry the control cases read.
type pausedEntry struct {
	ID           string `json:"id"`
	State        string `json:"state"`
	PausedReason string `json:"paused_reason"`
}

func (e *environment) pausedStatus(t *testing.T) []pausedEntry {
	t.Helper()
	var entries []pausedEntry
	if err := json.Unmarshal(e.statusRaw(t), &entries); err != nil {
		return nil
	}
	return entries
}

// processState is the state letter ps shows for the process, such as S or T.
func processState(t *testing.T, pid int) string {
	t.Helper()
	output, err := exec.Command("ps", "-o", "stat=", "-p", itoa(pid)).Output()
	if err != nil {
		t.Fatalf("ps -p %d: %v", pid, err)
	}
	return strings.TrimSpace(string(output))[:1]
}

// runningSleep starts `heavy sleep <marker>` and waits until it runs, returning the client and the run's id.
func (e *environment) runningSleep(t *testing.T, marker string) (*client, string) {
	t.Helper()
	killMarked(t, marker)
	repo := repository(t)
	started := e.start(t, repo, "sleep", marker)
	started.waitStdout(t, startedPattern)
	entries := e.waitStatus(t, "the run to show as running", func(entries []entry) bool {
		_, found := find(entries, "sleep", "running")
		return found
	})
	running, _ := find(entries, "sleep", "running")
	return started, running.ID
}

// plan 2.5: --pause <id> pauses the run for the user and says so in its output; --resume continues it
func TestPauseAndResumeByTheUser(t *testing.T) {
	current := setup(t)
	started, id := current.runningSleep(t, "30.511")
	pid := marked("30.511")[0]

	stdout, stderr, status := current.output(t, t.TempDir(), "--pause", id)

	if status != 0 || stdout != "heavy: paused run "+id+" (sleep)\n" || stderr != "" {
		t.Fatalf("--pause = %d, %q, %q", status, stdout, stderr)
	}
	started.waitStdout(t, `heavy: paused by the user at \d{2}:\d{2}:\d{2}\n`)
	if got := current.pausedStatus(t); len(got) != 1 || got[0] != (pausedEntry{ID: id, State: "paused", PausedReason: "user"}) {
		t.Fatalf("status = %#v, want the run paused by the user", got)
	}
	if state := processState(t, pid); state != "T" {
		t.Fatalf("the paused command is in state %s, want T", state)
	}

	stdout, stderr, status = current.output(t, t.TempDir(), "--resume", id)

	if status != 0 || stdout != "heavy: resumed run "+id+" (sleep)\n" || stderr != "" {
		t.Fatalf("--resume = %d, %q, %q", status, stdout, stderr)
	}
	started.waitStdout(t, `heavy: resumed at \d{2}:\d{2}:\d{2}\n`)
	if got := current.pausedStatus(t); len(got) != 1 || got[0] != (pausedEntry{ID: id, State: "running"}) {
		t.Fatalf("status = %#v, want the run running again", got)
	}
	if state := processState(t, pid); state == "T" {
		t.Fatalf("the resumed command is still stopped")
	}
}

// plan 2.5: --stop <id> ends the run, which records it as stopped
func TestStopByTheUserEndsTheRun(t *testing.T) {
	current := setup(t)
	started, id := current.runningSleep(t, "30.512")

	stdout, stderr, status := current.output(t, t.TempDir(), "--stop", id)

	if status != 0 || stdout != "heavy: stopped run "+id+" (sleep)\n" || stderr != "" {
		t.Fatalf("--stop = %d, %q, %q", status, stdout, stderr)
	}
	if exit := started.wait(t); exit != 143 {
		t.Fatalf("the stopped client ended with %d, want 143", exit)
	}
	if !regexp.MustCompile(`heavy: stopped by the user at \d{2}:\d{2}:\d{2}\n`).MatchString(started.stdout.String()) {
		t.Fatalf("stdout %q holds no stop line", started.stdout.String())
	}
	files, _ := filepath.Glob(filepath.Join(current.state, "history", "*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("history files %q, want one", files)
	}
	data, _ := os.ReadFile(files[0])
	var record struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(data, &record); err != nil || record.ID != id || record.Reason != "stopped" {
		t.Fatalf("history %q, want the run recorded as stopped", data)
	}
}

// critic 2.5: --stop of a paused run ends it, and its finished entry is no longer paused
func TestStopOfAPausedRunEndsItWithoutThePause(t *testing.T) {
	current := setup(t)
	started, id := current.runningSleep(t, "30.518")
	if _, stderr, status := current.output(t, t.TempDir(), "--pause", id); status != 0 {
		t.Fatalf("--pause = %d, %q", status, stderr)
	}

	if stdout, stderr, status := current.output(t, t.TempDir(), "--stop", id); status != 0 || stdout != "heavy: stopped run "+id+" (sleep)\n" {
		t.Fatalf("--stop = %d, %q, %q", status, stdout, stderr)
	}
	if exit := started.wait(t); exit != 143 {
		t.Fatalf("the stopped client ended with %d, want 143", exit)
	}
	if got := current.pausedStatus(t); len(got) != 1 || got[0] != (pausedEntry{ID: id, State: "finished", PausedReason: ""}) {
		t.Fatalf("status = %#v, want the run finished without a pause", got)
	}
}

// plan 2.5: control of an id no active run has
func TestControlOfAnUnknownRunFails(t *testing.T) {
	current := setup(t)
	for _, option := range []string{"--pause", "--resume", "--stop"} {
		stdout, stderr, status := current.output(t, t.TempDir(), option, "nope42")

		if status != 1 || stdout != "" || stderr != "heavy: no active run has id nope42; heavy --status shows the runs\n" {
			t.Errorf("%s nope42 = %d, %q, %q", option, status, stdout, stderr)
		}
	}
}

// plan 2.5: resume of a running run and pause of a paused one are refused
func TestControlOfARunInTheWrongStateFails(t *testing.T) {
	current := setup(t)
	_, id := current.runningSleep(t, "30.513")

	stdout, stderr, status := current.output(t, t.TempDir(), "--resume", id)
	if status != 1 || stdout != "" || stderr != "heavy: run "+id+" is not paused\n" {
		t.Fatalf("--resume of a running run = %d, %q, %q", status, stdout, stderr)
	}
	current.output(t, t.TempDir(), "--pause", id)
	stdout, stderr, status = current.output(t, t.TempDir(), "--pause", id)
	if status != 1 || stdout != "" || stderr != "heavy: run "+id+" is paused already\n" {
		t.Fatalf("--pause of a paused run = %d, %q, %q", status, stdout, stderr)
	}
	current.output(t, t.TempDir(), "--resume", id)
}

// bin/test_heavy_queue_order.py:130, :163, :177: forward of #2, #1 and a place nobody waits at
func TestForwardOfTheFirstPlacesOfTheQueue(t *testing.T) {
	current := setup(t)
	killMarked(t, "30.514", "30.515", "30.516")
	repo := repository(t)
	holder := current.start(t, repo, "sleep", "30.514")
	holder.waitStdout(t, startedPattern)
	first := current.start(t, repo, "sleep", "30.515")
	first.waitStdout(t, `#1 in the queue`)
	second := current.start(t, repo, "sleep", "30.516")
	second.waitStdout(t, `#2 in the queue`)

	if stdout, stderr, status := current.output(t, repo, "--forward", "1"); status != 1 || stdout != "" ||
		stderr != "heavy: the run at #1 is next already, nothing waits ahead of it\n" {
		t.Fatalf("--forward 1 = %d, %q, %q", status, stdout, stderr)
	}
	if stdout, stderr, status := current.output(t, repo, "--forward", "5"); status != 1 || stdout != "" ||
		stderr != "heavy: no run waits at #5; heavy --status shows the queue\n" {
		t.Fatalf("--forward 5 = %d, %q, %q", status, stdout, stderr)
	}
	if stdout, stderr, status := current.output(t, repo, "--forward", "2"); status != 0 || stdout != "heavy: moved sleep from #2 to #1\n" || stderr != "" {
		t.Fatalf("--forward 2 = %d, %q, %q", status, stdout, stderr)
	}
	entries := current.status(t)
	var waiting []int
	for _, candidate := range entries {
		if candidate.State == "waiting" {
			waiting = append(waiting, candidate.Pid)
		}
	}
	if len(waiting) != 2 || waiting[0] != second.process.Pid || waiting[1] != first.process.Pid {
		t.Fatalf("waiting clients %v, want %d before %d", waiting, second.process.Pid, first.process.Pid)
	}
}

// bin/test_heavy_queue_order.py:188
func TestForwardWithoutANumericPositionIsAUsageError(t *testing.T) {
	current := setup(t)
	for _, arguments := range [][]string{{"--forward"}, {"--forward", "x"}, {"--forward", "-1"}, {"--forward", "+2"}} {
		stdout, stderr, status := current.output(t, t.TempDir(), arguments...)

		if status != 64 || stdout != "" || !strings.HasPrefix(stderr, "heavy: --forward needs a position.") {
			t.Errorf("%q = %d, %q, %q", arguments, status, stdout, stderr)
		}
	}
}

// bin/test_heavy.py:226: a queue nobody used yet is free in both forms; EC-2: the text names the unmeasured VM
// (unmeasured)
func TestStatusOfAFreeQueue(t *testing.T) {
	current := setup(t)
	testkit.FakeProgram(t, current.fakes, "docker", "exit 1")

	text, _, textStatus := current.output(t, t.TempDir(), "--status")
	list, _, listStatus := current.output(t, t.TempDir(), "--status", "--json")

	want := "heavy: no heavy run is active\n" + unmeasured
	if text != want || textStatus != 0 || list != "[]\n" || listStatus != 0 {
		t.Fatalf("--status = %q (%d), --status --json = %q (%d)", text, textStatus, list, listStatus)
	}
}

// bin/test_heavy.py:210 through the daemon: one aligned line per run with its id and memory
func TestStatusTextOfARunningRun(t *testing.T) {
	current := setup(t)
	testkit.FakeProgram(t, current.fakes, "docker", "exit 1")
	_, id := current.runningSleep(t, "30.517")

	text, _, status := current.output(t, t.TempDir(), "--status")

	pattern := `^running  ` + id + `  sleep 30\.517  \S+  0\.0 GiB  since \d{2}:\d{2}\n` + regexp.QuoteMeta(unmeasured) + `$`
	if status != 0 || !regexp.MustCompile(pattern).MatchString(text) {
		t.Fatalf("--status = %q (%d), want a line matching %s", text, status, pattern)
	}
}

func itoa(number int) string {
	return strings.TrimSpace(time.Duration(0).String()[:0] + jsonNumber(number))
}

func jsonNumber(number int) string {
	data, _ := json.Marshal(number)
	return string(data)
}
