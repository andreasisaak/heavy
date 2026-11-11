package herdr

import (
	"errors"
	"fmt"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// ForwardDependencies are what the action forward reads and runs.
type ForwardDependencies struct {
	// Herdr is the herdr binary, as for a refresh.
	Herdr string
	// Pane is HERDR_PANE_ID, the focused pane herdr names to a plugin action.
	Pane string
	// Runs reads the queue of the running daemon; without a daemon there is none.
	Runs func() ([]protocol.RunStatus, error)
	// Forward moves the run waiting at position one place ahead.
	Forward func(position int) error
}

// Forward is `heavy --herdr-forward`, the action forward (FR-31): it moves the first waiting run of the focused
// pane one place ahead and says what happened in a herdr notification. Bound to a key with
//
//	[[keys.command]]
//	key = "prefix+f"
//	type = "plugin_action"
//	command = "local.heavy-queue.forward"
func Forward(deps ForwardDependencies) error {
	if deps.Pane == "" {
		return errors.New("heavy: --herdr-forward needs HERDR_PANE_ID, the focused pane herdr sets for a plugin action")
	}
	runs, err := deps.Runs()
	if err != nil {
		return err
	}
	body := "no queued run in this pane"
	for _, run := range runs {
		if run.State != "waiting" || run.Pane != deps.Pane {
			continue
		}
		body = run.Label + " is next already"
		if run.Position > 1 {
			if err := deps.Forward(run.Position); err != nil {
				return err
			}
			body = fmt.Sprintf("moved %s to #%d", run.Label, run.Position-1)
		}
		break
	}
	if _, err := call(deps.Herdr, "notification", "show", "heavy", "--body", body); err != nil {
		return fmt.Errorf("heavy: %w", err)
	}
	return nil
}
