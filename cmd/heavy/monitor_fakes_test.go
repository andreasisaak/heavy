package main

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/testkit"
)

// exitWithin waits for a started process and returns its exit status; after the limit the test fails.
func exitWithin(t *testing.T, process *exec.Cmd, limit time.Duration) int {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	select {
	case err := <-done:
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return exitError.ExitCode()
		}
		if err != nil {
			t.Fatal(err)
		}
		return 0
	case <-time.After(limit):
		_ = process.Process.Kill()
		t.Fatalf("%s did not end within %s", process.Path, limit)
		return 0
	}
}

func listenAsDaemon(t *testing.T, state string) net.Listener {
	t.Helper()
	listener, err := net.Listen("unix", filepath.Join(state, "daemon.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	return listener
}

func startMonitor(t *testing.T, arguments ...string) (*exec.Cmd, *lockedBuffer, *lockedBuffer) {
	t.Helper()
	monitor := exec.Command(testkit.BuildBinary(t), append([]string{"--monitor"}, arguments...)...)
	stdout, stderr := &lockedBuffer{}, &lockedBuffer{}
	monitor.Stdout, monitor.Stderr = stdout, stderr
	if err := monitor.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = monitor.Process.Kill() })
	return monitor, stdout, stderr
}

// lockedBuffer takes the output of a running process while the test reads it.
type lockedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.Write(data)
}

func (b *lockedBuffer) String() string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.String()
}

// acceptHello takes the next connection and reads its hello; the deadline keeps a test from waiting for ever.
func acceptHello(t *testing.T, listener net.Listener) (net.Conn, *bufio.Reader) {
	t.Helper()
	if err := listener.(*net.UnixListener).SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	link, err := listener.Accept()
	if err != nil {
		t.Fatalf("the monitor did not connect: %v", err)
	}
	t.Cleanup(func() { link.Close() })
	reader := bufio.NewReader(link)
	if _, err := protocol.ReadHello(reader); err != nil {
		t.Fatal(err)
	}
	return link, reader
}

// acceptSubscription greets the next connection as the daemon and returns it with the subscription it asked for.
func acceptSubscription(t *testing.T, listener net.Listener) (net.Conn, protocol.Subscribe) {
	t.Helper()
	link, reader := acceptHello(t, listener)
	answer(t, link, protocol.Hello{Protocol: protocol.Version})
	message, err := protocol.ReadMessage(reader)
	if err != nil || message.Type != protocol.TypeSubscribe {
		t.Fatalf("the monitor sent %+v, %v; want a subscription", message, err)
	}
	return link, *message.Subscribe
}

func answer[T protocol.Hello | protocol.Message](t *testing.T, link net.Conn, value T) {
	t.Helper()
	if err := protocol.Write(link, value); err != nil {
		t.Fatal(err)
	}
}

// waitForOutput waits until the output of a running process is exactly want.
func waitForOutput(t *testing.T, output *lockedBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for output.String() != want {
		if time.Now().After(deadline) {
			t.Fatalf("stdout = %q after 5s, want %q", output.String(), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
