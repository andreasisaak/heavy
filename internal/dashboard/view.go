package dashboard

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/andreasisaak/heavy/internal/platform"
)

// The styles of the dashboard. lipgloss v2 always writes their escape sequences; bubbletea downsamples them to the
// colour profile of the terminal and strips them where it has none.
var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Bold(true)
	keyStyle      = lipgloss.NewStyle().Faint(true)
	promptStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Yellow)
	passedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Green)
	failedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Red)
	symbolStyles  = map[string]lipgloss.Style{
		"running": lipgloss.NewStyle().Foreground(lipgloss.Green),
		"paused":  lipgloss.NewStyle().Foreground(lipgloss.Yellow),
		"waiting": lipgloss.NewStyle().Foreground(lipgloss.Blue),
	}
)

// View draws the dashboard in the alternate screen: the header, the runs, the machine and the keys, as blocks one
// empty line apart, every line cut to the width of the terminal (design G13).
func (m Model) View() tea.View {
	var blocks []string
	for _, block := range [][]string{m.header(), m.runRows(), m.machineRows(), m.footer()} {
		if len(block) > 0 {
			blocks = append(blocks, strings.Join(fitted(block, m.width), "\n"))
		}
	}
	view := tea.NewView(strings.Join(blocks, "\n\n"))
	view.AltScreen = true
	return view
}

// header is the machine and the clock, or why the dashboard shows no numbers.
func (m Model) header() []string {
	clock := m.now.Format("15:04")
	title := titleStyle.Render("heavy") + " · " + m.options.Host + " · "
	machine := m.status.Machine
	switch {
	case m.fetchErr != nil:
		return []string{
			rightAligned(title+failedStyle.Render("daemon not reachable, retrying"), clock, m.width),
			failedStyle.Render(m.fetchErr.Error()),
		}
	case machine == nil:
		return []string{rightAligned(title+"waiting for the first sample", clock, m.width)}
	}
	free := platform.Docker + " not measured"
	if machine.VMMeasured {
		free = platform.Docker + " free " + size(machine.VMAvailableBytes)
	}
	return []string{
		rightAligned(title+fmt.Sprintf("%s · load %.1f", cores(machine.Cores), machine.Load1), clock, m.width),
		fmt.Sprintf("%s · host free %d %% · swap %s/2s", free, machine.HostFreePercent, size(machine.SwapOutBytesPer2s)),
	}
}

// machineRows are the free memory of the last 30 minutes, the biggest containers outside the runs and the last
// results; a row without values is left out.
func (m Model) machineRows() []string {
	machine := m.status.Machine
	if machine == nil {
		return nil
	}
	var rows []string
	if len(machine.FreeSeries) > 0 {
		rows = append(rows, "memory free 30 min  "+Sparkline(machine.FreeSeries))
	}
	if len(machine.Foreign) > 0 {
		var containers []string
		for _, container := range machine.Foreign {
			shown := container.Name + " " + size(container.Bytes)
			if container.Checkout != "" {
				shown += " (" + place(container.Checkout) + ")"
			}
			containers = append(containers, shown)
		}
		rows = append(rows, "foreign  "+strings.Join(containers, " · "))
	}
	if len(machine.Recent) > 0 {
		var results []string
		for _, result := range machine.Recent {
			mark := passedStyle.Render("✓")
			if result.ExitStatus != 0 {
				mark = failedStyle.Render("✗")
			}
			ended := time.Unix(result.Ended, 0).In(m.now.Location()).Format("15:04")
			results = append(results, fmt.Sprintf("%s %s %dm %s %s", mark, result.Label, (result.Ended-result.Started)/60, place(result.Checkout), ended))
		}
		rows = append(rows, "recent  "+strings.Join(results, " · "))
	}
	return rows
}

// footer is the key row and, under it, the stop question or the answer to the last action.
func (m Model) footer() []string {
	rows := []string{keyStyle.Render(keyRow)}
	if line := m.statusLine(); line != "" {
		rows = append(rows, line)
	}
	return rows
}
