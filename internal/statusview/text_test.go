package statusview_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/statusview"
)

const gibibyte = 1 << 30

func clock(unix int64) string {
	return time.Unix(unix, 0).Format("15:04")
}

// bin/test_heavy.py:210, with the columns id and memory of plan 2.5
func TestStatusPrintsOneAlignedLinePerRun(t *testing.T) {
	runs := []protocol.RunStatus{
		{State: "running", ID: "a1b2c3", Command: "make ci", Directory: "/home/example/main-feature", Since: 1_800_000_000, RSSBytes: 5 * gibibyte / 2},
		{State: "waiting", Position: 1, ID: "d4e5f6", Command: "make test", Directory: "/sites/other", Since: 1_800_000_060, PredictedBytes: 3 * gibibyte},
	}

	got := statusview.Text(runs, "/home/example")

	want := "running    a1b2c3  make ci    ~/main-feature  2.5 GiB   since " + clock(1_800_000_000) + "\n" +
		"waiting 1  d4e5f6  make test  /sites/other    ~3.0 GiB  since " + clock(1_800_000_060) + "\n"
	if got != want {
		t.Fatalf("Text =\n%s\nwant\n%s", got, want)
	}
}

// bin/test_heavy.py:226
func TestStatusReportsAFreeQueue(t *testing.T) {
	if got := statusview.Text(nil, "/home/example"); got != "heavy: no heavy run is active\n" {
		t.Fatalf("Text = %q", got)
	}
}

// bin/test_heavy.py:362
func TestStatusPrintsFinishedRunsAfterTheQueueWithTheirResult(t *testing.T) {
	runs := []protocol.RunStatus{
		{State: "finished", ID: "bbbbbb", Command: "make test", Directory: "/sites/other", Ended: 1_800_000_010, ExitStatus: 2},
		{State: "finished", ID: "aaaaaa", Command: "make ci", Directory: "/sites/main", Ended: 1_800_000_000, ExitStatus: 0},
	}

	got := statusview.Text(runs, "/home/example")

	want := "heavy: no heavy run is active\n" +
		"failed 2  bbbbbb  make test  /sites/other  -  ended " + clock(1_800_000_010) + "\n" +
		"passed    aaaaaa  make ci    /sites/main   -  ended " + clock(1_800_000_000) + "\n"
	if got != want {
		t.Fatalf("Text =\n%s\nwant\n%s", got, want)
	}
}

// bin/test_heavy.py:487
func TestStatusCutsALongCommandToKeepTheLinesReadable(t *testing.T) {
	var files []string
	for number := range 40 {
		files = append(files, "src/file"+strconv.Itoa(number)+".ts")
	}
	command := "docker compose run --rm -T frontend sh -c " + strings.Join(files, " ")
	runs := []protocol.RunStatus{{State: "running", ID: "a1b2c3", Command: command, Directory: "/sites/main", Since: 1_800_000_000}}

	got := statusview.Text(runs, "/home/example")

	want := "running  a1b2c3  " + command[:59] + "…  /sites/main  0.0 GiB  since " + clock(1_800_000_000) + "\n"
	if got != want {
		t.Fatalf("Text =\n%q\nwant\n%q", got, want)
	}
}

func TestStatusNamesThePauseAndAnUnknownPrediction(t *testing.T) {
	runs := []protocol.RunStatus{
		{State: "paused", PausedReason: "user", ID: "a1b2c3", Command: "make ci", Directory: "/sites/main", Since: 1_800_000_000, RSSBytes: gibibyte},
		{State: "waiting", Position: 1, ID: "d4e5f6", Command: "make e2e", Directory: "/sites/main", Since: 1_800_000_060},
	}

	got := statusview.Text(runs, "/home/example")

	want := "paused user  a1b2c3  make ci   /sites/main  1.0 GiB  since " + clock(1_800_000_000) + "\n" +
		"waiting 1    d4e5f6  make e2e  /sites/main  ~?       since " + clock(1_800_000_060) + "\n"
	if got != want {
		t.Fatalf("Text =\n%s\nwant\n%s", got, want)
	}
}

func TestJSONOfAnEmptyQueueIsAnEmptyList(t *testing.T) {
	got, err := statusview.JSON(nil)

	if err != nil || got != "[]\n" {
		t.Fatalf("JSON = %q, %v; want []", got, err)
	}
}

func TestJSONHoldsEveryFieldOfAStatusIndented(t *testing.T) {
	got, err := statusview.JSON([]protocol.RunStatus{{State: "running", PID: 7, Since: 1, Order: 1.5, Directory: "/d", Label: "ci",
		Command: "make ci", ID: "a1", Checkout: "/c", Budget: 18}})

	want := `[
  {
    "state": "running",
    "pid": 7,
    "since": 1,
    "order": 1.5,
    "directory": "/d",
    "label": "ci",
    "command": "make ci",
    "agent": "",
    "pane": "",
    "session": "",
    "position": 0,
    "ended": 0,
    "exit_status": 0,
    "expires": 0,
    "id": "a1",
    "checkout": "/c",
    "budget": 18,
    "rss_bytes": 0,
    "predicted_bytes": 0,
    "paused_reason": "",
    "restarts": 0
  }
]
`
	if err != nil || got != want {
		t.Fatalf("JSON =\n%s, %v\nwant\n%s", got, err, want)
	}
}
