package daemon_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/slots"
)

// limitMachine writes the machine configuration of the scene with a new slot folder and waits until the daemon
// has read it: a sample runs after the tick that asks for it, so the test waits for the daemon's own word.
func (s *loadScene) limitMachine(t *testing.T, limit int) slots.Config {
	t.Helper()
	config := slots.Config{Path: filepath.Join(s.state, "machine.json"), MaxParallel: limit, Folder: t.TempDir()}
	write(t, config.Path, fmt.Sprintf(`{"max_parallel": %d, "slots": %q}`, limit, config.Folder))
	s.step(t, 5, "machine limit in the daemon log", func() bool { return limitLogged(t, s.state, config) })
	return config
}

// limitLogged reports whether the daemon logged that it took the machine configuration.
func limitLogged(t *testing.T, state string, config slots.Config) bool {
	t.Helper()
	for _, record := range logRecords(t, state) {
		if record["msg"] == "the machine limit changed" && record["slots"] == config.Folder && record["max_parallel"] == float64(config.MaxParallel) {
			return true
		}
	}
	return false
}

// pass lets the daemon take the given number of samples and gives the clients a moment to print.
func (s *loadScene) pass(t *testing.T, samples int) {
	t.Helper()
	for done := 0; done < samples; done++ {
		s.step(t, 1, "a sample", func() bool { return true })
	}
	time.Sleep(100 * time.Millisecond)
}

func acquire(t *testing.T, pool *slots.Pool, config slots.Config) bool {
	t.Helper()
	_, taken, err := pool.Acquire(config)
	if err != nil {
		t.Fatal(err)
	}
	return taken
}

// hold takes a slot for another user. The daemon locks each free slot for a moment while it counts the slots of
// others, so a first try can find the slot taken; the next one gets it.
func hold(t *testing.T, pool *slots.Pool, config slots.Config) {
	t.Helper()
	waitFor(t, func() bool { return acquire(t, pool, config) }, "a free machine slot for the other user")
}

func (s *loadScene) end(t *testing.T, client *heavyClient) {
	t.Helper()
	for _, pid := range s.commandPIDs(client) {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
	}
	if status := client.wait(t); status != 143 {
		t.Fatalf("the client ended with %d, want 143", status)
	}
}

// The limit of the machine holds over the daemons of all users: a run waits while the daemon of another user
// holds the only slot, starts when that one frees it, and holds it until its command ends.
func TestARunWaitsForTheMachineSlotAnotherUserHolds(t *testing.T) {
	scene := startLoadDaemon(t, plenty)
	config := scene.limitMachine(t, 1)
	other := slots.NewPool()
	hold(t, other, config)

	run := scene.start(t, repository(t), "sleep", "300.41")
	waits := `heavy: waits for one of the 1 machine slots \(` + regexp.QuoteMeta(config.Folder) + `\)\n`
	scene.step(t, 3, "line about the machine slot", func() bool { return run.has(waits) })
	scene.pass(t, 2)
	if run.has(startedLine) || strings.Count(run.output(), "machine slots") != 1 {
		t.Fatalf("the run printed %q, want it waiting with one line about the slot", run.output())
	}

	other.Release(1)
	scene.step(t, 3, "start after the release", func() bool { return run.has(startedLine + `sleep 300.41\n`) })
	if acquire(t, other, config) {
		t.Fatal("the other user took the slot the running run holds")
	}

	scene.end(t, run)
	scene.step(t, 3, "free slot after the run", func() bool { return acquire(t, other, config) })
}

// With a machine configuration its limit stands in for the default of three runs per user.
func TestTheMachineLimitReplacesTheDefaultOfThreeRuns(t *testing.T) {
	scene := startLoadDaemon(t, plenty)
	scene.limitMachine(t, 4)
	checkouts := []string{repository(t), repository(t), repository(t), repository(t), repository(t)}
	scene.remember(t, "sleep", checkouts...)

	var runs []*heavyClient
	for index, checkout := range checkouts {
		seconds := fmt.Sprintf("300.5%d", index)
		runs = append(runs, scene.start(t, checkout, "sleep", seconds))
		if index < 4 {
			scene.step(t, 3, "start of run "+seconds, func() bool { return runs[index].has(startedLine + `sleep ` + seconds + `\n`) })
		}
	}

	scene.pass(t, 3)
	if runs[4].has(startedLine) || runs[4].has("machine slots") {
		t.Fatalf("the fifth run printed %q, want it waiting in the queue behind the limit of 4", runs[4].output())
	}
}

// A user's own max_parallel still lowers the limit for that user.
func TestAPersonalLimitBelowTheMachineLimitHolds(t *testing.T) {
	scene := startLoadDaemon(t, plenty)
	write(t, scene.configuration, `{`+strings.Replace(neutralLoad, `"load": {`, `"load": {"max_parallel": 1, `, 1)+`}`)
	scene.limitMachine(t, 4)
	first, second := repository(t), repository(t)
	scene.remember(t, "sleep", first, second)

	one := scene.start(t, first, "sleep", "300.61")
	scene.step(t, 3, "start of the first run", func() bool { return one.has(startedLine + `sleep 300.61\n`) })
	two := scene.start(t, second, "sleep", "300.62")
	scene.pass(t, 3)

	if two.has(startedLine) {
		t.Fatalf("the second run printed %q, want it waiting behind the user's own limit of 1", two.output())
	}
}

