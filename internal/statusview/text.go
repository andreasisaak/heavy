// Package statusview prints the queue for `heavy --status`: aligned lines for people, JSON for scripts (G14).
package statusview

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// commandWidth is the longest command a status line shows; a longer one is cut with an ellipsis.
const commandWidth = 60

// Text prints one aligned line per run: state, id, command, directory (under home as ~), memory and time. Without
// an active run the first line says the queue is free.
func Text(runs []protocol.RunStatus, home string) string {
	var lines strings.Builder
	active := false
	for _, run := range runs {
		active = active || run.State != "finished"
	}
	if !active {
		lines.WriteString("heavy: no heavy run is active\n")
	}
	var rows [][]string
	for _, run := range runs {
		rows = append(rows, []string{state(run), run.ID, cut(run.Command), homeRelative(run.Directory, home), memory(run), moment(run)})
	}
	lines.WriteString(align(rows))
	return lines.String()
}

func state(run protocol.RunStatus) string {
	switch run.State {
	case "waiting":
		return fmt.Sprintf("waiting %d", run.Position)
	case "paused":
		return "paused " + run.PausedReason
	case "finished":
		if run.ExitStatus == 0 {
			return "passed"
		}
		return fmt.Sprintf("failed %d", run.ExitStatus)
	}
	return run.State
}

func cut(command string) string {
	if utf8.RuneCountInString(command) <= commandWidth {
		return command
	}
	return string([]rune(command)[:commandWidth-1]) + "…"
}

func homeRelative(directory, home string) string {
	if strings.HasPrefix(directory, home+"/") {
		return "~" + strings.TrimPrefix(directory, home)
	}
	return directory
}

// memory is the RSS of an active run, the expected peak of a waiting one (~? without history), - for a finished one.
func memory(run protocol.RunStatus) string {
	switch run.State {
	case "waiting":
		if run.PredictedBytes == 0 {
			return "~?"
		}
		return "~" + gibibytes(run.PredictedBytes)
	case "finished":
		return "-"
	}
	return gibibytes(run.RSSBytes)
}

func gibibytes(bytes int64) string {
	return fmt.Sprintf("%.1f GiB", float64(bytes)/(1<<30))
}

func moment(run protocol.RunStatus) string {
	if run.State == "finished" {
		return "ended " + time.Unix(run.Ended, 0).Format("15:04")
	}
	return "since " + time.Unix(run.Since, 0).Format("15:04")
}

// align pads every column but the last to its widest cell, two spaces apart, as column -t does.
func align(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for index, cell := range row {
			widths[index] = max(widths[index], utf8.RuneCountInString(cell))
		}
	}
	var lines strings.Builder
	for _, row := range rows {
		for index, cell := range row {
			lines.WriteString(cell)
			if index < len(row)-1 {
				lines.WriteString(strings.Repeat(" ", widths[index]-utf8.RuneCountInString(cell)+2))
			}
		}
		lines.WriteString("\n")
	}
	return lines.String()
}
