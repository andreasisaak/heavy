package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/client"
	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/testkit"
)

// queued is a `heavy sh -c <script>` of one installed heavy that runs beside the test.
type queued struct {
	command *exec.Cmd
	output  *bytes.Buffer
}

func queue(t *testing.T, root, state, directory, script string) *queued {
	t.Helper()
	run := &queued{command: exec.Command(filepath.Join(root, "bin", "heavy"), "sh", "-c", script), output: &bytes.Buffer{}}
	run.command.Dir, run.command.Env = directory, append(os.Environ(), "HEAVY_STATE_DIRECTORY="+state, "HEAVY_SESSION=example-session")
	run.command.Stdout, run.command.Stderr = run.output, run.output
	run.command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := run.command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-run.command.Process.Pid, syscall.SIGKILL) })
	return run
}

// runs waits for the queue to hold the given states and returns it.
func runs(t *testing.T, state string, states ...string) []protocol.RunStatus {
	t.Helper()
	var last []string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		status, _, err := client.StatusIfRunning(daemon.ConnectOptions{StateDirectory: state})
		if err != nil {
			continue
		}
		last = nil
		for _, run := range status.Runs {
			last = append(last, run.State)
		}
		if reflect.DeepEqual(last, states) {
			return status.Runs
		}
	}
	t.Fatalf("the queue holds %q after 10 s, want %q", last, states)
	return nil
}

// started are the versions of the daemons that started in this state directory, in their order.
func started(t *testing.T, state string) []string {
	t.Helper()
	log, err := os.ReadFile(filepath.Join(state, "daemon.log"))
	if err != nil {
		t.Fatal(err)
	}
	var versions []string
	for _, line := range strings.Split(strings.TrimSpace(string(log)), "\n") {
		var record struct {
			Message string `json:"msg"`
			Version string `json:"version"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("the log line %q: %v", line, err)
		}
		if record.Message == "daemon started" {
			versions = append(versions, record.Version)
		}
	}
	return versions
}

// The update of heavy on a busy machine: a daemon of the older heavy holds a running and a waiting run when a
// session with the newer heavy queues its first run. The newer daemon takes over at once. The command of the
// running run is a child of its client and runs on to its end, once; the waiting run keeps its place and starts
// after it, in the same worktree; and no client of the older heavy starts a daemon of its own version again.
func TestTheDaemonOfANewerHeavyTakesOverTheRunsOfAnOlderOne(t *testing.T) {
	older, newer := installed(t, "1.3.0", "aaa"), installed(t, "1.4.0", "bbb")
	state, worktree, marks := testkit.StateDir(t), resolvedTemp(t), filepath.Join(t.TempDir(), "marks")
	t.Cleanup(func() { stopDaemon(t, state) })
	first := queue(t, older, state, worktree, "echo first starts >> "+marks+"; sleep 4; echo first ends >> "+marks)
	id := runs(t, state, "running")[0].ID
	// The daemon takes a pause only once the client named the process group of its command; from then on the run can
	// go to another daemon.
	for _, option := range []string{"--pause", "--resume"} {
		for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
			control := exec.Command(filepath.Join(older, "bin", "heavy"), option, id)
			control.Env = append(os.Environ(), "HEAVY_STATE_DIRECTORY="+state)
			if output, err := control.CombinedOutput(); err == nil {
				break
			} else if time.Now().After(deadline) {
				t.Fatalf("heavy %s %s: %v\n%s", option, id, err, output)
			}
		}
	}
	second := queue(t, older, state, worktree, "echo second runs >> "+marks)
	runs(t, state, "running", "waiting")

	third := queue(t, newer, state, resolvedTemp(t), "echo third runs >> "+marks)
	results := []error{third.command.Wait(), first.command.Wait(), second.command.Wait()}

	if results[0] != nil || results[1] != nil || results[2] != nil {
		t.Fatalf("the runs ended with %v\nfirst: %s\nsecond: %s\nthird: %s", results, first.output, second.output, third.output)
	}
	if got, want := started(t, state), []string{"1.3.0", "1.4.0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("daemons started with the versions %q, want %q", got, want)
	}
	written, err := os.ReadFile(marks)
	if err != nil {
		t.Fatal(err)
	}
	// The third run is in a worktree of its own and starts when the new daemon has heard of the two it took over.
	order := strings.Replace(string(written), "third runs\n", "", 1)
	if want := "first starts\nfirst ends\nsecond runs\n"; order != want || !strings.Contains(string(written), "third runs\n") {
		t.Fatalf("the commands wrote %q, want %q with the third run anywhere after the first start", written, want)
	}
	lost := regexp.MustCompile(`heavy: lost the daemon at \d{2}:\d{2}:\d{2}, announcing the run to a new one\n`)
	if len(lost.FindAllString(first.output.String(), -1)) != 1 || len(lost.FindAllString(second.output.String(), -1)) != 1 {
		t.Fatalf("each run of the older heavy announces itself to the new daemon once\nfirst: %s\nsecond: %s", first.output, second.output)
	}
}
