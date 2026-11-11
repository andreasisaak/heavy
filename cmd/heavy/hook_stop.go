package main

import (
	"fmt"
	"io"
	"time"

	"github.com/andreasisaak/heavy/internal/client"
	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/stophook"
)

// exitBlock is the exit status with which a PreToolUse hook blocks the tool call; Claude Code shows stderr as
// the reason.
const exitBlock = 2

// runStopHook is `heavy --hook stop`, the PreToolUse hook for TaskStop and KillShell. A denial it cannot write
// to stdout still blocks the stop, through exitBlock (design G15).
func runStopHook(stdin io.Reader, stdout, stderr io.Writer) int {
	state, err := daemon.StateDirectory()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitBlock
	}
	deps := stophook.Dependencies{StateDirectory: state, Queue: runningQueue, Now: time.Now}
	if err := stophook.Run(stdin, stdout, deps); err != nil {
		fmt.Fprintln(stderr, err)
		return exitBlock
	}
	return 0
}

// runningQueue is the queue of the running daemon; it starts none, and without one no run waits or runs.
func runningQueue() ([]protocol.RunStatus, error) {
	connect, err := connectOptions()
	if err != nil {
		return nil, err
	}
	status, _, err := client.StatusIfRunning(connect)
	return status.Runs, err
}
