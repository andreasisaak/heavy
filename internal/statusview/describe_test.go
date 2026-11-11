package statusview_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/statusview"
)

var described = time.Unix(1_800_000_000, 0)

// hooks/heavy_queue.py:66: a run as the guard and the stop hook name it, its minutes rounded down
func TestDescribeNamesTheRunAndItsMinutes(t *testing.T) {
	long := strings.Repeat("ä", 61)
	cases := map[string]struct {
		run  protocol.RunStatus
		want string
	}{
		"waiting": {protocol.RunStatus{State: "waiting", Command: "make ci", Position: 2, Since: described.Unix() - 419}, "`make ci` waits at position 2 for 6 min"},
		"running": {protocol.RunStatus{State: "running", Command: "make ci", Since: described.Unix() - 240}, "`make ci` runs for 4 min"},
		"paused":  {protocol.RunStatus{State: "paused", Command: "make ci", Since: described.Unix() - 599}, "`make ci` is paused, started 9 min ago"},
		"long":    {protocol.RunStatus{State: "running", Command: long, Since: described.Unix()}, "`" + strings.Repeat("ä", 59) + "…` runs for 0 min"},
	}
	for name, c := range cases {
		if got := statusview.Describe(c.run, described); got != c.want {
			t.Errorf("%s: Describe = %q, want %q", name, got, c.want)
		}
	}
}

// V12: the open runs of a session are its waiting, running and paused runs, by the session id the daemon recorded
func TestOwnRunsAreTheOpenRunsOfTheSession(t *testing.T) {
	runs := []protocol.RunStatus{
		{ID: "a", State: "running", Session: "s1"}, {ID: "b", State: "paused", Session: "s1"}, {ID: "c", State: "waiting", Session: "s1"},
		{ID: "d", State: "finished", Session: "s1"}, {ID: "e", State: "running", Session: "s2"},
	}

	got := statusview.Own(runs, "s1")

	if want := runs[:3]; !reflect.DeepEqual(got, want) {
		t.Fatalf("Own = %+v, want %+v", got, want)
	}
}
