package client_test

import (
	"testing"

	heavyclient "github.com/andreasisaak/heavy/internal/client"
	"github.com/andreasisaak/heavy/internal/daemon"
)

// plan 4.1/4.3: hooks and shims read the queue of a running daemon and start none
func TestStatusIfRunningReadsTheQueueOfARunningDaemonOnly(t *testing.T) {
	e := setup(t)
	options := daemon.ConnectOptions{StateDirectory: e.state}

	_, foundBefore, errBefore := heavyclient.StatusIfRunning(options)
	_, id := e.runningSleep(t, "30.519")
	status, found, err := heavyclient.StatusIfRunning(options)

	if foundBefore || errBefore != nil {
		t.Fatalf("without a daemon StatusIfRunning = %v, %v; want nothing found", foundBefore, errBefore)
	}
	if err != nil || !found || len(status.Runs) != 1 || status.Runs[0].ID != id || status.Runs[0].State != "running" {
		t.Fatalf("StatusIfRunning = %+v, %v, %v; want the running run %s", status, found, err, id)
	}
}
