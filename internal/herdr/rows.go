package herdr

import (
	"fmt"
	"strconv"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// tokenNames are the sidebar rows of an agent from top to bottom (FR-30); herdr 0.9.1 takes the underscore
// (probe 2026-11-08).
var tokenNames = []string{"heavy", "heavy_2", "heavy_3"}

// row is one run in the sidebar: a symbol, the minutes and the short name, such as `▶ 6m · ci` or
// `⧗ 3m · #1 · ci`, so a cut in a narrow sidebar only ever hits the name. ttl is how many milliseconds herdr keeps
// the row of a run that ended, 0 for an open run.
type row struct {
	value string
	ttl   int64
}

// rowsOf are the rows of an agent's runs: the open ones, then those that ended in the last 30 s. An open run
// hides the result of an ended run of its label, the run it repeats.
func rowsOf(runs []protocol.RunStatus, now int64) ([]row, error) {
	openLabels := map[string]bool{}
	for _, run := range runs {
		if run.State != "finished" {
			openLabels[run.Label] = true
		}
	}
	var open, ended []row
	for _, run := range runs {
		if run.State == "finished" && openLabels[run.Label] {
			continue
		}
		current, err := rowOf(run, now)
		if err != nil {
			return nil, err
		}
		if run.State == "finished" {
			ended = append(ended, current)
		} else {
			open = append(open, current)
		}
	}
	return append(open, ended...), nil
}

func rowOf(run protocol.RunStatus, now int64) (row, error) {
	name := run.Label
	if run.Agent != "" {
		name += " ↳ " + run.Agent
	}
	switch run.State {
	case "running":
		return row{value: "▶ " + minutes(now-run.Since) + " · " + name}, nil
	case "paused":
		return row{value: "⏸ " + minutes(now-run.Since) + " · " + name}, nil
	case "waiting":
		return row{value: fmt.Sprintf("⧗ %s · #%d · %s", minutes(now-run.Since), run.Position, name)}, nil
	case "finished":
		symbol := "✓"
		if run.ExitStatus != 0 {
			symbol = "✗"
		}
		return row{value: symbol + " " + minutes(run.Ended-run.Since) + " · " + name, ttl: max((run.Expires-now)*1000, 1000)}, nil
	}
	return row{}, fmt.Errorf("heavy: run %s has the state %s, expected waiting, running, paused or finished", run.ID, run.State)
}

// minutes are seconds as the sidebar shows them: <1m, 6m, 1h05m.
func minutes(seconds int64) string {
	switch {
	case seconds < 60:
		return "<1m"
	case seconds < 3600:
		return strconv.FormatInt(seconds/60, 10) + "m"
	}
	return fmt.Sprintf("%dh%02dm", seconds/3600, seconds%3600/60)
}

// fitted puts the rows into the tokens: from four rows on, the last token counts the rows it stands for and
// expires with the last of them, or never while one of them is open.
func fitted(rows []row) []row {
	if len(rows) <= len(tokenNames) {
		return rows
	}
	kept := len(tokenNames) - 1
	more := row{value: fmt.Sprintf("+%d more", len(rows)-kept)}
	for _, hidden := range rows[kept:] {
		if hidden.ttl == 0 {
			more.ttl = 0
			break
		}
		more.ttl = max(more.ttl, hidden.ttl)
	}
	return append(rows[:kept:kept], more)
}

// reports are the arguments of the report-metadata calls that turn the agent's current tokens into the rows: one
// call for the rows of open runs and the tokens to clear, one for each row that expires, since herdr takes one
// --ttl-ms per call. A row without expiry that its token already shows is left alone. A row that expires is
// reported on every refresh: `herdr agent list` shows no expiry, and a token of an open run that keeps its text
// when its runs end, such as `+2 more`, would otherwise never expire. Its ttl counts to 30 s after the end, so
// the report says the same each time.
func reports(rows []row, current map[string]string) [][]string {
	var plain []string
	var expiring [][]string
	for index, name := range tokenNames {
		var wanted row
		if index < len(rows) {
			wanted = rows[index]
		}
		switch {
		case wanted.ttl == 0 && wanted.value == current[name]:
		case wanted.value == "":
			plain = append(plain, "--clear-token", name)
		case wanted.ttl == 0:
			plain = append(plain, "--token", name+"="+wanted.value)
		default:
			expiring = append(expiring, []string{"--token", name + "=" + wanted.value, "--ttl-ms", strconv.FormatInt(wanted.ttl, 10)})
		}
	}
	if len(plain) == 0 {
		return expiring
	}
	return append([][]string{plain}, expiring...)
}
