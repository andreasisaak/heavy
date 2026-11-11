package daemon_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/platform"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// hogID is the id of a container outside the runs in the fixture VM.
var hogID = strings.Repeat("ab", 32)

// plan 5.2 and FR-28: under memory pressure of 10 s a container of a checkout that belongs to no run reaches the
// monitors with the main checkout of its compose project; a run's tagged process inside it does not count (critic
// 5.1: docker compose exec into a container that ran before the run)
func TestForeignContainerUnderPressureReachesTheMonitorsOfItsCheckout(t *testing.T) {
	project := repository(t)
	found, _, err := lists.FindCheckout(project)
	if err != nil {
		t.Fatal(err)
	}
	scene := startLoadDaemonWithDocker(t, plenty, `ps) case "$*" in *com.docker.compose.service*) printf '%s\thog-1\thog\t%s\n' `+hogID+` `+project+` ;; esac ;;`)
	write(t, filepath.Join(scene.proc, "777", "status"), "Name:\tawk\nVmRSS:\t1572864 kB\n")
	write(t, filepath.Join(scene.proc, "777", "cgroup"), "0::/../"+hogID+"\n")
	write(t, filepath.Join(scene.proc, "777", "environ"), "PATH=/bin\x00")
	other := repository(t)
	scene.remember(t, "sleep", other)
	run := scene.start(t, other, "sleep", "300.51")
	scene.step(t, 3, "start of the run", func() bool { return run.has(startedLine + `sleep 300\.51\n`) })
	id := status(t, scene.state)[0].ID
	write(t, filepath.Join(scene.proc, "778", "status"), "Name:\tphp\nVmRSS:\t1048576 kB\n")
	write(t, filepath.Join(scene.proc, "778", "cgroup"), "0::/../"+hogID+"\n")
	write(t, filepath.Join(scene.proc, "778", "environ"), "HEAVY_RUN="+id+"\x00")
	_, monitor := subscribe(t, scene.state, protocol.Subscribe{Session: "s-f"})

	scene.setAvailable(t, pressure)
	pressureAt := scene.clock.Now()
	scene.step(t, 12, "the foreign notice in the log", func() bool {
		log, err := os.ReadFile(filepath.Join(scene.state, "daemon.log"))
		return err == nil && strings.Contains(string(log), "memory is low")
	})
	noticedAfter := scene.clock.Now().Sub(pressureAt)
	notice := monitor.next(t)

	want := protocol.Message{Type: protocol.TypeEvent, Event: &protocol.Event{Checkout: found.Main, Line: "heavy: memory is low (" + platform.Docker + " 1.0 GiB free); hog-1 of this repository uses 1.5 GiB. " +
		"If you do not need it now: docker compose --project-directory " + project + " stop hog"}}
	if !reflect.DeepEqual(notice, want) {
		t.Fatalf("notice = %#v %+v, want %+v", notice, notice.Event, want.Event)
	}
	// G10 and G5: pressure means 10 s below the threshold
	if noticedAfter < 10*time.Second {
		t.Fatalf("the notice came %s after the pressure began, want at least 10s", noticedAfter)
	}
}
