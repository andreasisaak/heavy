package daemon_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/sampler"
	"github.com/andreasisaak/heavy/internal/testkit"
)

// loadScene is a daemon of this test whose VM is a fixture /proc read by the real heavy --helper behind a fake
// docker, with clients of the real heavy binary.
type loadScene struct {
	*running
	proc          string
	binary        string
	configuration string
	dockerLog     string
}

// dockerCalls counts the docker calls of the given kind, such as ps.
func (s *loadScene) dockerCalls(t *testing.T, kind string) int {
	t.Helper()
	content, err := os.ReadFile(s.dockerLog)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(content), kind+"\n")
}

// neutralLoad are the thresholds of the scene: the host never limits (as in 2.4), the VM has those of design G5.
const neutralLoad = `"load": {"host_pressure_percent": 0, "host_recovered_percent": 0, "reserve_bytes": 0, "load_per_core": 1000, "swap_bytes_per_2s": 1000000000000}`

// startLoadDaemon starts the daemon with the thresholds of neutralLoad and a VM with the given available memory.
func startLoadDaemon(t *testing.T, availableKilobytes int64) *loadScene {
	t.Helper()
	return startLoadDaemonWithDocker(t, availableKilobytes, "")
}

// startLoadDaemonWithDocker is startLoadDaemon with more cases for the fake docker, such as one for ps.
func startLoadDaemonWithDocker(t *testing.T, availableKilobytes int64, cases string) *loadScene {
	t.Helper()
	binary := testkit.BuildBinary(t)
	home := testkit.Home(t)
	configuration := filepath.Join(home, ".config", "heavy", "config.json")
	write(t, configuration, `{`+neutralLoad+`}`)
	scene := &loadScene{proc: t.TempDir(), binary: binary, configuration: configuration}
	write(t, filepath.Join(scene.proc, "vmstat"), "pswpout 0\n")
	write(t, filepath.Join(scene.proc, "loadavg"), "0.50 0.50 0.50 1/100 99\n")
	write(t, filepath.Join(scene.proc, "cpuinfo"), strings.Repeat("processor\t: 0\n\n", 8))
	scene.setAvailable(t, availableKilobytes)
	folder := t.TempDir()
	linux := filepath.Join(folder, "heavy-linux")
	write(t, linux, "a linux build\n")
	scene.dockerLog = filepath.Join(folder, "calls.log")
	docker := testkit.FakeProgram(t, folder, "docker", `printf '%s\n' "$1" >> `+scene.dockerLog+`
case "$1" in
version) echo 29.8.2 ;;
run) exec `+binary+` --helper --root `+scene.proc+` ;;
`+cases+`
esac`)
	if output, err := exec.Command(docker, "warm-up").CombinedOutput(); err != nil {
		t.Fatalf("warming up the fake docker: %v\n%s", err, output)
	}
	scene.running = startDaemonWith(t, protocol.Identity{}, func(options *daemon.Options) {
		options.PersonalList = configuration
		options.VM = &sampler.VMOptions{Docker: docker, LinuxBinary: linux, Image: "heavy-probe-helper:1", Container: "heavy-probe-helper-1", Timeout: 5 * time.Second}
	})
	return scene
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// allow lists the commands for the project of the main checkout in the personal list.
func (s *loadScene) allow(t *testing.T, checkout, commands string) {
	t.Helper()
	write(t, s.configuration, fmt.Sprintf(`{"projects": {%q: {"enabled": true, "commands": [%s]}}, %s}`, checkout, commands, neutralLoad))
}

// setAvailable writes MemAvailable of the fixture VM.
func (s *loadScene) setAvailable(t *testing.T, kilobytes int64) {
	t.Helper()
	write(t, filepath.Join(s.proc, "meminfo"), fmt.Sprintf("MemTotal:       25165824 kB\nMemAvailable:   %d kB\n", kilobytes))
}

// step moves the clock by 2 s and waits until the daemon sampled, then gives the clients a moment, until the
// condition holds; it fails after the given number of steps.
func (s *loadScene) step(t *testing.T, steps int, what string, condition func() bool) {
	t.Helper()
	for done := 0; done < steps; done++ {
		now := s.clock.advanceOnce(t, 2*time.Second)
		s.clock.tick(t, now)
		s.clock.tick(t, now)
		for wait := 0; wait < 20; wait++ {
			if condition() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	log, _ := os.ReadFile(filepath.Join(s.state, "daemon.log"))
	t.Fatalf("after %d steps of 2 s: no %s; daemon log:\n%s", steps, what, log)
}

// historyRecords reads every record of the history of the scene.
func (s *loadScene) historyLines(t *testing.T) string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(s.state, "history", "*.jsonl"))
	var lines []string
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, strings.TrimSpace(string(content)))
	}
	return strings.Join(lines, "\n")
}

