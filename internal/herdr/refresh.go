package herdr

import (
	"fmt"
	"io"
	"time"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// RefreshDependencies are what a refresh of the sidebar reads and runs.
type RefreshDependencies struct {
	// Herdr is the herdr binary: HERDR_BIN_PATH, which herdr sets for its plugins, else herdr on PATH.
	Herdr string
	// Runs reads the queue of the running daemon; without a daemon there is none.
	Runs func() ([]protocol.RunStatus, error)
	// Now is the time the minutes of the runs count to.
	Now func() time.Time
	// Lock is the file whose lock runs the refreshes one after another.
	Lock string
	// Event is HERDR_PLUGIN_EVENT, the startup or event hook that runs the refresh, "" for the action.
	Event string
}

// Refresh is `heavy --herdr-refresh`: it reports the rows of each agent whose tokens changed and prints one line
// of what it did, which herdr keeps in the plugin log. Refreshes run one after another under a lock, so the last
// one always reads the final queue and a slow earlier one cannot report an older state after it.
func Refresh(deps RefreshDependencies, stdout io.Writer) error {
	unlock, err := lock(deps.Lock)
	if err != nil {
		return err
	}
	defer unlock()
	runs, err := deps.Runs()
	if err != nil {
		return err
	}
	listed, err := agents(deps.Herdr)
	if err != nil {
		return err
	}
	now := deps.Now().Unix()
	total, reported := 0, 0
	for _, entry := range listed {
		if entry.PaneID == "" {
			continue
		}
		total++
		changed, err := update(deps.Herdr, entry, runs, now)
		if err != nil {
			return err
		}
		if changed {
			reported++
		}
	}
	event := deps.Event
	if event == "" {
		event = "manual"
	}
	if _, err := fmt.Fprintf(stdout, "heavy-queue: event=%s agents=%d reported=%d\n", event, total, reported); err != nil {
		return fmt.Errorf("heavy: writing the refresh line: %w", err)
	}
	return nil
}

// update reports the rows of one agent where they differ from its tokens; changed says whether it reported any.
func update(herdr string, entry agentEntry, runs []protocol.RunStatus, now int64) (changed bool, err error) {
	checkout, err := isCheckout(entry.Cwd)
	if err != nil {
		return false, err
	}
	var own []protocol.RunStatus
	for _, run := range runs {
		if belongs(run, entry.PaneID, entry.Cwd, checkout) {
			own = append(own, run)
		}
	}
	rows, err := rowsOf(own, now)
	if err != nil {
		return false, err
	}
	calls := reports(fitted(rows), entry.Tokens)
	for _, arguments := range calls {
		if _, err := call(herdr, append([]string{"pane", "report-metadata", entry.PaneID, "--source", source}, arguments...)...); err != nil {
			return false, fmt.Errorf("heavy: reporting the heavy rows of pane %s: %w", entry.PaneID, err)
		}
	}
	return len(calls) > 0, nil
}
