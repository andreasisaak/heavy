package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/herdr"
	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/sampler"
	"github.com/andreasisaak/heavy/internal/slots"
)

const (
	// idleTimeout is how long the daemon stays without runs and subscribers (design G3).
	idleTimeout = 10 * time.Minute
	// helloTimeout is how long a new connection may take to send its hello.
	helloTimeout = 5 * time.Second
	// vmTimeout bounds each request to the helper in the VM and each short docker call.
	vmTimeout = 5 * time.Second
)

// runDaemon serves the queue until the daemon idles, hands over or gets SIGTERM or SIGINT. With --detach it starts the
// daemon in a session of its own and ends at once, the step daemon.Connect starts it through.
func runDaemon(arguments []string, stderr io.Writer) int {
	if len(arguments) == 1 && arguments[0] == "--detach" {
		return detachDaemon(stderr)
	}
	if len(arguments) != 0 {
		fmt.Fprintf(stderr, "heavy: --daemon takes no argument but --detach, got %q. %s\n", strings.Join(arguments, " "), usage)
		return exitUsage
	}
	identity, err := ownIdentity()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	state, err := daemon.StateDirectory()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	personalList, err := lists.PersonalPath()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	binary, err := ownBinary()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	options := daemon.Options{
		StateDirectory: state, Identity: identity, Clock: daemon.SystemClock(), IdleTimeout: idleTimeout, HelloTimeout: helloTimeout,
		ReadHost: sampler.ReadHost, PersonalList: personalList, InstalledStamp: installedStamp(binary), RedrawHerdr: herdrRedraw(state),
		MachineConfig: slots.ConfigPath(),
	}
	measureDocker(&options, binary, identity.Stamp)
	if err := daemon.Run(ctx, options); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// herdrRedraw runs the plugin action refresh in every running herdr server, the default one and each
// `herdr --session <name>`, within 5 s each; nil without herdr on PATH, and for a daemon of another state directory
// than ~/.cache/heavy: the herdr plugin reads the queue of that one, and tests and probes, which use their own, never
// reach the real herdr (V34).
func herdrRedraw(state string) func() error {
	home, err := os.UserHomeDir()
	if err != nil || state != filepath.Join(home, ".cache", "heavy") {
		return nil
	}
	program, err := exec.LookPath("herdr")
	if err != nil {
		return nil
	}
	return func() error {
		servers, err := herdr.Servers(filepath.Join(home, ".config", "herdr"))
		if err != nil {
			return err
		}
		return herdr.RedrawAll(program, servers, 5*time.Second)
	}
}

// installedStamp reads the stamp of the build next to the binary, which a rebuild replaces while the daemon runs.
func installedStamp(binary string) func() (string, error) {
	return func() (string, error) {
		identity, err := protocol.LocalIdentity(binary)
		return identity.Stamp, err
	}
}

// ownBinary is the path of this binary, through the link a shim may have started it by.
func ownBinary() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("heavy: finding the heavy binary: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", fmt.Errorf("heavy: resolving %s: %w", executable, err)
	}
	return resolved, nil
}

// ownIdentity is the identity of this binary.
func ownIdentity() (protocol.Identity, error) {
	binary, err := ownBinary()
	if err != nil {
		return protocol.Identity{}, err
	}
	return protocol.LocalIdentity(binary)
}

// detachDaemon is `heavy --daemon --detach`.
func detachDaemon(stderr io.Writer) int {
	binary, err := ownBinary()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := daemon.Detach(binary); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
