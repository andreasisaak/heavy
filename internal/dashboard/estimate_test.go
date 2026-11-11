package dashboard

import (
	"strings"
	"testing"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// The dashboard names the estimate a run without history is planned with, while it waits and while it runs.
func TestMemoryNamesTheEstimateOfARunWithoutHistory(t *testing.T) {
	cases := []struct {
		name     string
		run      protocol.RunStatus
		estimate int64
		want     string
	}{
		{"waiting without history", protocol.RunStatus{State: "waiting"}, 8 * gibibyte, "~8.0 GiB estimated"},
		{"waiting without history and without the key", protocol.RunStatus{State: "waiting"}, 0, "~? expected"},
		{"waiting with history", protocol.RunStatus{State: "waiting", PredictedBytes: 3 * gibibyte}, 8 * gibibyte, "~3.0 GiB expected"},
		{"running without history", protocol.RunStatus{State: "running", RSSBytes: 5 * gibibyte / 2}, 8 * gibibyte, "2.5 GiB · est. 8.0 GiB"},
		{"paused without history", protocol.RunStatus{State: "paused", RSSBytes: 512 * mebibyte}, 8 * gibibyte, "512 MiB · est. 8.0 GiB"},
		{"running without history and without the key", protocol.RunStatus{State: "running", RSSBytes: 5 * gibibyte / 2}, 0, "2.5 GiB"},
		{"running with history", protocol.RunStatus{State: "running", RSSBytes: 5 * gibibyte / 2, PredictedBytes: gibibyte}, 8 * gibibyte, "2.5 GiB"},
	}
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			if got := memory(current.run, current.estimate); got != current.want {
				t.Fatalf("memory = %q, want %q", got, current.want)
			}
		})
	}
}

// The view asks for the estimate with every frame, so a changed personal list shows without a restart.
func TestViewShowsTheEstimateTheOptionsGive(t *testing.T) {
	daemon := &fakeDaemon{answers: []fetchAnswer{{status: protocol.Status{Runs: []protocol.RunStatus{
		{State: "waiting", ID: "run-ui", Label: "ui-test", Directory: "/Users/example/sites/shopfront-performance", Checkout: "/Users/example/sites/shopfront",
			Since: exampleNow.Add(-3 * 60 * 1e9).Unix(), Position: 1},
	}}}}}
	model := started(t, daemon)
	model.options.Estimate = func() int64 { return 6 * gibibyte }

	got := screen(t, model)

	want := "› ⧗ 3m  #1  ui-test  shopfront-performance  ~6.0 GiB estimated"
	if lines := strings.Split(got.Content, "\n"); len(lines) < 3 || lines[2] != want {
		t.Fatalf("view =\n%s\nwant the run line %q", got.Content, want)
	}
}
