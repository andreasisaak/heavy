package dashboard

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/statusview"
)

// symbols mark the state of a run (design G13).
var symbols = map[string]string{"running": "▶", "paused": "⏸", "waiting": "⧗"}

// runRows are one row per open run: the cursor ›, the symbol, the minutes, #n for a waiting run, the label, the
// directory of the run, its memory and its cores or why it is paused. Without open runs the rows say so, unless the
// daemon has not answered.
func (m Model) runRows() []string {
	open := m.open()
	if len(open) == 0 {
		if m.fetchErr != nil {
			return nil
		}
		return []string{"  no heavy run in the queue"}
	}
	cells := make([][]cell, len(open))
	for index, current := range open {
		cells[index] = []cell{
			{text: fmt.Sprintf("%dm", statusview.Minutes(current, m.now)), right: true},
			{text: position(current)},
			{text: current.Label},
			{text: place(current.Directory)},
			{text: memory(current, m.options.Estimate())},
			{text: detail(current)},
		}
	}
	rows := make([]string, len(open))
	for index, aligned := range align(cells) {
		marker, style := " ", lipgloss.NewStyle()
		if index == m.cursor {
			marker, style = "›", selectedStyle
		}
		symbol := symbolStyles[open[index].State].Inherit(style).Render(symbols[open[index].State])
		rows[index] = style.Render(marker) + " " + symbol + " " + style.Render(aligned)
	}
	return rows
}

func position(run protocol.RunStatus) string {
	if run.State != "waiting" {
		return ""
	}
	return fmt.Sprintf("#%d", run.Position)
}

// memory is the resident memory of a running or paused run, the expected peak of a waiting one. A run without
// history shows the estimate heavy plans it with, also while it runs, and ~? while it waits without one.
func memory(run protocol.RunStatus, estimate int64) string {
	unknown := run.PredictedBytes == 0
	switch {
	case run.State != "waiting" && unknown && estimate > 0:
		return size(run.RSSBytes) + " · est. " + size(estimate)
	case run.State != "waiting":
		return size(run.RSSBytes)
	case unknown && estimate > 0:
		return "~" + size(estimate) + " estimated"
	case unknown:
		return "~? expected"
	}
	return "~" + size(run.PredictedBytes) + " expected"
}

// detail is the cores of a running run and the reason of a paused one, such as paused: memory.
func detail(run protocol.RunStatus) string {
	switch run.State {
	case "running":
		return cores(run.Budget)
	case "paused":
		return "paused: " + run.PausedReason
	}
	return ""
}

// cell is one column of a row, aligned left or right.
type cell struct {
	text  string
	right bool
}

// align pads the cells of each column to the widest, two spaces apart, and leaves out columns empty in every row
// and the empty cells at the end of a row, so no row ends in spaces.
func align(rows [][]cell) []string {
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for column, current := range row {
			widths[column] = max(widths[column], lipgloss.Width(current.text))
		}
	}
	aligned := make([]string, len(rows))
	for index, row := range rows {
		last := len(row) - 1
		for last >= 0 && row[last].text == "" {
			last--
		}
		var parts []string
		for column, current := range row[:last+1] {
			if widths[column] == 0 {
				continue
			}
			padding := strings.Repeat(" ", widths[column]-lipgloss.Width(current.text))
			if current.right {
				parts = append(parts, padding+current.text)
			} else if column < last {
				parts = append(parts, current.text+padding)
			} else {
				parts = append(parts, current.text)
			}
		}
		aligned[index] = strings.Join(parts, "  ")
	}
	return aligned
}
