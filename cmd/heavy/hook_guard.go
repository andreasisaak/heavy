package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/andreasisaak/heavy/internal/client"
	"github.com/andreasisaak/heavy/internal/current"
	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/detect"
	"github.com/andreasisaak/heavy/internal/guard"
	"github.com/andreasisaak/heavy/internal/herdr"
	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// runGuardHook is `heavy --hook guard`, the PreToolUse hook for Bash: it reads the hook input on stdin and writes
// the answer of internal/guard to stdout. The queue comes from a running daemon, and the hook starts none: without
// a daemon the queue is free. An error before the guard runs denies the call (design G15).
func runGuardHook(stdin io.Reader, stdout, stderr io.Writer) int {
	dependencies, err := guardDependencies(stderr)
	if err == nil {
		err = guard.Run(stdin, stdout, dependencies)
	} else {
		err = guard.Deny(stdout, err.Error())
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func guardDependencies(stderr io.Writer) (guard.Dependencies, error) {
	binary, err := ownBinary()
	if err != nil {
		return guard.Dependencies{}, err
	}
	identity, err := protocol.LocalIdentity(binary)
	if err != nil {
		return guard.Dependencies{}, err
	}
	state, err := daemon.StateDirectory()
	if err != nil {
		return guard.Dependencies{}, err
	}
	personal, err := lists.PersonalPath()
	if err != nil {
		return guard.Dependencies{}, err
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	// The hook runs with every Bash call of every session, so the first call after an update turns the link to the
	// shims to this heavy, also for the sessions that started before it. A link that cannot be set stops no call.
	if err := current.Point(state, filepath.Dir(filepath.Dir(binary)), false); err != nil {
		logger.Warn("the link to the shims stays as it is", "error", err)
	}
	options := daemon.ConnectOptions{StateDirectory: state, Identity: identity}
	return guard.Dependencies{
		Heavy:        binary,
		PersonalList: personal,
		Queue: func() (protocol.Status, error) {
			status, _, err := client.StatusIfRunning(options)
			return status, err
		},
		Container:   detect.ContainerLookup(os.Getenv("PATH"), logger),
		Now:         time.Now,
		Log:         logger,
		HerdrPlugin: herdr.Plugin{Data: os.Getenv("CLAUDE_PLUGIN_DATA"), Version: identity.Version, Heavy: binary},
	}, nil
}
