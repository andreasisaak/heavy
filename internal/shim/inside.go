package shim

import (
	"github.com/andreasisaak/heavy/internal/client"
	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// activeRun asks a running daemon for the run id and reports it active while it runs or is paused (FR-10). It
// starts no daemon: without one, reached is false and the caller passes the call through (V15).
func activeRun(connect func() (daemon.ConnectOptions, error), id string) (run protocol.RunStatus, active, reached bool, err error) {
	options, err := connect()
	if err != nil {
		return protocol.RunStatus{}, false, false, err
	}
	status, found, err := client.StatusIfRunning(options)
	if err != nil || !found {
		return protocol.RunStatus{}, false, found, err
	}
	for _, run := range status.Runs {
		if run.ID == id && (run.State == "running" || run.State == "paused") {
			return run, true, true, nil
		}
	}
	return protocol.RunStatus{}, false, true, nil
}
