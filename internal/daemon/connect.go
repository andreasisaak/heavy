package daemon

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/andreasisaak/heavy/internal/protocol"
)

const (
	// connectTimeout is how long Connect waits for a daemon to answer (FR-12).
	connectTimeout = 2 * time.Second
	// restartPause keeps Connect from starting daemons faster than one can take the lock.
	restartPause = 200 * time.Millisecond
)

// ConnectOptions name the state directory, the heavy binary that starts a daemon, and this heavy's identity.
type ConnectOptions struct {
	StateDirectory string
	Binary         string
	Identity       protocol.Identity
}

// Connection is an open connection to the daemon, after both hellos.
type Connection struct {
	Daemon     protocol.Hello
	connection net.Conn
	reader     *bufio.Reader
}

// SetDeadline ends a Send or Receive that is not done by the deadline with an error wrapping os.ErrDeadlineExceeded;
// the zero time clears it.
func (c *Connection) SetDeadline(deadline time.Time) error {
	return c.connection.SetDeadline(deadline)
}

// Close ends the connection.
func (c *Connection) Close() error {
	return c.connection.Close()
}

// Send writes a message to the daemon.
func (c *Connection) Send(message protocol.Message) error {
	return protocol.Write(c.connection, message)
}

// Receive reads the next message of the daemon; io.EOF when the daemon closed the connection.
func (c *Connection) Receive() (protocol.Message, error) {
	return protocol.ReadMessage(c.reader)
}

// Connect reaches the daemon, starting `<binary> --daemon` detached when none answers, and waits up to 2 s. A
// daemon that hands over to this newer heavy ends, and Connect starts this heavy's daemon in its place.
func Connect(options ConnectOptions) (*Connection, error) {
	socket, err := socketPath(options.StateDirectory)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(options.StateDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("heavy: creating the state directory %s: %w", options.StateDirectory, err)
	}
	lock := filepath.Join(options.StateDirectory, "daemon.lock")
	deadline := time.Now().Add(connectTimeout)
	var lastStart time.Time
	for {
		if raw, err := net.Dial("unix", socket); err == nil {
			connection, err := greetDaemon(raw, options.Identity, false)
			if err != nil {
				raw.Close()
				return nil, err
			}
			if !connection.Daemon.Handover {
				return connection, nil
			}
			raw.Close()
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("heavy: no daemon answered on %s within %s; see %s", socket, connectTimeout, filepath.Join(options.StateDirectory, "daemon.log"))
		}
		free, err := lockFree(lock)
		if err != nil {
			return nil, err
		}
		if free && time.Since(lastStart) >= restartPause {
			if err := startDaemon(options); err != nil {
				return nil, err
			}
			lastStart = time.Now()
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Reach dials a running daemon once and never starts one, for hooks, shims, the herdr refresh and the dashboard,
// which only ask; found is false when none answers. It greets as a reader, so it never replaces an older daemon.
func Reach(options ConnectOptions) (connection *Connection, found bool, err error) {
	socket, err := socketPath(options.StateDirectory)
	if err != nil {
		return nil, false, err
	}
	raw, err := net.Dial("unix", socket)
	if err != nil {
		return nil, false, nil
	}
	connection, err = greetDaemon(raw, options.Identity, true)
	if err != nil {
		raw.Close()
		return nil, false, err
	}
	return connection, true, nil
}

func greetDaemon(raw net.Conn, identity protocol.Identity, onlyReads bool) (*Connection, error) {
	if err := raw.SetDeadline(time.Now().Add(greetTimeout)); err != nil {
		return nil, fmt.Errorf("heavy: setting the deadline of the hello: %w", err)
	}
	hello := protocol.Hello{Protocol: protocol.Version, Version: identity.Version, Stamp: identity.Stamp, Reader: onlyReads}
	if err := protocol.Write(raw, hello); err != nil {
		return nil, fmt.Errorf("heavy: greeting the daemon: %w", err)
	}
	reader := bufio.NewReader(raw)
	answer, err := protocol.ReadHello(reader)
	if err != nil {
		return nil, fmt.Errorf("heavy: reading the daemon's hello: %w", err)
	}
	if answer.Error != "" {
		return nil, &RefusedError{Text: answer.Error}
	}
	if answer.Protocol != protocol.Version {
		return nil, &RefusedError{Text: fmt.Sprintf("heavy: the running daemon speaks protocol %d, this heavy %d; restart the session", answer.Protocol, protocol.Version)}
	}
	if err := raw.SetDeadline(time.Time{}); err != nil {
		return nil, fmt.Errorf("heavy: clearing the deadline of the hello: %w", err)
	}
	return &Connection{Daemon: answer, connection: raw, reader: reader}, nil
}

// greetTimeout bounds the hellos, so a daemon that accepts and says nothing fails the call instead of freezing it.
const greetTimeout = 5 * time.Second

// RefusedError is a daemon that refused this heavy in its hello, such as one of another protocol; asking again gets
// the same answer, so a monitor ends instead of retrying.
type RefusedError struct {
	Text string
}

func (e *RefusedError) Error() string { return e.Text }

// startDaemon starts the daemon through `heavy --daemon --detach`, which starts it in a session of its own and ends at
// once: the daemon then hangs on launchd and outlives the heavy that started it. Claude Code ends a stopped task, a
// monitor or a session together with every process below it by parent pid (heavy-37, 2026-11-08).
func startDaemon(options ConnectOptions) error {
	command := exec.Command(options.Binary, "--daemon", "--detach")
	command.Env = append(os.Environ(), "HEAVY_STATE_DIRECTORY="+options.StateDirectory)
	output, err := command.CombinedOutput()
	if err == nil {
		return nil
	}
	if said := strings.TrimSpace(string(output)); said != "" {
		return fmt.Errorf("heavy: starting the daemon through %s --daemon --detach failed: %w: %s", options.Binary, err, said)
	}
	return fmt.Errorf("heavy: starting the daemon through %s --daemon --detach failed: %w", options.Binary, err)
}

// Detach starts `binary --daemon` in a session of its own and returns without waiting for it, so the daemon is no
// child of the heavy that called startDaemon.
func Detach(binary string) error {
	command := exec.Command(binary, "--daemon")
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return fmt.Errorf("heavy: starting the daemon %s --daemon failed: %w", binary, err)
	}
	return command.Process.Release()
}
