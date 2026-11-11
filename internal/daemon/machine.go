package daemon

import (
	"time"

	"github.com/andreasisaak/heavy/internal/history"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// recentResults is how many results of the history the dashboard shows.
const recentResults = 5

// foreignFresh is how old the last look at the containers may be for the dashboard: two looks, since samples come
// a little later than every 2 s and a look takes up to a second, so the next one can start 12 s after the last.
const foreignFresh = 2 * foreignEvery

// machineStatus is the machine as the dashboard shows it; nil before the first sample. The containers outside the
// runs come from the last look, if it is at most 20 s old. The caller holds state.
func (s *server) machineStatus(now time.Time, recent []history.Record) *protocol.Machine {
	measurements := s.machine.Measurements(s.load)
	if measurements.Cores < 1 {
		return nil
	}
	machine := &protocol.Machine{
		Cores: measurements.Cores, VMMeasured: measurements.VMMeasured, VMAvailableBytes: measurements.VMAvailableBytes,
		SwapOutBytesPer2s: measurements.SwapOutBytesPer2s, Load1: measurements.Load1, HostFreePercent: measurements.HostFreePercent,
		FreeSeries: s.machine.FreeSeries(now), Foreign: []protocol.ForeignContainer{}, Recent: []protocol.Result{},
	}
	if now.Sub(s.biggestAt) <= foreignFresh {
		machine.Foreign = s.biggest
	}
	for _, record := range recent {
		machine.Recent = append(machine.Recent, protocol.Result{
			Label: record.Label, Checkout: record.Checkout, ExitStatus: record.ExitStatus, Reason: record.Reason,
			Started: record.Started, Ended: record.Ended,
		})
	}
	return machine
}
