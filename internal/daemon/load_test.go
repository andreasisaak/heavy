package daemon_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/andreasisaak/heavy/internal/platform"
	"github.com/andreasisaak/heavy/internal/testkit"
)

const (
	plenty   = 8 << 20 // kilobytes of available VM memory: 8 GiB
	pressure = 1 << 20 // 1 GiB, below vm_pressure_bytes (1.5 GiB)
	recovery = 3 << 20 // 3 GiB, above vm_recovered_bytes (2.5 GiB)
)

const startedLine = `heavy: started at \d{2}:\d{2}:\d{2} with \d+ cores \(HEAVY_CPUS\), running: `

// plan 3.3: memory pressure for 10 s with two runs pauses the younger, whose command stands (ps state T); 30 s of
// recovery resume it; its history record holds the measured peak and the paused seconds of the injected clock
func TestMemoryPressurePausesTheYoungerRunUntilMemoryRecovers(t *testing.T) {
	scene := startLoadDaemon(t, plenty)
	first, second := repository(t), repository(t)
	scene.remember(t, "sleep", first, second)
	older := scene.start(t, first, "sleep", "300.31")
	scene.step(t, 3, "start of the older run", func() bool { return older.has(startedLine + `sleep 300.31\n`) })
	younger := scene.start(t, second, "sleep", "300.32")
	scene.step(t, 3, "start of the younger run", func() bool { return younger.has(startedLine + `sleep 300.32\n`) })

	scene.setAvailable(t, pressure)
	scene.step(t, 8, "pause line", func() bool {
		return younger.has(`heavy: paused at \d{2}:\d{2}:\d{2} \(` + platform.Docker + ` 1\.0 GiB free, host 50 % free\)\n`)
	})
	pausedAt := scene.clock.Now()
	if got := scene.processState(t, younger); got != "T" {
		t.Fatalf("the paused command is in state %s, want T", got)
	}
	if got := scene.processState(t, older); got == "T" || older.has(`paused`) {
		t.Fatalf("the older run is in state %s with output %q, want it running", got, older.output())
	}

	scene.setAvailable(t, recovery)
	scene.step(t, 20, "resume line", func() bool { return younger.has(`heavy: resumed at \d{2}:\d{2}:\d{2}\n`) })
	resumedAt := scene.clock.Now()
	if got := scene.processState(t, younger); got == "T" {
		t.Fatalf("the resumed command is still in state T")
	}

	for _, pid := range scene.commandPIDs(younger) {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
	}
	if status := younger.wait(t); status != 143 {
		t.Fatalf("the younger client ended with %d, want 143", status)
	}
	var record struct {
		Label         string `json:"label"`
		Command       string `json:"command"`
		PeakRSSBytes  int64  `json:"peak_rss_bytes"`
		PausedSeconds int64  `json:"paused_seconds"`
	}
	scene.step(t, 2, "history record of the younger run", func() bool { return strings.Contains(scene.historyLines(t), `"sleep 300.32"`) })
	for _, line := range strings.Split(scene.historyLines(t), "\n") {
		if strings.Contains(line, `"sleep 300.32"`) {
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				t.Fatal(err)
			}
		}
	}
	if record.PeakRSSBytes <= 0 || record.PausedSeconds != int64(resumedAt.Sub(pausedAt).Seconds()) {
		t.Fatalf("history %+v, want a peak above 0 and %s paused", record, resumedAt.Sub(pausedAt))
	}
}

// plan 3.3: a run with restart: true under pressure that lasts 60 s after its pause ends and waits as #1; once memory
// recovers it starts a second time, and the client ends with the status of that second run
func TestLastingPressureRestartsARunWithRestart(t *testing.T) {
	scene := startLoadDaemon(t, plenty)
	programs := t.TempDir()
	testkit.FakeProgram(t, programs, "attempt", `count=$(cat attempts 2>/dev/null || echo 0); count=$((count + 1)); echo $count > attempts
[ $count -ge 2 ] && exit 7
exec sleep 300.34`)
	t.Setenv("PATH", programs+":"+os.Getenv("PATH"))
	first, second := repository(t), repository(t)
	scene.allow(t, second, `{"command": "attempt", "restart": true}`)
	scene.remember(t, "sleep", first)
	scene.remember(t, "attempt", second)
	older := scene.start(t, first, "sleep", "300.33")
	scene.step(t, 3, "start of the older run", func() bool { return older.has(startedLine + `sleep 300.33\n`) })
	younger := scene.start(t, second, "attempt")
	scene.step(t, 3, "start of the younger run", func() bool { return younger.has(startedLine + `attempt\n`) })

	scene.setAvailable(t, pressure)
	scene.step(t, 8, "pause line", func() bool { return younger.has(`heavy: paused at `) })
	scene.step(t, 35, "restart line", func() bool {
		return younger.has(`heavy: restarting at \d{2}:\d{2}:\d{2} \(memory\), back to #1\n`)
	})
	scene.setAvailable(t, recovery)
	scene.step(t, 25, "second start", func() bool { return strings.Count(younger.output(), "heavy: started at") == 2 })

	if status := younger.wait(t); status != 7 {
		t.Fatalf("the restarted client ended with %d, want 7 of its second run; stdout %q", status, younger.output())
	}
	if !strings.Contains(scene.historyLines(t), `"reason":"restarted"`) {
		t.Fatalf("history %s holds no restarted record", scene.historyLines(t))
	}
}