// A broken machine configuration keeps the last good one and is logged once.
func TestABrokenMachineConfigurationKeepsTheLastOne(t *testing.T) {
	scene := startLoadDaemon(t, plenty)
	config := scene.limitMachine(t, 1)
	other := slots.NewPool()
	hold(t, other, config)
	// The pool is not used again below; without this the collector may close its slot file, which frees the slot.
	defer runtime.KeepAlive(other)
	write(t, config.Path, `{"max_parallel": 0}`)
	scene.pass(t, 3)

	run := scene.start(t, repository(t), "sleep", "300.71")

	scene.step(t, 3, "line about the machine slot", func() bool { return run.has(`heavy: waits for one of the 1 machine slots`) })
	failures := 0
	for _, record := range logRecords(t, scene.state) {
		if record["msg"] == "reading the machine configuration failed, the last one stays" {
			failures++
			want := "heavy: " + config.Path + `: expected {"max_parallel": 8, "slots": "/var/lib/heavy/slots"} with both keys, "max_parallel" a whole number from 1 and "slots" an absolute folder, got {"max_parallel": 0}`
			if record["error"] != want {
				t.Fatalf("logged error %q, want %q", record["error"], want)
			}
		}
	}
	if failures != 1 {
		t.Fatalf("the daemon logged the broken machine configuration %d times, want once", failures)
	}
}

// Without the slot folder the run starts and says what is missing: a machine that starts nothing helps nobody.
func TestWithoutTheSlotFolderARunStartsAndSaysSo(t *testing.T) {
	scene := startLoadDaemon(t, plenty)
	config := scene.limitMachine(t, 2)
	if err := os.Remove(config.Folder); err != nil {
		t.Fatal(err)
	}

	run := scene.start(t, repository(t), "sleep", "300.81")

	missing := `heavy: the slot folder ` + regexp.QuoteMeta(config.Folder) + ` of ` + regexp.QuoteMeta(config.Path) + ` is missing; create it with mode 1777\n`
	scene.step(t, 3, "start without a slot", func() bool { return run.has(startedLine+`sleep 300.81\n`) && run.has(missing) })
	scene.pass(t, 2)
	if count := strings.Count(run.output(), "is missing; create it"); count != 1 {
		t.Fatalf("the run heard of the missing folder %d times, want once: %q", count, run.output())
	}
}

// A run that runs already, announced again after a daemon ended, takes a machine slot; without a free one it keeps
// running, and the log says that it runs beyond the limit.
func TestAnAdoptedRunningRunTakesAMachineSlot(t *testing.T) {
	current := startDaemon(t, protocol.Identity{})
	config := slots.Config{Path: filepath.Join(current.state, "machine.json"), MaxParallel: 1, Folder: t.TempDir()}
	write(t, config.Path, fmt.Sprintf(`{"max_parallel": 1, "slots": %q}`, config.Folder))
	now := current.clock.advanceOnce(t, 2*time.Second)
	current.clock.tick(t, now)
	current.clock.tick(t, now)
	waitFor(t, func() bool { return limitLogged(t, current.state, config) }, "the machine limit in the daemon log")
	adopt := func(id, checkout string) {
		// The run's group is a real one of this test: the daemon ends the group of a run whose client went away.
		command := exec.Command("sleep", "30.91")
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = command.Process.Kill()
			_ = command.Wait()
		})
		adoption := protocol.Adoption{ID: id, State: "running", Since: 1, Order: 1, ProcessGroup: command.Process.Pid, Cores: 4}
		registration := protocol.Register{Label: "sleep", Command: []string{"sleep"}, Checkout: checkout, Worktree: checkout, Adopt: &adoption}
		_, _, connection := greet(t, current.state, `{"protocol":1,"version":"","stamp":""}`)
		if err := protocol.Write(connection, protocol.Message{Type: protocol.TypeRegister, Register: &registration}); err != nil {
			t.Fatalf("registering: %v", err)
		}
	}

	adopt("first", "/c1")
	waitFor(t, func() bool { return len(askStatus(t, current.state, false).Runs) == 1 }, "the first adopted run")
	if acquire(t, slots.NewPool(), config) {
		t.Fatal("another user took the slot of the adopted run")
	}
	adopt("second", "/c2")
	waitFor(t, func() bool { return len(askStatus(t, current.state, false).Runs) == 2 }, "the second adopted run")

	var beyond []map[string]any
	for _, record := range logRecords(t, current.state) {
		if record["msg"] == "a run announced again runs without a machine slot" {
			beyond = append(beyond, record)
		}
	}
	want := []map[string]any{{"level": "WARN", "msg": "a run announced again runs without a machine slot", "id": "second", "taken": false, "error": "<nil>"}}
	if !reflect.DeepEqual(beyond, want) {
		t.Fatalf("log records about runs beyond the limit = %v, want %v", beyond, want)
	}
}

// A run that started without a slot, as before the slot folder existed, takes one as soon as it can, so the
// daemons of other users count it.
func TestARunWithoutASlotTakesOneOnceTheFolderIsThere(t *testing.T) {
	scene := startLoadDaemon(t, plenty)
	config := scene.limitMachine(t, 1)
	if err := os.Remove(config.Folder); err != nil {
		t.Fatal(err)
	}
	run := scene.start(t, repository(t), "sleep", "300.95")
	scene.step(t, 3, "start without a slot", func() bool { return run.has(startedLine + `sleep 300.95\n`) })

	if err := os.Mkdir(config.Folder, 0o777); err != nil {
		t.Fatal(err)
	}
	scene.pass(t, 2)

	if acquire(t, slots.NewPool(), config) {
		t.Fatal("another user took the only slot while the run that started without one still runs")
	}
}