// heavyClient is a heavy client process of the scene.
type heavyClient struct {
	command *exec.Cmd
	mutex   sync.Mutex
	stdout  bytes.Buffer
	errors  lockedBuffer
	done    chan struct{}
	exit    int
}

// lockedBuffer collects stderr of a client.
type lockedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.Write(data)
}

func (c *heavyClient) stderr() string {
	c.errors.mutex.Lock()
	defer c.errors.mutex.Unlock()
	return c.errors.buffer.String()
}

func (c *heavyClient) Write(data []byte) (int, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.stdout.Write(data)
}

func (c *heavyClient) output() string {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.stdout.String()
}

func (c *heavyClient) has(pattern string) bool {
	return regexp.MustCompile(pattern).MatchString(c.output())
}

// repository is a new git repository.
func repository(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("/usr/bin/git", "init", "-q", path).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}
	return path
}

// remember writes a history record with a peak of 1 GiB for the label in each checkout, so runs there overlap.
func (s *loadScene) remember(t *testing.T, label string, checkouts ...string) {
	t.Helper()
	var lines []string
	for index, checkout := range checkouts {
		lines = append(lines, fmt.Sprintf(`{"id":"old%d","label":%q,"command":"x","checkout":%q,"directory":%q,"session":"","agent":"","since":1,"started":2,"ended":3,"exit_status":0,"reason":"passed","peak_rss_bytes":1073741824,"budget":4,"paused_seconds":0,"restarts":0}`, index, label, checkout, checkout))
	}
	file := filepath.Join(s.state, "history", s.clock.Now().Format("2006-01-02")+".jsonl")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	opened, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	if _, err := opened.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		t.Fatal(err)
	}
}

// start runs heavy with the arguments in the repository; the command's group dies with the test.
func (s *loadScene) start(t *testing.T, repository string, arguments ...string) *heavyClient {
	t.Helper()
	client := &heavyClient{command: exec.Command(s.binary, arguments...), done: make(chan struct{})}
	client.command.Dir, client.command.Stdout, client.command.Stderr = repository, client, &client.errors
	client.command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := client.command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		state, _ := client.command.Process.Wait()
		client.exit = state.ExitCode()
		close(client.done)
	}()
	t.Cleanup(func() {
		for _, pid := range s.commandPIDs(client) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		_ = syscall.Kill(-client.command.Process.Pid, syscall.SIGKILL)
		<-client.done
	})
	return client
}

// commandPIDs are the processes of the client's command: the client's children, which run in their own group.
func (s *loadScene) commandPIDs(client *heavyClient) []int {
	output, _ := exec.Command("pgrep", "-P", strconv.Itoa(client.command.Process.Pid)).Output()
	var pids []int
	for _, field := range strings.Fields(string(output)) {
		if pid, err := strconv.Atoi(field); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

// processState is the ps state letter of the client's command, such as S or T.
func (s *loadScene) processState(t *testing.T, client *heavyClient) string {
	t.Helper()
	pids := s.commandPIDs(client)
	if len(pids) != 1 {
		t.Fatalf("client %d has the children %v, want its command alone", client.command.Process.Pid, pids)
	}
	output, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pids[0])).Output()
	if err != nil {
		t.Fatalf("ps -p %d: %v", pids[0], err)
	}
	return strings.TrimSpace(string(output))[:1]
}

func (c *heavyClient) wait(t *testing.T) int {
	t.Helper()
	select {
	case <-c.done:
		return c.exit
	case <-time.After(20 * time.Second):
		t.Fatalf("heavy %v did not end; stdout %q", c.command.Args, c.output())
		return 0
	}
}

// startDaemonWith starts a daemon of this test with changed options.
func startDaemonWith(t *testing.T, identity protocol.Identity, change func(*daemon.Options)) *running {
	t.Helper()
	state := testkit.StateDir(t)
	clock := newFakeClock()
	ctx, cancel := context.WithCancel(context.Background())
	current := &running{state: state, clock: clock, cancel: cancel, done: make(chan error, 1)}
	configured := options(state, identity, clock)
	change(&configured)
	go func() {
		current.done <- daemon.Run(ctx, configured)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-current.done:
		case <-time.After(10 * time.Second):
			t.Errorf("the daemon did not end after its context")
		}
	})
	waitFor(t, func() bool {
		connection, err := net.Dial("unix", filepath.Join(state, "daemon.sock"))
		if err == nil {
			connection.Close()
		}
		return err == nil
	}, "the daemon to listen")
	return current
}
