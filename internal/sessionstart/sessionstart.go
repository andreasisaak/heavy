// Package sessionstart is `heavy --hook session-start` (FR-35): it puts the shims in front of PATH and exports the
// session id through CLAUDE_ENV_FILE, registers the session for its monitor (design G9) and generates the herdr
// plugin of this heavy in CLAUDE_PLUGIN_DATA (design G12). Its stdout becomes context for Claude, so it writes
// nothing there.
package sessionstart

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/andreasisaak/heavy/internal/herdr"
	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/sessions"
	"github.com/andreasisaak/heavy/internal/shell"
)

// Dependencies are what the hook reads and writes besides its input.
type Dependencies struct {
	// EnvFile is CLAUDE_ENV_FILE, "" when Claude Code did not set it.
	EnvFile string
	// Shims is the shims folder of the plugin.
	Shims          string
	StateDirectory string
	// ClaudePID is the PID of Claude Code, named by hooks/build.sh (HEAVY_CLAUDE_PID).
	ClaudePID int
	// Alive reports whether a process lives; the registrations of ended Claude Code processes other than ClaudePID go.
	Alive func(pid int) bool
	Now   func() time.Time
	// HerdrPlugin is the herdr plugin of this heavy, with Data "" when Claude Code did not set CLAUDE_PLUGIN_DATA.
	HerdrPlugin herdr.Plugin
}

// input is the part of the SessionStart input the hook reads; Claude Code sends more fields, which it ignores.
type input struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
}

// Run handles one session start and returns the exit status; notes and errors go to stderr.
func Run(stdin io.Reader, stdout, stderr io.Writer, deps Dependencies) int {
	raw, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "heavy: reading the SessionStart input: %v\n", err)
		return 1
	}
	var start input
	if err := json.Unmarshal(raw, &start); err != nil || start.SessionID == "" {
		fmt.Fprintf(stderr, "heavy: expected a session_id in the SessionStart input, got %s\n", strings.TrimSpace(string(raw)))
		return 1
	}
	if err := writeEnvironment(start, deps, stderr); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := register(start, deps); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	// The own registration stays whatever Alive says: its Claude Code started this hook.
	kept := func(pid int) bool { return pid == deps.ClaudePID || deps.Alive(pid) }
	if _, err := sessions.Prune(deps.StateDirectory, kept); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if deps.HerdrPlugin.Data == "" {
		fmt.Fprintln(stderr, "heavy: CLAUDE_PLUGIN_DATA is not set, so the herdr plugin is not generated")
		return 0
	}
	if err := deps.HerdrPlugin.Generate(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// writeEnvironment puts the shims in front of PATH and HEAVY_SESSION into CLAUDE_ENV_FILE; without one it notes
// on stderr that only the hook queues.
func writeEnvironment(start input, deps Dependencies, stderr io.Writer) error {
	if deps.EnvFile == "" {
		fmt.Fprintln(stderr, "heavy: CLAUDE_ENV_FILE is not set, so the shims stay off PATH and only the PreToolUse hook queues heavy calls")
		return nil
	}
	shims := shell.Quote(deps.Shims)
	lines := fmt.Sprintf("case \"$PATH\" in %s:*) ;; *) export PATH=%s:\"$PATH\" ;; esac\nexport HEAVY_SESSION=%s\n", shims, shims, shell.Quote(start.SessionID))
	file, err := os.OpenFile(deps.EnvFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("heavy: opening CLAUDE_ENV_FILE %s: %w", deps.EnvFile, err)
	}
	defer file.Close()
	if _, err := file.WriteString(lines); err != nil {
		return fmt.Errorf("heavy: writing CLAUDE_ENV_FILE %s: %w", deps.EnvFile, err)
	}
	return nil
}

// register writes the session with its working directory and main checkout under the PID of Claude Code.
func register(start input, deps Dependencies) error {
	checkout := start.Cwd
	if found, inside, err := lists.FindCheckout(start.Cwd); err != nil {
		return err
	} else if inside {
		checkout = found.Main
	}
	return sessions.Write(deps.StateDirectory, deps.ClaudePID, sessions.Registration{SessionID: start.SessionID, Cwd: start.Cwd, Checkout: checkout, Time: deps.Now().Unix()})
}
