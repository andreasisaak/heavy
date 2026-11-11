package herdr_test

import (
	"path/filepath"
	"testing"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// FR-30: the row of a run that just ended shows its result and herdr drops it 30 s after the end
func TestEndedRunKeepsItsRowUntilThirtySecondsAfterItsEnd(t *testing.T) {
	main := filepath.Join(checkouts(t, "main"), "main")

	calls, _ := refreshed(t, []protocol.RunStatus{finished(0, 240, main, "test", 0)}, []agent{
		agentOf("w1:p1", main, map[string]string{"heavy": "▶ 4m · test"}),
	})

	check(t, "herdr calls", calls, [][]string{agentList, report("w1:p1", "--token", "heavy=✓ 4m · test", "--ttl-ms", "30000")})
}

// a run that ended next to a run of another label keeps its row; its expiry goes in a call of its own, since
// herdr takes one --ttl-ms per call
func TestEndedRunNextToAnActiveRunKeepsItsRow(t *testing.T) {
	main := filepath.Join(checkouts(t, "main"), "main")
	runs := []protocol.RunStatus{running(365, main, "ci"), finished(5, 30, main, "test", 1)}

	calls, _ := refreshed(t, runs, []agent{agentOf("w1:p1", main, map[string]string{"heavy_3": "⧗ 2m · #2 · lint"})})

	check(t, "herdr calls", calls, [][]string{
		agentList,
		report("w1:p1", "--token", "heavy=▶ 6m · ci", "--clear-token", "heavy_3"),
		report("w1:p1", "--token", "heavy_2=✗ <1m · test", "--ttl-ms", "25000"),
	})
}

// herdr shows no expiry in `herdr agent list`, so a row of an ended run is reported again on every refresh, with
// the time left until 30 s after its end
func TestRowOfAnEndedRunIsReportedAgainWithTheTimeLeft(t *testing.T) {
	main := filepath.Join(checkouts(t, "main"), "main")

	calls, _ := refreshed(t, []protocol.RunStatus{finished(10, 795, main, "ci", 0)}, []agent{
		agentOf("w1:p1", main, map[string]string{"heavy": "✓ 13m · ci"}),
	})

	check(t, "herdr calls", calls, [][]string{agentList, report("w1:p1", "--token", "heavy=✓ 13m · ci", "--ttl-ms", "20000")})
}

// the third row of four runs stays `+2 more` when the two runs it stands for end, and takes on the expiry of the
// later one
func TestMoreRowTakesTheExpiryOfTheRunsItStandsForWhenTheyEnd(t *testing.T) {
	main := filepath.Join(checkouts(t, "main"), "main")
	runs := []protocol.RunStatus{running(365, main, "ci"), running(240, main, "test"), finished(5, 90, main, "ui-test", 0), finished(10, 60, main, "lint", 1)}

	calls, _ := refreshed(t, runs, []agent{
		agentOf("w1:p1", main, map[string]string{"heavy": "▶ 6m · ci", "heavy_2": "▶ 4m · test", "heavy_3": "+2 more"}),
	})

	check(t, "herdr calls", calls, [][]string{agentList, report("w1:p1", "--token", "heavy_3=+2 more", "--ttl-ms", "25000")})
}
