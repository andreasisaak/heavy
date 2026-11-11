package herdr_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/shell"
	"github.com/andreasisaak/heavy/internal/testkit"
)

// now is the clock of the cases in seconds, NOW of herdr/test_heavy_queue.py.
const now = 1_800_000_000

// agentList is the call that reads the agents.
var agentList = []string{"agent", "list"}

// agent is one entry of `herdr agent list` as herdr 0.9.1 prints it, with the fields the plugin reads.
type agent struct {
	PaneID string            `json:"pane_id"`
	Agent  string            `json:"agent"`
	Cwd    string            `json:"cwd"`
	Tokens map[string]string `json:"tokens"`
}

func agentOf(pane, cwd string, tokens map[string]string) agent {
	return agent{PaneID: pane, Agent: "claude", Cwd: cwd, Tokens: tokens}
}

// fakeHerdr is a herdr that answers `agent list` with its agents, records every call as one line of arguments
// separated by the unit separator, and fails the calls whose first two arguments are failing.
type fakeHerdr struct {
	path  string
	calls string
}

func newHerdr(t *testing.T, agents []agent, failing string) fakeHerdr {
	t.Helper()
	directory := t.TempDir()
	listing, err := json.Marshal(map[string]any{"id": "cli:agent:list", "result": map[string]any{"type": "agent_list", "agents": agents}})
	if err != nil {
		t.Fatal(err)
	}
	return newHerdrPrinting(t, directory, string(listing), failing)
}

// newHerdrPrinting is a fake herdr whose `agent list` prints listing as it is.
func newHerdrPrinting(t *testing.T, directory, listing, failing string) fakeHerdr {
	t.Helper()
	listingFile := filepath.Join(directory, "agents.json")
	if err := os.WriteFile(listingFile, []byte(listing), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(directory, "calls")
	script := `{ for argument in "$@"; do printf '%s\037' "$argument"; done; printf '\n'; } >> ` + shell.Quote(calls) + `
case "$1 $2" in
  ` + shell.Quote(failing) + `) echo "herdr: example failure of $1 $2" >&2; exit 3 ;;
  'agent list') cat ` + shell.Quote(listingFile) + ` ;;
esac`
	return fakeHerdr{path: testkit.FakeProgram(t, directory, "herdr", script), calls: calls}
}

// recorded are the calls of the fake, each as its arguments.
func (f fakeHerdr) recorded(t *testing.T) [][]string {
	t.Helper()
	data, err := os.ReadFile(f.calls)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		calls = append(calls, strings.Split(strings.TrimSuffix(line, "\x1f"), "\x1f"))
	}
	return calls
}

// report is the call that reports the tokens of pane, with the source of the plugin.
func report(pane string, arguments ...string) []string {
	return append([]string{"pane", "report-metadata", pane, "--source", "heavy-queue"}, arguments...)
}

// record is a run as the daemon lists it, arrived or started secondsAgo, with the fields 0.6 printed.
func record(state string, secondsAgo int64, directory, label string) protocol.RunStatus {
	since := now - secondsAgo
	return protocol.RunStatus{
		State: state, PID: 4242, Since: since, Order: float64(since), Directory: directory, Label: label, Command: "make " + label,
	}
}

func running(secondsAgo int64, directory, label string) protocol.RunStatus {
	return record("running", secondsAgo, directory, label)
}

func paused(secondsAgo int64, directory, label string) protocol.RunStatus {
	return record("paused", secondsAgo, directory, label)
}

func waiting(position int, secondsAgo int64, directory, label string) protocol.RunStatus {
	run := record("waiting", secondsAgo, directory, label)
	run.Position = position
	return run
}

// finished is a run that ended secondsAgo after lasting lasted seconds; the daemon keeps it for 30 s.
func finished(secondsAgo, lasted int64, directory, label string, exitStatus int) protocol.RunStatus {
	run := record("finished", secondsAgo+lasted, directory, label)
	run.Ended, run.ExitStatus = now-secondsAgo, exitStatus
	run.Expires = run.Ended + 30
	return run
}

func inPane(run protocol.RunStatus, pane string) protocol.RunStatus {
	run.Pane = pane
	return run
}

// checkouts creates a git repository for each name in a temporary directory and returns the directory.
func checkouts(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range names {
		if output, err := exec.Command("git", "init", "-q", filepath.Join(root, name)).CombinedOutput(); err != nil {
			t.Fatalf("git init %s: %v\n%s", name, err, output)
		}
	}
	return root
}

func check(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s:\n got %#v\nwant %#v", what, got, want)
	}
}
