package sampler_test

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/sampler"
	"github.com/andreasisaak/heavy/internal/testkit"
)

// fakeDocker is a docker program that logs its calls and runs the real heavy --helper against a fixture /proc.
type fakeDocker struct {
	path  string
	log   string
	state string
	linux string
}

// dockerBehaviour is the shell code the fake runs for docker version, build and run; {helper} in run stands for
// heavy --helper against the fixture /proc.
type dockerBehaviour struct {
	version string
	build   string
	run     string
}

func newFakeDocker(t *testing.T, behaviour dockerBehaviour) *fakeDocker {
	t.Helper()
	folder := t.TempDir()
	fake := &fakeDocker{log: filepath.Join(folder, "calls.log"), state: t.TempDir(), linux: filepath.Join(folder, "heavy-linux")}
	if err := os.WriteFile(fake.linux, []byte("a linux build of heavy\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := procFixture(t)
	if behaviour.version == "" {
		behaviour.version = "echo 29.8.2"
	}
	if behaviour.build == "" {
		behaviour.build = `for last; do :; done
  cat > ` + fake.state + `/dockerfile
  cmp -s ` + fake.linux + ` "$last/heavy-linux" || { echo "the context lacks heavy-linux" >&2; exit 1; }
  touch ` + fake.state + `/image`
	}
	if behaviour.run == "" {
		behaviour.run = "exec {helper}"
	}
	behaviour.run = strings.ReplaceAll(behaviour.run, "{helper}", testkit.BuildBinary(t)+" --helper --root "+root)
	script := `printf '%s\n' "$*" >> ` + fake.log + `
case "$1" in
version) ` + behaviour.version + ` ;;
image) test -f ` + fake.state + `/image ;;
build) ` + behaviour.build + ` ;;
rm) ;;
run) ` + behaviour.run + ` ;;
esac`
	fake.path = testkit.FakeProgram(t, folder, "docker", script)
	// macOS checks a new executable on its first start, which can take longer than the short timeouts of the tests.
	if output, err := exec.Command(fake.path, "warm-up").CombinedOutput(); err != nil {
		t.Fatalf("warming up %s: %v\n%s", fake.path, err, output)
	}
	if err := os.Remove(fake.log); err != nil {
		t.Fatal(err)
	}
	return fake
}

// calls are the docker calls so far, the context folder of a build replaced by <context>.
func (f *fakeDocker) calls(t *testing.T) []string {
	t.Helper()
	content, err := os.ReadFile(f.log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, "build ") {
			words := strings.Fields(line)
			lines[index] = strings.Join(append(words[:len(words)-1], "<context>"), " ")
		}
	}
	return lines
}

func (f *fakeDocker) haveImage(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.state, "image"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

// procFixture is a /proc of a VM with 6 GiB available, 25 swapped-out pages, load 2.5 and 8 cores.
func procFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"meminfo": "MemTotal:       25165824 kB\nMemAvailable:    6291456 kB\n",
		"vmstat":  "pswpin 3\npswpout 25\n",
		"loadavg": "2.50 2.00 1.00 3/400 777\n",
		"cpuinfo": strings.Repeat("processor\t: 0\n\n", 8),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// vmClock is a clock the test moves by hand.
type vmClock struct {
	mutex sync.Mutex
	now   time.Time
}

func (c *vmClock) Now() time.Time {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.now
}

func (c *vmClock) advance(step time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.now = c.now.Add(step)
}

func newVM(t *testing.T, fake *fakeDocker, clock *vmClock, timeout time.Duration) *sampler.VM {
	t.Helper()
	vm := sampler.NewVM(sampler.VMOptions{
		Docker: fake.path, LinuxBinary: fake.linux, Image: "heavy-probe-helper:1", Container: "heavy-probe-helper-1",
		Timeout: timeout, Now: clock.Now, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	t.Cleanup(func() { vm.Close() })
	return vm
}

// started are the calls of a start with the image in place.
var started = []string{
	"version --format {{.Server.Version}}",
	"image inspect --format {{.Id}} heavy-probe-helper:1",
	"rm -f heavy-probe-helper-1",
	"run --rm -i --pid=host --cap-add=SYS_PTRACE --cap-add=KILL --name heavy-probe-helper-1 heavy-probe-helper:1",
}
