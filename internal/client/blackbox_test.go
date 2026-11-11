package client_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/testkit"
)

const patience = 30 * time.Second

// environment is one isolated heavy installation: binary, state directory, home, fake programs.
type environment struct {
	binary string
	state  string
	fakes  string
	log    string
}

// setup builds the binary, isolates state and home, neutralises the load thresholds and makes
// sure the daemon of the test is gone afterwards.
func setup(t *testing.T) *environment {
	t.Helper()
	binary := testkit.BuildBinary(t)
	state := testkit.StateDir(t)
	home := testkit.Home(t)
	configuration := filepath.Join(home, ".config", "heavy")
	if err := os.MkdirAll(configuration, 0o755); err != nil {
		t.Fatal(err)
	}
	limits := `{"load": {"host_pressure_percent": 0, "host_recovered_percent": 0, "reserve_bytes": 0, "load_per_core": 1000, "swap_bytes_per_2s": 1000000000000}}`
	if err := os.WriteFile(filepath.Join(configuration, "config.json"), []byte(limits), 0o644); err != nil {
		t.Fatal(err)
	}
	fakes := t.TempDir()
	t.Setenv("PATH", fakes+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HEAVY_SESSION", "session-blackbox")
	t.Setenv("HERDR_PANE_ID", "")
	log := filepath.Join(t.TempDir(), "order.log")
	t.Cleanup(func() { stopDaemon(t, state) })
	return &environment{binary: binary, state: state, fakes: fakes, log: log}
}

func stopDaemon(t *testing.T, state string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(state, "daemon.lock"))
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
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
	t.Errorf("daemon %d did not end after SIGTERM within 10 s", pid)
}

func daemonPid(t *testing.T, state string) int {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(state, "daemon.lock"))
	if err != nil {
		t.Fatalf("reading daemon.lock: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		t.Fatalf("daemon.lock holds %q, expected one pid", content)
	}
	return pid
}

// repository creates a git repository and returns its resolved path.
func repository(t *testing.T) string {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("/usr/bin/git", "init", "-q", directory)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	return directory
}

// fake writes a program into the PATH of the test.
func (e *environment) fake(t *testing.T, name, script string) {
	t.Helper()
	testkit.FakeProgram(t, e.fakes, name, script)
}

// job writes a fake that appends its name to the order log.
func (e *environment) job(t *testing.T, name string) {
	t.Helper()
	e.fake(t, name, fmt.Sprintf("echo %s >> %s", name, e.log))
}

func (e *environment) order(t *testing.T) []string {
	t.Helper()
	content, err := os.ReadFile(e.log)
	if err != nil {
		t.Fatalf("reading order log: %v", err)
	}
	return strings.Fields(string(content))
}

