// Package dashboard is `heavy --dashboard`: a live view of the queue, the machine and the recent results, with keys
// to move, pause, resume and stop runs (design G13, FR-33).
package dashboard

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// RefreshEvery is the pause between an answer of the daemon and the next question (FR-33).
const RefreshEvery = 2 * time.Second

// defaultWidth is the width the dashboard draws for until the terminal reports its size.
const defaultWidth = 80

// Options are what the dashboard needs from outside.
type Options struct {
	// Host names the machine in the header.
	Host string
	// Fetch asks the daemon for the queue and the machine (protocol.Status with Detail).
	Fetch func() (protocol.Status, error)
	// Send sends a forward, pause, resume or terminate message and returns the daemon's answer line.
	Send func(protocol.Message) (string, error)
	// Now is the clock; the location of its times is the one the dashboard shows times in.
	Now func() time.Time
}

// Model is the state of the dashboard: the last queue the daemon sent, the selected run, the open stop question
// and the line under the keys.
type Model struct {
	options Options
	width   int
	// status is the last answer of the daemon; a failed fetch keeps it and sets fetchErr.
	status   protocol.Status
	fetchErr error
	// now is the time of the last answer, for the clock and the minutes.
	now time.Time
	// cursor is the row of the selected run, selected its id, so the selection follows its run through a refresh.
	cursor   int
	selected string
	// asking is the run the stop question is about.
	asking *protocol.RunStatus
	note   note
}

// note is the line under the keys: the daemon's answer to the last action, or why a key did nothing.
type note struct {
	text   string
	failed bool
}

// tickMsg starts the next fetch.
type tickMsg struct{}

// fetchedMsg is the answer of the daemon to a fetch.
type fetchedMsg struct {
	status protocol.Status
	err    error
}

// answerMsg is the daemon's answer to an action.
type answerMsg struct {
	line string
	err  error
}

// New is the dashboard before its first answer from the daemon.
func New(options Options) Model {
	return Model{options: options, width: defaultWidth, now: options.Now()}
}

// Init asks the daemon for the first time.
func (m Model) Init() tea.Cmd {
	return m.fetch
}

// Update takes a message of the program and returns the next state. A fetch starts only after the answer to the
// last one, so two never overlap.
func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
	case fetchedMsg:
		return m.answered(message), tea.Tick(RefreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
	case tickMsg:
		return m, m.fetch
	case answerMsg:
		m.note = note{text: message.line}
		if message.err != nil {
			m.note = note{text: message.err.Error(), failed: true}
		}
	case tea.KeyPressMsg:
		return m.pressed(message)
	}
	return m, nil
}

func (m Model) fetch() tea.Msg {
	status, err := m.options.Fetch()
	return fetchedMsg{status: status, err: err}
}

// answered keeps the new queue and moves the cursor to the selected run, or to the last row when it has ended.
func (m Model) answered(message fetchedMsg) Model {
	m.now = m.options.Now()
	m.fetchErr = message.err
	if message.err != nil {
		return m
	}
	m.status = message.status
	open := m.open()
	for index, current := range open {
		if current.ID == m.selected {
			m.cursor = index
		}
	}
	return m.moveTo(m.cursor)
}

// moveTo moves the cursor to the row, kept within the open runs.
func (m Model) moveTo(row int) Model {
	open := m.open()
	m.cursor = max(0, min(row, len(open)-1))
	m.selected = ""
	if len(open) > 0 {
		m.selected = open[m.cursor].ID
	}
	return m
}

// open are the runs that wait, run or are paused, in the daemon's order; finished runs show under recent.
func (m Model) open() []protocol.RunStatus {
	var open []protocol.RunStatus
	for _, current := range m.status.Runs {
		if current.State == "running" || current.State == "paused" || current.State == "waiting" {
			open = append(open, current)
		}
	}
	return open
}

// selectedRun is the run under the cursor, nil without open runs.
func (m Model) selectedRun() *protocol.RunStatus {
	open := m.open()
	if len(open) == 0 {
		return nil
	}
	return &open[m.cursor]
}
