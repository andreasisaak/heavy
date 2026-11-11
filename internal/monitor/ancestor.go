package monitor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/andreasisaak/heavy/internal/sessions"
)

// claudePrograms are the names a Claude Code process runs under: the native install, and node for an npm install.
var claudePrograms = map[string]bool{"claude": true, "node": true}

// locate is the target of --session, or the nearest Claude Code process above the monitor once its registration
// is there; the search repeats every RetryPause until FindFor has passed. It returns ctx.Err() when ctx ends first.
func locate(ctx context.Context, deps Dependencies) (target, error) {
	if deps.Session != "" {
		return target{session: deps.Session}, nil
	}
	deadline := time.Now().Add(deps.FindFor)
	for {
		pid, err := registeredClaude(deps)
		if err != nil {
			return target{}, err
		}
		if pid > 0 {
			return target{claudePID: pid}, nil
		}
		if !time.Now().Before(deadline) {
			return target{}, fmt.Errorf("heavy: found no registered Claude Code session among the ancestors of pid %d", deps.PID)
		}
		if !pause(ctx, deps.RetryPause) {
			return target{}, ctx.Err()
		}
	}
}

// registeredClaude is the PID of the nearest Claude Code process above the monitor when its registration was
// written since that process started, else 0. Registrations are never deleted, so a farther ancestor or an older
// registration belongs to another session: an outer Claude Code, or an ended one whose PID came back.
func registeredClaude(deps Dependencies) (int, error) {
	pid, claude, err := nearestClaude(deps)
	if err != nil || pid == 0 {
		return 0, err
	}
	registration, err := sessions.Read(deps.StateDirectory, pid)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if registration.Time < claude.Started.Unix()-startRounding {
		return 0, nil
	}
	return pid, nil
}

// startRounding is how many seconds a registration may lie before the start of its process: the registration holds
// whole seconds, the start microseconds.
const startRounding = 1

// nearestClaude walks up from the parent of the monitor to the first process that runs a Claude Code program; 0
// when the walk reaches PID 1, which is never Claude Code.
func nearestClaude(deps Dependencies) (int, Process, error) {
	process, err := deps.Process(deps.PID)
	for err == nil && process.Parent > 1 {
		pid := process.Parent
		process, err = deps.Process(pid)
		if err == nil && claudePrograms[process.Name] {
			return pid, process, nil
		}
	}
	return 0, Process{}, err
}
