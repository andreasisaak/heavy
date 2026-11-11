package statusview

import (
	"fmt"
	"math"
	"time"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// describedWidth is how many characters of a run's command the hooks show.
const describedWidth = 60

// open are the states of a run that has not ended.
var open = map[string]bool{"waiting": true, "running": true, "paused": true}

// Own are the open runs of the session, by the session id the daemon recorded.
func Own(runs []protocol.RunStatus, session string) []protocol.RunStatus {
	var own []protocol.RunStatus
	for _, run := range runs {
		if open[run.State] && run.Session == session {
			own = append(own, run)
		}
	}
	return own
}

// Describe names a run and its minutes for the hooks: `make ci` waits at position 2 for 6 min, `make ci` runs for
// 4 min, or `make ci` is paused, started 9 min ago.
func Describe(run protocol.RunStatus, now time.Time) string {
	command := run.Command
	if characters := []rune(command); len(characters) > describedWidth {
		command = string(characters[:describedWidth-1]) + "…"
	}
	switch run.State {
	case "waiting":
		return fmt.Sprintf("`%s` waits at position %d for %d min", command, run.Position, Minutes(run, now))
	case "paused":
		return fmt.Sprintf("`%s` is paused, started %d min ago", command, Minutes(run, now))
	}
	return fmt.Sprintf("`%s` runs for %d min", command, Minutes(run, now))
}

// Minutes since the run arrived or started, rounded down as 0.6 did with //.
func Minutes(run protocol.RunStatus, now time.Time) int {
	return int(math.Floor(now.Sub(time.Unix(run.Since, 0)).Seconds() / 60))
}
