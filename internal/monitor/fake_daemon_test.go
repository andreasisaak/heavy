package monitor_test

import (
	"bufio"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// patience bounds every wait of a test for the monitor.
const patience = 5 * time.Second

// fakeDaemon listens on $STATE/daemon.sock like the daemon: it takes the hello of each connection, answers with
// its own, reads the first message and hands the connection to the test. A hello that refuses the connection
// ends it at once, as the daemon does.
type fakeDaemon struct {
	listener net.Listener
	hello    protocol.Hello
	links    chan *fakeLink
	mutex    sync.Mutex
	open     []net.Conn
}

// fakeLink is one connection of the monitor to the fake daemon, with the first message it sent.
type fakeLink struct {
	connection net.Conn
	first      protocol.Message
	err        error
}

func startFakeDaemon(t *testing.T, state string) *fakeDaemon {
	t.Helper()
	return startFakeDaemonWith(t, state, protocol.Hello{Protocol: protocol.Version})
}

// startFakeDaemonWith starts a fake daemon that answers every hello with the given one.
func startFakeDaemonWith(t *testing.T, state string, hello protocol.Hello) *fakeDaemon {
	t.Helper()
	listener, err := net.Listen("unix", filepath.Join(state, "daemon.sock"))
	if err != nil {
		t.Fatalf("listening as the fake daemon: %v", err)
	}
	fake := &fakeDaemon{listener: listener, hello: hello, links: make(chan *fakeLink, 8)}
	go fake.serve()
	t.Cleanup(fake.close)
	return fake
}

func (f *fakeDaemon) serve() {
	for {
		connection, err := f.listener.Accept()
		if err != nil {
			return
		}
		f.mutex.Lock()
		f.open = append(f.open, connection)
		f.mutex.Unlock()
		reader := bufio.NewReader(connection)
		link := &fakeLink{connection: connection}
		if _, link.err = protocol.ReadHello(reader); link.err == nil {
			if link.err = protocol.Write(connection, f.hello); link.err == nil {
				if f.hello.Error != "" || f.hello.Protocol != protocol.Version {
					connection.Close()
					continue
				}
				link.first, link.err = protocol.ReadMessage(reader)
			}
		}
		f.links <- link
	}
}

// close ends the fake daemon: no more connections, and the open ones end.
func (f *fakeDaemon) close() {
	f.listener.Close()
	f.mutex.Lock()
	defer f.mutex.Unlock()
	for _, connection := range f.open {
		connection.Close()
	}
}

// connections is the number of connections the fake daemon took.
func (f *fakeDaemon) connections() int {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return len(f.open)
}

// next waits for the next connection of the monitor and returns it with its first message.
func (f *fakeDaemon) next(t *testing.T) *fakeLink {
	t.Helper()
	select {
	case link := <-f.links:
		if link.err != nil {
			t.Fatalf("the fake daemon greeting the monitor: %v", link.err)
		}
		return link
	case <-time.After(patience):
		t.Fatalf("the monitor did not subscribe within %s", patience)
		return nil
	}
}

// subscribed waits for the next subscription, checks it asks for want and answers that it holds.
func (f *fakeDaemon) subscribed(t *testing.T, want protocol.Subscribe) *fakeLink {
	t.Helper()
	link := f.next(t)
	if link.first.Type != protocol.TypeSubscribe || link.first.Subscribe == nil || *link.first.Subscribe != want {
		t.Fatalf("the monitor sent %+v, want a subscription %+v", link.first, want)
	}
	link.send(t, protocol.Message{Type: protocol.TypeSubscribe, Subscribe: &want})
	return link
}

func (l *fakeLink) send(t *testing.T, message protocol.Message) {
	t.Helper()
	if err := protocol.Write(l.connection, message); err != nil {
		t.Fatalf("the fake daemon sending %+v: %v", message, err)
	}
}

func (l *fakeLink) event(t *testing.T, event protocol.Event) {
	t.Helper()
	l.send(t, protocol.Message{Type: protocol.TypeEvent, Event: &event})
}
