package dashboard

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// keyRow names the keys under the dashboard.
const keyRow = "↑/↓ select · f forward · p pause/resume · s stop · q quit"

// pressed handles a key: ↑/↓ select, f forward, p pause or resume, s stop after y, q and ctrl+c quit (design G13).
// While the stop question is open, y ends the run and any other key but ctrl+c drops the question.
func (m Model) pressed(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	pressed := key.String()
	if pressed == "ctrl+c" {
		return m, tea.Quit
	}
	if m.asking != nil {
		asked := *m.asking
		m.asking = nil
		if pressed == "y" {
			return m.send(protocol.Message{Type: protocol.TypeTerminate, Terminate: &protocol.Target{ID: asked.ID}})
		}
		return m, nil
	}
	switch pressed {
	case "q":
		return m, tea.Quit
	case "up":
		return m.moveTo(m.cursor - 1), nil
	case "down":
		return m.moveTo(m.cursor + 1), nil
	case "f", "p", "s":
		return m.act(pressed)
	}
	return m, nil
}

// act runs the action of f, p or s on the selected run, or says why the key does nothing.
func (m Model) act(pressed string) (tea.Model, tea.Cmd) {
	selected := m.selectedRun()
	if selected == nil {
		m.note = note{text: "no run selected"}
		return m, nil
	}
	switch {
	case pressed == "f" && selected.State == "waiting":
		return m.send(protocol.Message{Type: protocol.TypeForward, Forward: &protocol.Forward{Position: selected.Position}})
	case pressed == "f":
		m.note = note{text: fmt.Sprintf("f moves a waiting run ahead; %s is %s", selected.Label, selected.State)}
	case pressed == "p" && selected.State == "running":
		return m.send(protocol.Message{Type: protocol.TypePause, Pause: &protocol.Target{ID: selected.ID}})
	case pressed == "p" && selected.State == "paused":
		return m.send(protocol.Message{Type: protocol.TypeResume, Resume: &protocol.Target{ID: selected.ID}})
	case pressed == "p":
		m.note = note{text: fmt.Sprintf("p pauses a running run and resumes a paused one; %s is %s", selected.Label, selected.State)}
	default:
		m.asking = selected
		m.note = note{}
	}
	return m, nil
}

// send clears the line under the keys and sends the message without blocking the view; the answer comes back as
// an answerMsg.
func (m Model) send(message protocol.Message) (tea.Model, tea.Cmd) {
	m.note = note{}
	send := m.options.Send
	return m, func() tea.Msg {
		line, err := send(message)
		return answerMsg{line: line, err: err}
	}
}

// statusLine is the stop question or the note, "" without either.
func (m Model) statusLine() string {
	switch {
	case m.asking != nil:
		return promptStyle.Render(fmt.Sprintf("stop %s? y/n", m.asking.Label))
	case m.note.failed:
		return failedStyle.Render(m.note.text)
	}
	return m.note.text
}
