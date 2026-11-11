// Package herdr is heavy's side of the herdr plugin local.heavy-queue (design G12). `heavy --herdr-refresh` shows
// each run of an agent in a sidebar row of its own, as the pane tokens heavy, heavy_2 and heavy_3 (FR-30);
// `heavy --herdr-forward` moves the queued run of the focused pane one place ahead (FR-31). The manifest herdr
// links lies in CLAUDE_PLUGIN_DATA and runs the newest installed heavy by its absolute path; the SessionStart
// hook and the guard generate it.
package herdr

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// source is the metadata source heavy reports its tokens under, as 0.6 did.
const source = "heavy-queue"

// call runs herdr with the arguments and returns its stdout; a failure names the call, the exit and herdr's stderr.
func call(herdr string, arguments ...string) ([]byte, error) {
	command := exec.Command(herdr, arguments...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("%s %s failed: %w: %s", herdr, strings.Join(arguments, " "), err, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}
