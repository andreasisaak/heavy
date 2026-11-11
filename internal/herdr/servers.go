package herdr

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Servers are the sockets of the herdr servers that run, in the herdr config directory: the default server's
// herdr.sock and one sessions/<name>/herdr.sock per named session (`herdr --session <name>`). A socket whose
// server ended takes no connection and is left out.
func Servers(configDirectory string) ([]string, error) {
	candidates := []string{filepath.Join(configDirectory, "herdr.sock")}
	sessions := filepath.Join(configDirectory, "sessions")
	entries, err := os.ReadDir(sessions)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("heavy: listing the herdr sessions in %s: %w", sessions, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			candidates = append(candidates, filepath.Join(sessions, entry.Name(), "herdr.sock"))
		}
	}
	var running []string
	for _, socket := range candidates {
		connection, err := net.DialTimeout("unix", socket, time.Second)
		if err != nil {
			continue
		}
		connection.Close()
		running = append(running, socket)
	}
	return running, nil
}

// RedrawAll runs the plugin action refresh in each of the servers, so the panes of every herdr show the queue; a
// server whose refresh fails does not keep the others from theirs.
func RedrawAll(herdr string, servers []string, timeout time.Duration) error {
	var failures []error
	for _, socket := range servers {
		if err := redraw(herdr, socket, timeout); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func redraw(herdr, socket string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, herdr, "plugin", "action", "invoke", "refresh", "--plugin", "local.heavy-queue")
	command.Env = append(os.Environ(), "HERDR_SOCKET_PATH="+socket)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("heavy: %s plugin action invoke refresh on %s: %w: %s", herdr, socket, err, strings.TrimSpace(string(output)))
	}
	return nil
}
