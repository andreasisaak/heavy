package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/herdr"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/sessions"
	"github.com/andreasisaak/heavy/internal/sessionstart"
)

// runSessionStartHook is `heavy --hook session-start`: the shims folder lies next to bin/ in the plugin, the PID of
// Claude Code comes from HEAVY_CLAUDE_PID, which hooks/build.sh sets to its parent, and the herdr plugin runs this
// binary.
func runSessionStartHook(stdin io.Reader, stdout, stderr io.Writer) int {
	claude, err := strconv.Atoi(os.Getenv("HEAVY_CLAUDE_PID"))
	if err != nil || claude <= 1 {
		fmt.Fprintln(stderr, "heavy: HEAVY_CLAUDE_PID is not set; the SessionStart hook runs through hooks/build.sh, which names the PID of Claude Code")
		return 1
	}
	binary, err := ownBinary()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	identity, err := protocol.LocalIdentity(binary)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	state, err := daemon.StateDirectory()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return sessionstart.Run(stdin, stdout, stderr, sessionstart.Dependencies{
		EnvFile: os.Getenv("CLAUDE_ENV_FILE"), Shims: filepath.Join(filepath.Dir(filepath.Dir(binary)), "shims"),
		StateDirectory: state, ClaudePID: claude, Alive: sessions.Alive, Now: time.Now,
		HerdrPlugin: herdr.Plugin{Data: os.Getenv("CLAUDE_PLUGIN_DATA"), Version: identity.Version, Heavy: binary},
	})
}
