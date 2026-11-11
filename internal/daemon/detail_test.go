package daemon_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// detail asks for the status with the machine, as the dashboard does.
func detail(t *testing.T, state string) protocol.Status {
	t.Helper()
	return askStatus(t, state, true)
}

// askStatus asks for the whole status answer, with or without the machine.
func askStatus(t *testing.T, state string, withMachine bool) protocol.Status {
	t.Helper()
	_, reader, connection := greet(t, state, `{"protocol":1,"version":"","stamp":""}`)
	if err := protocol.Write(connection, protocol.Message{Type: protocol.TypeStatus, Status: &protocol.Status{Detail: withMachine}}); err != nil {
		t.Fatal(err)
	}
	answer, err := protocol.ReadMessage(reader)
	if err != nil || answer.Status == nil {
		t.Fatalf("status answer %#v, %v", answer, err)
	}
	return *answer.Status
}

// plan 6.3 and design G13: the dashboard's status carries the machine: its numbers, the free memory of the last
// 30 minutes in steps of 3 minutes, the biggest containers outside the runs while a dashboard asks, and the last
// results of the history, newest first
func TestDetailedStatusCarriesTheMachineForTheDashboard(t *testing.T) {
	project := repository(t)
	found, _, err := lists.FindCheckout(project)
	if err != nil {
		t.Fatal(err)
	}
	scene := startLoadDaemonWithDocker(t, plenty, `ps) case "$*" in *com.docker.compose.service*) printf '%s\thog-1\thog\t%s\n' `+hogID+` `+project+` ;; esac ;;`)
	write(t, filepath.Join(scene.proc, "777", "status"), "Name:\tawk\nVmRSS:\t1572864 kB\n")
	write(t, filepath.Join(scene.proc, "777", "cgroup"), "0::/../"+hogID+"\n")
	write(t, filepath.Join(scene.proc, "777", "environ"), "PATH=/bin\x00")
	scene.remember(t, "ci", "/sites/a", "/sites/b")
	if plain := detail(t, scene.state); plain.Machine == nil || len(plain.Machine.Foreign) != 0 {
		t.Fatalf("first detailed status = %+v, want a machine without a look at the containers yet", plain.Machine)
	}

	scene.step(t, 8, "the containers in the detailed status", func() bool { return len(detail(t, scene.state).Machine.Foreign) == 1 })
	got := *detail(t, scene.state).Machine
	plain := askStatus(t, scene.state, false)

	want := protocol.Machine{
		Cores: 8, VMMeasured: true, VMAvailableBytes: plenty << 10, SwapOutBytesPer2s: 0, Load1: 0.5, HostFreePercent: 50,
		FreeSeries: []int64{plenty << 10},
		Foreign:    []protocol.ForeignContainer{{Name: "hog-1", Checkout: found.Main, Bytes: 1572864 << 10}},
		Recent: []protocol.Result{
			{Label: "ci", Checkout: "/sites/b", ExitStatus: 0, Reason: "passed", Started: 2, Ended: 3},
			{Label: "ci", Checkout: "/sites/a", ExitStatus: 0, Reason: "passed", Started: 2, Ended: 3},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("machine = %+v\nwant      %+v", got, want)
	}
	if plain.Machine != nil {
		t.Fatalf("a status without detail carries the machine %+v", plain.Machine)
	}
	// critic 6: while the dashboard keeps asking every 2 s, every answer names the containers, also between looks
	for step := 0; step < 12; step++ {
		scene.step(t, 1, "a sample", func() bool { return true })
		if foreign := detail(t, scene.state).Machine.Foreign; len(foreign) != 1 {
			t.Fatalf("step %d: foreign = %+v, want the hog in every answer", step, foreign)
		}
	}
}