// history writes one record for the checkout into today's file, with 1 GiB peak and label sleep.
func (e *environment) history(t *testing.T, checkouts ...string) {
	t.Helper()
	directory := filepath.Join(e.state, "history")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	var lines []string
	for index, checkout := range checkouts {
		record := map[string]any{
			"id": fmt.Sprintf("old%d", index), "label": "sleep", "command": "sleep 5", "checkout": checkout,
			"directory": checkout, "session": "old-session", "agent": "", "since": now.Unix() - 100,
			"started": now.Unix() - 90, "ended": now.Unix() - 80, "exit_status": 0, "reason": "passed",
			"peak_rss_bytes": 1073741824, "budget": 4, "paused_seconds": 0, "restarts": 0,
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(encoded))
	}
	path := filepath.Join(directory, now.Format("2006-01-02")+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// buffer is a goroutine-safe byte collector.
type buffer struct {
	mutex sync.Mutex
	data  bytes.Buffer
}

func (b *buffer) copyFrom(reader io.Reader) {
	chunk := make([]byte, 4096)
	for {
		count, err := reader.Read(chunk)
		if count > 0 {
			b.mutex.Lock()
			b.data.Write(chunk[:count])
			b.mutex.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (b *buffer) String() string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.data.String()
}

// client is a running `heavy <command>` subprocess.
type client struct {
	process *os.Process
	stdout  *buffer
	stderr  *buffer
	done    chan struct{}
	exit    int
}

func (c *client) finished() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

func (c *client) wait(t *testing.T) int {
	t.Helper()
	select {
	case <-c.done:
		return c.exit
	case <-time.After(patience):
		t.Fatalf("client did not end within %s; stdout %q stderr %q", patience, c.stdout.String(), c.stderr.String())
		return -1
	}
}

// start runs heavy with the arguments in the directory, in its own process group.
func (e *environment) start(t *testing.T, directory string, arguments ...string) *client {
	t.Helper()
	command := exec.Command(e.binary, arguments...)
	command.Dir = directory
	command.Env = os.Environ()
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errRead, errWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stdout = outWrite
	command.Stderr = errWrite
	if err := command.Start(); err != nil {
		t.Fatalf("starting heavy %v: %v", arguments, err)
	}
	outWrite.Close()
	errWrite.Close()
	started := &client{process: command.Process, stdout: &buffer{}, stderr: &buffer{}, done: make(chan struct{})}
	go started.stdout.copyFrom(outRead)
	go started.stderr.copyFrom(errRead)
	go func() {
		state, _ := command.Process.Wait()
		started.exit = state.ExitCode()
		close(started.done)
	}()
	t.Cleanup(func() {
		if !started.finished() {
			_ = syscall.Kill(-started.process.Pid, syscall.SIGKILL)
		}
	})
	return started
}

// output runs heavy to completion and returns stdout, stderr and the exit status.
func (e *environment) output(t *testing.T, directory string, arguments ...string) (string, string, int) {
	t.Helper()
	started := e.start(t, directory, arguments...)
	status := started.wait(t)
	return started.stdout.String(), started.stderr.String(), status
}

func eventually(t *testing.T, limit time.Duration, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s: %s", limit, description)
}

func (c *client) waitStdout(t *testing.T, pattern string) {
	t.Helper()
	expression := regexp.MustCompile(pattern)
	eventually(t, patience, fmt.Sprintf("stdout matches %s, has %q", pattern, c.stdout.String()), func() bool {
		if expression.MatchString(c.stdout.String()) {
			return true
		}
		if c.finished() && !expression.MatchString(c.stdout.String()) {
			t.Fatalf("client ended with exit %d before stdout matched %s; stdout %q stderr %q", c.exit, pattern, c.stdout.String(), c.stderr.String())
		}
		return false
	})
}

const (
	startedPattern = `heavy: started at \d{2}:\d{2}:\d{2} with [0-9]+ cores \(HEAVY_CPUS\), running: `
	waitingPattern = `heavy: waiting since \d{2}:\d{2}:\d{2}, #%d in the queue\n`
)

type entry struct {
	State      string `json:"state"`
	Pid        int    `json:"pid"`
	Since      int64  `json:"since"`
	Directory  string `json:"directory"`
	Label      string `json:"label"`
	Command    string `json:"command"`
	Session    string `json:"session"`
	Position   int    `json:"position"`
	Ended      int64  `json:"ended"`
	ID         string `json:"id"`
	Checkout   string `json:"checkout"`
	ExitStatus int    `json:"exit_status"`
}

func (e *environment) statusRaw(t *testing.T) []byte {
	t.Helper()
	command := exec.Command(e.binary, "--status", "--json")
	command.Env = os.Environ()
	output, err := command.Output()
	if err != nil {
		return nil
	}
	return output
}

func (e *environment) status(t *testing.T) []entry {
	t.Helper()
	var entries []entry
	if err := json.Unmarshal(e.statusRaw(t), &entries); err != nil {
		return nil
	}
	return entries
}

func (e *environment) waitStatus(t *testing.T, description string, condition func([]entry) bool) []entry {
	t.Helper()
	var last []entry
	eventually(t, patience, description, func() bool {
		last = e.status(t)
		return condition(last)
	})
	return last
}

func find(entries []entry, label, state string) (entry, bool) {
	for _, candidate := range entries {
		if candidate.Label == label && candidate.State == state {
			return candidate, true
		}
	}
	return entry{}, false
}

// marked returns the pids of processes whose command line is `sleep <marker>`.
func marked(marker string) []int {
	output, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return nil
	}
	var pids []int
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[1] == "sleep" && fields[2] == marker {
			pid, _ := strconv.Atoi(fields[0])
			pids = append(pids, pid)
		}
	}
	return pids
}

func killMarked(t *testing.T, markers ...string) {
	t.Cleanup(func() {
		for _, marker := range markers {
			for _, pid := range marked(marker) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
}

func waitMarked(t *testing.T, limit time.Duration, marker string, present bool) {
	t.Helper()
	eventually(t, limit, fmt.Sprintf("sleep %s present=%v", marker, present), func() bool {
		return (len(marked(marker)) > 0) == present
	})
}

// plan 2.4: heavy true, queue free
func TestStartLineForFreeQueue(t *testing.T) {
	e := setup(t)
	repo := repository(t)
	stdout, stderr, status := e.output(t, repo, "true")
	if status != 0 {
		t.Fatalf("exit status %d, stderr %q", status, stderr)
	}
	if !regexp.MustCompile(`^` + startedPattern + `true\n$`).MatchString(stdout) {
		t.Fatalf("stdout %q is not exactly one started line", stdout)
	}
}

// plan 2.4: heavy sh -c 'exit 7'
func TestExitStatusOfCommand(t *testing.T) {
	e := setup(t)
	repo := repository(t)
	_, stderr, status := e.output(t, repo, "sh", "-c", "exit 7")
	if status != 7 {
		t.Fatalf("exit status %d, want 7; stderr %q", status, stderr)
	}
}

// plan 2.4: heavy sh -c 'printf a; sleep 1; printf b'
func TestOutputPassesThroughBeforeCommandEnds(t *testing.T) {
	e := setup(t)
	repo := repository(t)
	run := e.start(t, repo, "sh", "-c", "printf a; sleep 1; printf b")
	run.waitStdout(t, `^`+startedPattern+`sh -c printf a; sleep 1; printf b\n.`)
	if run.finished() {
		t.Fatalf("client ended before the reader saw the first byte; stdout %q", run.stdout.String())
	}
	if status := run.wait(t); status != 0 {
		t.Fatalf("exit status %d", status)
	}
	expression := regexp.MustCompile(`^` + startedPattern + `sh -c printf a; sleep 1; printf b\nab$`)
	if !expression.MatchString(run.stdout.String()) {
		t.Fatalf("stdout %q does not end with the command output ab", run.stdout.String())
	}
}

// plan 2.4: command writes to stderr
func TestCommandStderrStaysOnStderr(t *testing.T) {
	e := setup(t)
	repo := repository(t)
	e.fake(t, "noisy", "echo oops >&2")
	stdout, stderr, status := e.output(t, repo, "noisy")
	if status != 0 {
		t.Fatalf("exit status %d", status)
	}
	if stderr != "oops\n" {
		t.Fatalf("stderr %q, want %q", stderr, "oops\n")
	}
	if !regexp.MustCompile(`^` + startedPattern + `noisy\n$`).MatchString(stdout) {
		t.Fatalf("stdout %q holds more than the started line", stdout)
	}
}

// plan 2.4: two runs, same checkout
func TestSameCheckoutRunsOneAfterAnother(t *testing.T) {
	e := setup(t)
	repo := repository(t)
	marker := filepath.Join(t.TempDir(), "first-ended")
	e.fake(t, "first", "sleep 2; touch "+marker)
	e.fake(t, "second", "test -f "+marker)
	first := e.start(t, repo, "first")
	first.waitStdout(t, startedPattern+`first\n`)
	second := e.start(t, repo, "second")
	second.waitStdout(t, fmt.Sprintf(`^`+waitingPattern+`$`, 1))
	if first.finished() {
		t.Fatalf("first ended before second reported waiting")
	}
	if status := second.wait(t); status != 0 {
		t.Fatalf("second started before the first ended (exit %d); stdout %q", status, second.stdout.String())
	}
	if !regexp.MustCompile(`^` + fmt.Sprintf(waitingPattern, 1) + startedPattern + `second\n$`).MatchString(second.stdout.String()) {
		t.Fatalf("second stdout %q", second.stdout.String())
	}
	if status := first.wait(t); status != 0 {
		t.Fatalf("first exit %d", status)
	}
}

// plan 2.4: two runs, different checkouts, both with history (1 GiB)
func TestDifferentCheckoutsWithHistoryOverlap(t *testing.T) {
	e := setup(t)
	one, two := repository(t), repository(t)
	e.history(t, one, two)
	first := e.start(t, one, "sleep", "5.201")
	killMarked(t, "5.201", "5.202")
	first.waitStdout(t, startedPattern+`sleep 5.201\n`)
	second := e.start(t, two, "sleep", "5.202")
	e.waitStatus(t, "both runs running at once", func(entries []entry) bool {
		_, a := find(entries, "sleep", "running")
		count := 0
		for _, candidate := range entries {
			if candidate.State == "running" {
				count++
			}
		}
		return a && count == 2
	})
	if first.finished() || second.finished() {
		t.Fatalf("a run ended before both ran at the same time")
	}
	if first.wait(t) != 0 || second.wait(t) != 0 {
		t.Fatalf("a run failed")
	}
}

// plan 2.4: two runs, different checkouts, second without history
func TestDifferentCheckoutsWithoutHistoryRunSerially(t *testing.T) {
	e := setup(t)
	one, two := repository(t), repository(t)
	e.history(t, one)
	killMarked(t, "3.201", "3.202")
	first := e.start(t, one, "sleep", "3.201")
	first.waitStdout(t, startedPattern+`sleep 3.201\n`)
	second := e.start(t, two, "sleep", "3.202")
	second.waitStdout(t, fmt.Sprintf(waitingPattern, 1))
	entries := e.status(t)
	if len(entries) != 2 || entries[0].State != "running" || entries[1].State != "waiting" {
		t.Fatalf("status while first runs: %+v", entries)
	}
	second.waitStdout(t, startedPattern+`sleep 3.202\n`)
	if !first.finished() {
		after := e.status(t)
		for _, candidate := range after {
			if candidate.Command == "sleep 3.201" && candidate.State != "finished" {
				t.Fatalf("second started while first was %s", candidate.State)
			}
		}
	}
	if first.wait(t) != 0 || second.wait(t) != 0 {
		t.Fatalf("a run failed")
	}
}

// plan 2.4: three waiting runs, same checkout
func TestWaitingRunsStartInArrivalOrder(t *testing.T) {
	e := setup(t)
	repo := repository(t)
	for _, name := range []string{"job1", "job2", "job3"} {
		e.job(t, name)
	}
	killMarked(t, "4.301")
	holder := e.start(t, repo, "sleep", "4.301")
	holder.waitStdout(t, startedPattern+`sleep 4.301\n`)
	var waiting []*client
	for position, name := range []string{"job1", "job2", "job3"} {
		run := e.start(t, repo, name)
		run.waitStdout(t, fmt.Sprintf(waitingPattern, position+1))
		waiting = append(waiting, run)
	}
	for _, run := range waiting {
		if status := run.wait(t); status != 0 {
			t.Fatalf("waiting run exit %d", status)
		}
	}
	if got := e.order(t); !reflect.DeepEqual(got, []string{"job1", "job2", "job3"}) {
		t.Fatalf("start order %v", got)
	}
}

// plan 2.4: --forward 3 with three waiting
func TestForwardMovesThirdAheadOfSecond(t *testing.T) {
	e := setup(t)
	repo := repository(t)
	for _, name := range []string{"job1", "job2", "job3"} {
		e.job(t, name)
	}
	killMarked(t, "5.301")
	holder := e.start(t, repo, "sleep", "5.301")
	holder.waitStdout(t, startedPattern+`sleep 5.301\n`)
	var waiting []*client
	for position, name := range []string{"job1", "job2", "job3"} {
		run := e.start(t, repo, name)
		run.waitStdout(t, fmt.Sprintf(waitingPattern, position+1))
		waiting = append(waiting, run)
	}
	stdout, stderr, status := e.output(t, repo, "--forward", "3")
	if status != 0 || stdout != "heavy: moved job3 from #3 to #2\n" {
		t.Fatalf("--forward 3 gave exit %d stdout %q stderr %q", status, stdout, stderr)
	}
	for _, run := range waiting {
		if run.wait(t) != 0 {
			t.Fatalf("waiting run failed")
		}
	}
	if got := e.order(t); !reflect.DeepEqual(got, []string{"job1", "job3", "job2"}) {
		t.Fatalf("start order %v", got)
	}
}

// plan 2.4: client killed with SIGKILL, command sleep 100 with grandchild sleep 100 &
func TestKilledClientTakesCommandAndGrandchildWithIt(t *testing.T) {
	e := setup(t)
	repo := repository(t)
	killMarked(t, "100.123", "100.124")
	e.fake(t, "forker", "sleep 100.123 &\nsleep 100.124")
	run := e.start(t, repo, "forker")
	run.waitStdout(t, startedPattern+`forker\n`)
	waitMarked(t, 10*time.Second, "100.123", true)
	waitMarked(t, 10*time.Second, "100.124", true)
	if err := syscall.Kill(run.process.Pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	waitMarked(t, 12*time.Second, "100.123", false)
	waitMarked(t, 12*time.Second, "100.124", false)
}

// plan 2.4: client gets SIGTERM
func TestTermToClientReachesCommandAndExitsWith143(t *testing.T) {
	e := setup(t)
	repo := repository(t)
	killMarked(t, "100.125")
	proof := filepath.Join(t.TempDir(), "got-term")
	e.fake(t, "trapper", fmt.Sprintf("trap 'echo got > %s; exit 0' TERM\nsleep 100.125 &\nwait $!", proof))
	run := e.start(t, repo, "trapper")
	run.waitStdout(t, startedPattern+`trapper\n`)
	waitMarked(t, 10*time.Second, "100.125", true)
	if err := syscall.Kill(run.process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if status := run.wait(t); status != 143 {
		t.Fatalf("exit status %d, want 143", status)
	}
	content, err := os.ReadFile(proof)
	if err != nil || string(content) != "got\n" {
		t.Fatalf("command did not record SIGTERM: %q, %v", content, err)
	}
	waitMarked(t, 12*time.Second, "100.125", false)
}

// plan 2.4: daemon killed with SIGKILL while a run runs
func TestRunSurvivesDaemonKill(t *testing.T) {
	e := setup(t)
	repo := repository(t)
	killMarked(t, "9.401")
	run := e.start(t, repo, "sleep", "9.401")
	run.waitStdout(t, startedPattern+`sleep 9.401\n`)
	before, _ := find(e.waitStatus(t, "run is running", func(entries []entry) bool {
		_, ok := find(entries, "sleep", "running")
		return ok
	}), "sleep", "running")
	oldDaemon := daemonPid(t, e.state)
	if err := syscall.Kill(oldDaemon, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	after := e.waitStatus(t, "new daemon lists the same run", func(entries []entry) bool {
		found, ok := find(entries, "sleep", "running")
		return ok && found.ID == before.ID
	})
	found, _ := find(after, "sleep", "running")
	if found.ID != before.ID || found.Since != before.Since {
		t.Fatalf("run after restart id=%q since=%d, before id=%q since=%d", found.ID, found.Since, before.ID, before.Since)
	}
	if newDaemon := daemonPid(t, e.state); newDaemon == oldDaemon {
		t.Fatalf("daemon pid still %d", oldDaemon)
	}
	if status := run.wait(t); status != 0 {
		t.Fatalf("run exit %d, want 0", status)
	}
}

// plan 2.4: daemon crash with two waiting (#1, #2)
func TestWaitingOrderSurvivesDaemonKill(t *testing.T) {
	e := setup(t)
	repo := repository(t)
	e.job(t, "job1")
	e.job(t, "job2")
	killMarked(t, "9.501")
	holder := e.start(t, repo, "sleep", "9.501")
	holder.waitStdout(t, startedPattern+`sleep 9.501\n`)
	first := e.start(t, repo, "job1")
	first.waitStdout(t, fmt.Sprintf(waitingPattern, 1))
	second := e.start(t, repo, "job2")
	second.waitStdout(t, fmt.Sprintf(waitingPattern, 2))
	if err := syscall.Kill(daemonPid(t, e.state), syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	entries := e.waitStatus(t, "holder running and both waiting again", func(entries []entry) bool {
		_, running := find(entries, "sleep", "running")
		_, one := find(entries, "job1", "waiting")
		_, two := find(entries, "job2", "waiting")
		return running && one && two
	})
	one, _ := find(entries, "job1", "waiting")
	two, _ := find(entries, "job2", "waiting")
	if one.Position != 1 || two.Position != 2 {
		t.Fatalf("positions after restart: job1 #%d job2 #%d", one.Position, two.Position)
	}
	if holder.wait(t) != 0 || first.wait(t) != 0 || second.wait(t) != 0 {
		t.Fatalf("a run failed")
	}
	if got := e.order(t); !reflect.DeepEqual(got, []string{"job1", "job2"}) {
		t.Fatalf("start order after crash %v", got)
	}
}

// plan 2.4: environment in the command
func TestCommandEnvironmentAndStatusFields(t *testing.T) {
	e := setup(t)
	repo := repository(t)
	dump := filepath.Join(t.TempDir(), "env")
	e.fake(t, "envjob", fmt.Sprintf("printf '%%s\\n%%s\\n%%s\\n' \"$HEAVY_RUN\" \"$HEAVY_CPUS\" \"$HEAVY_SESSION\" > %s\nsleep 3.601", dump))
	killMarked(t, "3.601")
	run := e.start(t, repo, "envjob")
	run.waitStdout(t, startedPattern+`envjob\n`)
	entries := e.waitStatus(t, "envjob running", func(entries []entry) bool {
		_, ok := find(entries, "envjob", "running")
		return ok
	})
	current, _ := find(entries, "envjob", "running")
	var raw []map[string]any
	if err := json.Unmarshal(e.statusRaw(t), &raw); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for key := range raw[0] {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := []string{"agent", "budget", "checkout", "command", "directory", "ended", "exit_status", "expires", "id", "label", "order", "pane", "paused_reason", "pid", "position", "predicted_bytes", "restarts", "rss_bytes", "session", "since", "state"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("status fields %v, want %v", keys, want)
	}
	if current.Checkout != repo || current.Directory != repo || current.Session != "session-blackbox" || current.Command != "envjob" || current.Position != 0 || current.Pid != run.process.Pid {
		t.Fatalf("status entry %+v for repo %s client pid %d", current, repo, run.process.Pid)
	}
	if run.wait(t) != 0 {
		t.Fatalf("envjob failed")
	}
	content, err := os.ReadFile(dump)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 3 || lines[0] != current.ID || lines[2] != "session-blackbox" {
		t.Fatalf("environment dump %q, id %q", lines, current.ID)
	}
	cpus, err := strconv.Atoi(lines[1])
	if err != nil || cpus < 1 {
		t.Fatalf("HEAVY_CPUS %q is no integer >= 1", lines[1])
	}
	if !strings.Contains(run.stdout.String(), fmt.Sprintf("with %d cores (HEAVY_CPUS)", cpus)) {
		t.Fatalf("started line %q does not name %d cores", run.stdout.String(), cpus)
	}
}

// plan 2.4: heavy nope (program missing)
func TestMissingProgramExits127(t *testing.T) {
	e := setup(t)
	repo := repository(t)
	stdout, stderr, status := e.output(t, repo, "nope")
	want := "heavy: found no command nope, so nothing waits in the queue. To see the queue: heavy --status.\n"
	if status != 127 || stderr != want || stdout != "" {
		t.Fatalf("exit %d stdout %q stderr %q", status, stdout, stderr)
	}
}
