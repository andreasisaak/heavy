package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/sampler"
)

// fakeClock moves only when a test advances it; every advance hands the daemon one tick.
type fakeClock struct {
	mutex sync.Mutex
	now   time.Time
	ticks chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 7, 20, 0, 0, 0, time.Local), ticks: make(chan time.Time)}
}

func (c *fakeClock) Now() time.Time {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.now
}

func (c *fakeClock) Ticks() <-chan time.Time { return c.ticks }

// advance moves the clock and waits until the daemon took the tick; a second tick at the same time makes sure
// it finished with the first and still runs.
func (c *fakeClock) advance(t *testing.T, step time.Duration) {
	t.Helper()
	now := c.advanceOnce(t, step)
	c.tick(t, now)
}

// advanceOnce moves the clock and hands the daemon one tick, after which it may end.
func (c *fakeClock) advanceOnce(t *testing.T, step time.Duration) time.Time {
	t.Helper()
	c.mutex.Lock()
	c.now = c.now.Add(step)
	now := c.now
	c.mutex.Unlock()
	c.tick(t, now)
	return now
}

func (c *fakeClock) tick(t *testing.T, now time.Time) {
	t.Helper()
	select {
	case c.ticks <- now:
	case <-time.After(5 * time.Second):
		t.Fatalf("the daemon took no tick at %s", now)
	}
}

// running is a daemon of this test process.
type running struct {
	state  string
	clock  *fakeClock
	cancel context.CancelFunc
	done   chan error
}

func startDaemon(t *testing.T, identity protocol.Identity) *running {
	t.Helper()
	return startDaemonWith(t, identity, func(*daemon.Options) {})
}

// ended waits for Run to return and gives its error.
func (r *running) ended(t *testing.T) error {
	t.Helper()
	select {
	case err := <-r.done:
		r.done <- err
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("the daemon did not end")
		return nil
	}
}

func waitFor(t *testing.T, condition func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// greet dials the daemon, sends a hello and returns the daemon's answer and the open connection.
func greet(t *testing.T, state string, hello string) (protocol.Hello, *bufio.Reader, net.Conn) {
	t.Helper()
	connection, err := net.Dial("unix", filepath.Join(state, "daemon.sock"))
	if err != nil {
		t.Fatalf("dialing the daemon: %v", err)
	}
	t.Cleanup(func() { connection.Close() })
	if _, err := connection.Write([]byte(hello + "\n")); err != nil {
		t.Fatalf("sending the hello: %v", err)
	}
	reader := bufio.NewReader(connection)
	answer, err := protocol.ReadHello(reader)
	if err != nil {
		t.Fatalf("reading the answer: %v", err)
	}
	return answer, reader, connection
}

// logRecords returns the records of daemon.log without their time.
func logRecords(t *testing.T, state string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(state, "daemon.log"))
	if err != nil {
		t.Fatalf("reading daemon.log: %v", err)
	}
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		record := map[string]any{}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("daemon.log line %q is no JSON: %v", line, err)
		}
		delete(record, "time")
		records = append(records, record)
	}
	return records
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// options are the daemon options of a test: a fake host and no personal list.
func options(state string, identity protocol.Identity, clock daemon.Clock) daemon.Options {
	return daemon.Options{
		StateDirectory: state, Identity: identity, Clock: clock, IdleTimeout: 10 * time.Minute, HelloTimeout: 200 * time.Millisecond,
		ReadHost: fakeHost, PersonalList: filepath.Join(state, "no-config.json"), WithoutVM: "no VM in this test",
		MachineConfig:  filepath.Join(state, "machine.json"),
		InstalledStamp: func() (string, error) { return identity.Stamp, nil },
	}
}

// fakeHost is a host with room for every run.
func fakeHost() (sampler.HostSample, error) {
	return sampler.HostSample{Time: time.Now(), FreePercent: 50, MemoryBytes: 48 << 30, Cores: 18, PageSize: 16384}, nil
}