// critic 3.3: a user stop while a run restarts ends the run for good: its command does not start again, the client
// ends with exit 1 and the reason, and the history counts the run as stopped
func TestStopDuringARestartEndsTheRun(t *testing.T) {
	scene := startLoadDaemon(t, plenty)
	programs := t.TempDir()
	testkit.FakeProgram(t, programs, "stubborn", `echo attempt >> attempts
trap '' TERM
sleep 300.35 &
wait`)
	t.Setenv("PATH", programs+":"+os.Getenv("PATH"))
	first, second := repository(t), repository(t)
	scene.allow(t, second, `{"command": "stubborn", "restart": true}`)
	scene.remember(t, "sleep", first)
	scene.remember(t, "stubborn", second)
	older := scene.start(t, first, "sleep", "300.36")
	scene.step(t, 3, "start of the older run", func() bool { return older.has(startedLine + `sleep 300.36\n`) })
	younger := scene.start(t, second, "stubborn")
	scene.step(t, 3, "start of the younger run", func() bool { return younger.has(startedLine + `stubborn\n`) })
	scene.setAvailable(t, pressure)
	scene.step(t, 8, "pause line", func() bool { return younger.has(`heavy: paused at `) })
	scene.step(t, 35, "restart line", func() bool { return younger.has(`heavy: restarting at `) })
	var id string
	for _, entry := range status(t, scene.state) {
		if entry.Label == "stubborn" {
			id = entry.ID
		}
	}

	stop := exec.Command(scene.binary, "--stop", id)
	output, err := stop.CombinedOutput()

	if err != nil || string(output) != "heavy: stopped run "+id+" (stubborn)\n" {
		t.Fatalf("--stop = %q, %v", output, err)
	}
	if status := younger.wait(t); status != 1 || !strings.Contains(younger.stderr(), "heavy: stopped by the user during its restart\n") {
		t.Fatalf("the client ended with %d, stderr %q; want 1 and the reason", status, younger.stderr())
	}
	attempts, _ := os.ReadFile(filepath.Join(second, "attempts"))
	history := scene.historyLines(t)
	if string(attempts) != "attempt\n" || !strings.Contains(history, `"label":"stubborn"`) || !strings.Contains(history, `"reason":"stopped"`) {
		t.Fatalf("attempts %q, history %s; want one attempt and a stopped record", attempts, history)
	}
}

// plan 3.3, probe 2026-11-08 (71 containers, docker inspect up to 1.2 s): one sample lists the containers once for
// all active runs, not once per run
func TestOneSampleListsTheContainersOnceForAllRuns(t *testing.T) {
	scene := startLoadDaemon(t, plenty)
	first, second := repository(t), repository(t)
	scene.remember(t, "sleep", first, second)
	older := scene.start(t, first, "sleep", "300.37")
	scene.step(t, 3, "start of the older run", func() bool { return older.has(startedLine + `sleep 300.37\n`) })
	younger := scene.start(t, second, "sleep", "300.38")
	scene.step(t, 3, "start of the younger run", func() bool { return younger.has(startedLine + `sleep 300.38\n`) })
	scene.step(t, 1, "a sample", func() bool { return true })
	before := scene.dockerCalls(t, "ps")

	scene.step(t, 1, "a sample", func() bool { return true })
	scene.step(t, 1, "a sample", func() bool { return true })
	scene.step(t, 1, "a sample", func() bool { return true })

	if got := scene.dockerCalls(t, "ps") - before; got != 3 {
		t.Fatalf("three samples with two runs listed the containers %d times, want 3", got)
	}
}
