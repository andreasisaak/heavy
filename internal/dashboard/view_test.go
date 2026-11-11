package dashboard

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/andreasisaak/heavy/internal/platform"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// shown is what a view puts on the screen.
type shown struct {
	Content   string
	AltScreen bool
}

func screen(t *testing.T, model Model) shown {
	t.Helper()
	view := model.View()
	return shown{Content: plain(t, view.Content), AltScreen: view.AltScreen}
}

func lines(rows ...string) string {
	return strings.Join(rows, "\n")
}

// plan 6.3: at width 80 the view shows the header, the open runs, the series, the foreign load, the recent results
// and the keys (design G13)
func TestViewAtWidth80ShowsTheQueueAndTheMachine(t *testing.T) {
	daemon := &fakeDaemon{answers: []fetchAnswer{{status: protocol.Status{Runs: exampleRuns(), Machine: exampleMachine()}}}}
	model := started(t, daemon)

	got := screen(t, model)

	want := shown{AltScreen: true, Content: lines(
		"heavy · Mac · 18 cores · load 6.8                                          20:41",
		platform.Docker+" free 1.7 GiB · host free 8 % · swap 83 MiB/2s",
		"",
		"› ▶ 12m      ci       shopfront-csv-export     2.5 GiB            9 cores",
		"  ⏸  4m      test     shopfront-communication  1.1 GiB            paused: memory",
		"  ⧗  3m  #1  ui-test  shopfront-performance    ~3.0 GiB expected",
		"",
		"memory free 30 min  ██▇▆▃▂▂▁▂▃",
		"foreign  shopfront-app-1 10.1 GiB (shopfront) · spa-watch 1.5 GiB (shopfront)",
		"recent  ✓ ci 13m shopfront 20:02 · ✗ test 6m shopfront 19:40",
		"",
		"↑/↓ select · f forward · p pause/resume · s stop · q quit",
	)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("view =\n%s\n%+v\nwant\n%s\n%+v", got.Content, got, want.Content, want)
	}
}

func TestViewCutsLinesWiderThanTheTerminal(t *testing.T) {
	daemon := &fakeDaemon{answers: []fetchAnswer{{status: protocol.Status{Runs: exampleRuns(), Machine: exampleMachine()}}}}
	model := step(t, started(t, daemon), windowWidth(60))

	got := screen(t, model)

	want := shown{AltScreen: true, Content: lines(
		"heavy · Mac · 18 cores · load 6.8                      20:41",
		platform.Docker+" free 1.7 GiB · host free 8 % · swap 83 MiB/2s",
		"",
		"› ▶ 12m      ci       shopfront-csv-export     2.5 GiB     …",
		"  ⏸  4m      test     shopfront-communication  1.1 GiB     …",
		"  ⧗  3m  #1  ui-test  shopfront-performance    ~3.0 GiB exp…",
		"",
		"memory free 30 min  ██▇▆▃▂▂▁▂▃",
		"foreign  shopfront-app-1 10.1 GiB (shopfront) · spa-watch 1…",
		"recent  ✓ ci 13m shopfront 20:02 · ✗ test 6m shopfront 19:40",
		"",
		"↑/↓ select · f forward · p pause/resume · s stop · q quit",
	)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("view =\n%s\n%+v\nwant\n%s\n%+v", got.Content, got, want.Content, want)
	}
}

func TestViewWithoutAMachineSampleWaitsForIt(t *testing.T) {
	daemon := &fakeDaemon{answers: []fetchAnswer{{status: protocol.Status{Runs: exampleRuns()[2:3]}}}}
	model := started(t, daemon)

	got := screen(t, model)

	want := shown{AltScreen: true, Content: lines(
		"heavy · Mac · waiting for the first sample                                 20:41",
		"",
		"› ⧗ 3m  #1  ui-test  shopfront-performance  ~3.0 GiB expected",
		"",
		"↑/↓ select · f forward · p pause/resume · s stop · q quit",
	)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("view =\n%s\n%+v\nwant\n%s\n%+v", got.Content, got, want.Content, want)
	}
}

