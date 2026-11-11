package dashboard

import (
	"bytes"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// exampleNow is the clock of every case: 20:41:30 in a zone one hour east of UTC.
var exampleNow = time.Date(2026, 11, 8, 20, 41, 30, 0, time.FixedZone("CET", 3600))

func ago(duration time.Duration) int64 {
	return exampleNow.Add(-duration).Unix()
}

func gibibytes(amount float64) int64 {
	return int64(amount * float64(gibibyte))
}

// fakeDaemon answers the fetches with its statuses in turn, the last one again and again, and records what the
// dashboard sends.
type fakeDaemon struct {
	answers  []fetchAnswer
	fetches  int
	sent     []protocol.Message
	sendLine string
	sendErr  error
}

type fetchAnswer struct {
	status protocol.Status
	err    error
}

func (f *fakeDaemon) fetch() (protocol.Status, error) {
	answer := f.answers[min(f.fetches, len(f.answers)-1)]
	f.fetches++
	return answer.status, answer.err
}

func (f *fakeDaemon) send(message protocol.Message) (string, error) {
	f.sent = append(f.sent, message)
	return f.sendLine, f.sendErr
}

// started is the dashboard at width 80 after its first fetch.
func started(t *testing.T, daemon *fakeDaemon) Model {
	t.Helper()
	model := New(Options{Host: "Mac", Fetch: daemon.fetch, Send: daemon.send, Now: func() time.Time { return exampleNow }, Estimate: func() int64 { return 0 }})
	model = step(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
	return step(t, model, run(t, model.Init(), "Init"))
}

// step hands the message to Update and returns the next model; its command is dropped.
func step(t *testing.T, model Model, message tea.Msg) Model {
	t.Helper()
	next, _ := model.Update(message)
	updated, isModel := next.(Model)
	if !isModel {
		t.Fatalf("Update returned a %T, want a dashboard.Model", next)
	}
	return updated
}

// press hands a key press to Update and returns the next model and the message of its command, nil without one.
func press(t *testing.T, model Model, key tea.KeyPressMsg) (Model, tea.Msg) {
	t.Helper()
	next, command := model.Update(key)
	updated, isModel := next.(Model)
	if !isModel {
		t.Fatalf("Update returned a %T, want a dashboard.Model", next)
	}
	if command == nil {
		return updated, nil
	}
	return updated, command()
}

func run(t *testing.T, command tea.Cmd, from string) tea.Msg {
	t.Helper()
	if command == nil {
		t.Fatalf("%s returned no command", from)
	}
	return command()
}

func letter(character rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: character, Text: string(character)}
}

var (
	keyDown  = tea.KeyPressMsg{Code: tea.KeyDown}
	keyUp    = tea.KeyPressMsg{Code: tea.KeyUp}
	keyCtrlC = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
)

// plain is the content of the view as a terminal without colours shows it: lipgloss v2 styles always carry their
// escape sequences, and the colour profile of the output strips them (NoTTY drops every sequence).
func plain(t *testing.T, content string) string {
	t.Helper()
	var text bytes.Buffer
	writer := colorprofile.Writer{Forward: &text, Profile: colorprofile.NoTTY}
	if _, err := writer.WriteString(content); err != nil {
		t.Fatalf("stripping the escape sequences: %v", err)
	}
	return text.String()
}

// exampleRuns are a running, a paused and a waiting run, and a finished one the dashboard leaves out.
func exampleRuns() []protocol.RunStatus {
	return []protocol.RunStatus{
		{State: "running", ID: "run-ci", Label: "ci", Directory: "/Users/example/sites/shopfront-csv-export", Checkout: "/Users/example/sites/shopfront",
			Since: ago(12*time.Minute + 10*time.Second), RSSBytes: gibibytes(2.5), Budget: 9},
		{State: "paused", ID: "run-test", Label: "test", Directory: "/Users/example/sites/shopfront-communication", Checkout: "/Users/example/sites/shopfront",
			Since: ago(4*time.Minute + 5*time.Second), RSSBytes: gibibytes(1.1), Budget: 6, PausedReason: "memory"},
		{State: "waiting", ID: "run-ui", Label: "ui-test", Directory: "/Users/example/sites/shopfront-performance", Checkout: "/Users/example/sites/shopfront",
			Since: ago(3 * time.Minute), Position: 1, PredictedBytes: gibibytes(3)},
		{State: "finished", ID: "run-lint", Label: "lint", Directory: "/Users/example/sites/shopfront", Checkout: "/Users/example/sites/shopfront",
			Since: ago(9 * time.Minute), Ended: ago(time.Minute), ExitStatus: 0},
	}
}

// exampleMachine is the machine with a series of ten values, two foreign containers and two results.
func exampleMachine() *protocol.Machine {
	return &protocol.Machine{
		Cores: 18, VMMeasured: true, VMAvailableBytes: gibibytes(1.7), SwapOutBytesPer2s: 83 * mebibyte, Load1: 6.8, HostFreePercent: 8,
		FreeSeries: []int64{8 * gibibyte, 8 * gibibyte, 7 * gibibyte, 6 * gibibyte, 4 * gibibyte, 3 * gibibyte, 3 * gibibyte, 2 * gibibyte, 3 * gibibyte, 4 * gibibyte},
		Foreign: []protocol.ForeignContainer{
			{Name: "shopfront-app-1", Checkout: "/Users/example/sites/shopfront", Bytes: gibibytes(10.1)},
			{Name: "spa-watch", Checkout: "/Users/example/sites/shopfront", Bytes: gibibytes(1.5)},
		},
		Recent: []protocol.Result{
			{Label: "ci", Checkout: "/Users/example/sites/shopfront", ExitStatus: 0, Started: ago(52 * time.Minute), Ended: ago(39 * time.Minute)},
			{Label: "test", Checkout: "/Users/example/sites/shopfront", ExitStatus: 2, Reason: "red", Started: ago(67 * time.Minute), Ended: ago(61 * time.Minute)},
		},
	}
}
