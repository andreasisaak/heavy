package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/andreasisaak/heavy/internal/client"
	"github.com/andreasisaak/heavy/internal/herdr"
)

// runHerdrRefresh is `heavy --herdr-refresh`, the startup hook, the event hook pane.agent_detected and the action
// refresh of the herdr plugin. It reads the queue of a running daemon and starts none: without one no run shows.
func runHerdrRefresh(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) > 0 {
		fmt.Fprintf(stderr, "heavy: --herdr-refresh takes no arguments, got %s. %s\n", arguments[0], usage)
		return exitUsage
	}
	connect, err := connectOptions()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	err = herdr.Refresh(herdr.RefreshDependencies{
		Herdr: herdrBinary(), Runs: runningQueue, Now: time.Now,
		Lock: filepath.Join(connect.StateDirectory, "herdr-refresh.lock"), Event: os.Getenv("HERDR_PLUGIN_EVENT"),
	}, stdout)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// runHerdrForward is `heavy --herdr-forward`, the action forward of the herdr plugin.
func runHerdrForward(arguments []string, _, stderr io.Writer) int {
	if len(arguments) > 0 {
		fmt.Fprintf(stderr, "heavy: --herdr-forward takes no arguments, got %s. %s\n", arguments[0], usage)
		return exitUsage
	}
	connect, err := connectOptions()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	err = herdr.Forward(herdr.ForwardDependencies{
		Herdr: herdrBinary(), Pane: os.Getenv("HERDR_PANE_ID"), Runs: runningQueue,
		Forward: func(position int) error {
			_, err := client.Forward(connect, position)
			return err
		},
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// herdrBinary is the herdr that runs the plugin: HERDR_BIN_PATH, which herdr sets for its plugin commands, else
// herdr on PATH, as 0.6 took it.
func herdrBinary() string {
	if path := os.Getenv("HERDR_BIN_PATH"); path != "" {
		return path
	}
	return "herdr"
}
