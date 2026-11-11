package statusview_test

import (
	"testing"

	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/statusview"
)

// A run without history is planned with unpredicted_peak_bytes of the personal list; the status says so, while it
// waits and while it runs. A run with history and a finished run show no estimate.
func TestStatusNamesTheEstimateOfARunWithoutHistory(t *testing.T) {
	runs := []protocol.RunStatus{
		{State: "running", ID: "a1", Command: "make ci", Directory: "/s/a", Since: 1_800_000_000, RSSBytes: 5 * gibibyte / 2, PredictedBytes: 3 * gibibyte},
		{State: "running", ID: "b2", Command: "make lint", Directory: "/s/b", Since: 1_800_000_000, RSSBytes: gibibyte / 2},
		{State: "paused", ID: "c3", Command: "make e2e", Directory: "/s/c", Since: 1_800_000_000, RSSBytes: gibibyte},
		{State: "waiting", Position: 1, ID: "d4", Command: "make test", Directory: "/s/d", Since: 1_800_000_000},
		{State: "waiting", Position: 2, ID: "e5", Command: "make docs", Directory: "/s/e", Since: 1_800_000_000, PredictedBytes: gibibyte},
		{State: "finished", ID: "f6", Command: "make x", Directory: "/s/f", Ended: 1_800_000_000},
	}
	at := clock(1_800_000_000)

	got := statusview.Text(runs, "/home/example", 8*gibibyte)

	want := "running    a1  make ci    /s/a  2.5 GiB                 since " + at + "\n" +
		"running    b2  make lint  /s/b  0.5 GiB (est. 8.0 GiB)  since " + at + "\n" +
		"paused     c3  make e2e   /s/c  1.0 GiB (est. 8.0 GiB)  since " + at + "\n" +
		"waiting 1  d4  make test  /s/d  ~8.0 GiB est.           since " + at + "\n" +
		"waiting 2  e5  make docs  /s/e  ~1.0 GiB                since " + at + "\n" +
		"passed     f6  make x     /s/f  -                       ended " + at + "\n"
	if got != want {
		t.Fatalf("Text =\n%s\nwant\n%s", got, want)
	}
}

func TestStatusWithoutAnEstimateShowsARunWithoutHistoryAsBefore(t *testing.T) {
	runs := []protocol.RunStatus{
		{State: "running", ID: "b2", Command: "make lint", Directory: "/s/b", Since: 1_800_000_000, RSSBytes: gibibyte / 2},
		{State: "waiting", Position: 1, ID: "d4", Command: "make test", Directory: "/s/d", Since: 1_800_000_000},
	}
	at := clock(1_800_000_000)

	got := statusview.Text(runs, "/home/example", 0)

	want := "running    b2  make lint  /s/b  0.5 GiB  since " + at + "\n" +
		"waiting 1  d4  make test  /s/d  ~?       since " + at + "\n"
	if got != want {
		t.Fatalf("Text =\n%s\nwant\n%s", got, want)
	}
}