func TestViewOfAnEmptyQueueWithoutTheVMSaysSo(t *testing.T) {
	machine := exampleMachine()
	machine.VMMeasured, machine.VMAvailableBytes = false, 0
	machine.FreeSeries, machine.Foreign, machine.Recent = []int64{}, []protocol.ForeignContainer{}, []protocol.Result{}
	daemon := &fakeDaemon{answers: []fetchAnswer{{status: protocol.Status{Runs: []protocol.RunStatus{}, Machine: machine}}}}
	model := started(t, daemon)

	got := screen(t, model)

	want := shown{AltScreen: true, Content: lines(
		"heavy · Mac · 18 cores · load 6.8                                          20:41",
		platform.Docker+" not measured · host free 8 % · swap 83 MiB/2s",
		"",
		"  no heavy run in the queue",
		"",
		"↑/↓ select · f forward · p pause/resume · s stop · q quit",
	)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("view =\n%s\n%+v\nwant\n%s\n%+v", got.Content, got, want.Content, want)
	}
}

// plan 6.3: a fetch that fails turns the header into `daemon not reachable, retrying`; the last runs stay
func TestViewAfterAFailedFetchSaysTheDaemonIsNotReachable(t *testing.T) {
	daemon := &fakeDaemon{answers: []fetchAnswer{
		{status: protocol.Status{Runs: exampleRuns()[:1]}},
		{err: errors.New("heavy: no daemon answers in /tmp/heavy-state-example; the next heavy run starts one")},
	}}
	model := started(t, daemon)
	model = step(t, model, run(t, nextFetch(t, model), "the tick"))

	got := screen(t, model)

	want := shown{AltScreen: true, Content: lines(
		"heavy · Mac · daemon not reachable, retrying                               20:41",
		"heavy: no daemon answers in /tmp/heavy-state-example; the next heavy run starts…",
		"",
		"› ▶ 12m  ci  shopfront-csv-export  2.5 GiB  9 cores",
		"",
		"↑/↓ select · f forward · p pause/resume · s stop · q quit",
	)}
	if !reflect.DeepEqual(got, want) || daemon.fetches != 2 {
		t.Fatalf("view after %d fetches =\n%s\n%+v\nwant 2 fetches and\n%s\n%+v", daemon.fetches, got.Content, got, want.Content, want)
	}
}

func TestViewBeforeAnyAnswerOfTheDaemonShowsNoRuns(t *testing.T) {
	daemon := &fakeDaemon{answers: []fetchAnswer{{err: errors.New("heavy: example refusal")}}}
	model := started(t, daemon)

	got := screen(t, model)

	want := shown{AltScreen: true, Content: lines(
		"heavy · Mac · daemon not reachable, retrying                               20:41",
		"heavy: example refusal",
		"",
		"↑/↓ select · f forward · p pause/resume · s stop · q quit",
	)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("view =\n%s\n%+v\nwant\n%s\n%+v", got.Content, got, want.Content, want)
	}
}

// A waiting run shows what keeps it where a running one shows its cores, cut to the width of the terminal.
func TestViewNamesWhatKeepsAWaitingRun(t *testing.T) {
	runs := exampleRuns()
	runs[2].Waits = "same worktree as run a1"
	daemon := &fakeDaemon{answers: []fetchAnswer{{status: protocol.Status{Runs: runs, Machine: exampleMachine()}}}}

	got := strings.Split(screen(t, started(t, daemon)).Content, "\n")

	if want := "  ⧗  3m  #1  ui-test  shopfront-performance    ~3.0 GiB expected  same worktree…"; len(got) < 6 || got[5] != want {
		t.Fatalf("view =\n%s\nwant the line\n%s", strings.Join(got, "\n"), want)
	}
}
