package scheduler_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/scheduler"
)

const gibibyte = int64(1) << 30

var now = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func ago(seconds int) time.Time { return now.Add(-time.Duration(seconds) * time.Second) }

func seconds(count int) time.Duration { return time.Duration(count) * time.Second }

// running builds a running run that started the given seconds ago.
func running(id, checkout string, order float64, startedSecondsAgo int) scheduler.Run {
	return scheduler.Run{ID: id, Checkout: checkout, Status: scheduler.Running, Order: order,
		Arrived: ago(startedSecondsAgo + 5), Started: ago(startedSecondsAgo)}
}

// waiting builds a waiting run; a prediction of 0 means unknown.
func waiting(id, checkout string, order float64, predictedBytes int64) scheduler.Run {
	return scheduler.Run{ID: id, Checkout: checkout, Status: scheduler.Waiting, Order: order, Arrived: ago(100 - int(order)),
		Predicted: predictedBytes > 0, PredictedBytes: predictedBytes}
}

// paused builds a run paused the given seconds ago for the reason.
func paused(id, checkout string, order float64, startedSecondsAgo, pausedSecondsAgo int, reason scheduler.PauseReason) scheduler.Run {
	run := running(id, checkout, order, startedSecondsAgo)
	run.Status = scheduler.Paused
	run.PausedReason = reason
	run.PausedAt = ago(pausedSecondsAgo)
	return run
}

func with(run scheduler.Run, change func(*scheduler.Run)) scheduler.Run {
	change(&run)
	return run
}

func exclusive(run scheduler.Run) scheduler.Run {
	return with(run, func(r *scheduler.Run) { r.Exclusive = true })
}

func restart(run scheduler.Run) scheduler.Run {
	return with(run, func(r *scheduler.Run) { r.Restart = true })
}

// neutral measurements: 18 cores, load 0, VM 10 GiB free, host 50 % of 48 GiB, no swap, no pressure, no recovery.
func neutral(change ...func(*scheduler.Measurements)) scheduler.Measurements {
	m := scheduler.Measurements{Cores: 18, Load1: 0, VMMeasured: true, VMAvailableBytes: 10 * gibibyte,
		HostFreePercent: 50, HostMemoryBytes: 48 * gibibyte}
	for _, apply := range change {
		apply(&m)
	}
	return m
}

func vm(bytes int64) func(*scheduler.Measurements) {
	return func(m *scheduler.Measurements) { m.VMAvailableBytes = bytes }
}
func load(value float64) func(*scheduler.Measurements) {
	return func(m *scheduler.Measurements) { m.Load1 = value }
}
func host(percent int) func(*scheduler.Measurements) {
	return func(m *scheduler.Measurements) { m.HostFreePercent = percent }
}
func swap(bytes int64) func(*scheduler.Measurements) {
	return func(m *scheduler.Measurements) { m.SwapOutBytesPer2s = bytes }
}
func pressure(duration time.Duration) func(*scheduler.Measurements) {
	return func(m *scheduler.Measurements) { m.PressureFor = duration }
}
func recovered(duration time.Duration) func(*scheduler.Measurements) {
	return func(m *scheduler.Measurements) { m.RecoveredFor = duration }
}

func config(change ...func(*scheduler.Config)) scheduler.Config {
	cfg := scheduler.DefaultConfig(lists.DefaultLoad())
	for _, apply := range change {
		apply(&cfg)
	}
	return cfg
}

var decideCases = []struct {
	name  string
	state scheduler.State
	m     scheduler.Measurements
	cfg   scheduler.Config
	want  []scheduler.Action
}{
	// plan 2.2: Queue leer
	{"queue empty", scheduler.State{}, neutral(), config(), nil},
	// plan 2.2: A wartet (c1, Prognose 3 GiB), nichts aktiv
	{"lone waiting run starts with all cores",
		scheduler.State{Runs: []scheduler.Run{waiting("A", "c1", 1, 3*gibibyte)}}, neutral(), config(),
		[]scheduler.Action{{Kind: scheduler.Start, ID: "A", Cores: 18}}},
	// plan 2.2: A läuft (c1), B wartet (c1)
	{"same checkout waits",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c1", 2, gibibyte)}}, neutral(), config(), nil},
	// plan 2.2: A läuft (c1), B wartet (c2, 3 GiB), VM 6 GiB, Last 6,8
	{"second checkout starts with cores minus load",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c2", 2, 3*gibibyte)}},
		neutral(vm(6*gibibyte), load(6.8)), config(),
		[]scheduler.Action{{Kind: scheduler.Start, ID: "B", Cores: 11}}},
	// plan 2.2: wie oben, VM 4,5 GiB
	{"reserve not kept",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c2", 2, 3*gibibyte)}},
		neutral(vm(4*gibibyte+gibibyte/2), load(6.8)), config(), nil},
	// plan 2.2: A läuft (c1), B wartet (c2, Prognose unbekannt)
	{"unknown prediction waits for an idle queue",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c2", 2, 0)}}, neutral(), config(), nil},
	// plan 2.2: nichts aktiv, B wartet (Prognose unbekannt)
	{"unknown prediction starts alone",
		scheduler.State{Runs: []scheduler.Run{waiting("B", "c2", 1, 0)}}, neutral(), config(),
		[]scheduler.Action{{Kind: scheduler.Start, ID: "B", Cores: 18}}},
	// plan 2.2: A läuft exclusive, B wartet (c2)
	{"active exclusive run blocks",
		scheduler.State{Runs: []scheduler.Run{exclusive(running("A", "c1", 1, 60)), waiting("B", "c2", 2, gibibyte)}}, neutral(), config(), nil},
	// plan 2.2: A läuft (c1), B wartet exclusive (c2)
	{"waiting exclusive run needs an idle queue",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), exclusive(waiting("B", "c2", 2, gibibyte))}}, neutral(), config(), nil},
	// plan 2.2: A, B, C laufen (c1, c2, c3), D wartet (c4)
	{"max parallel reached",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 90), running("B", "c2", 2, 60), running("C", "c3", 3, 30), waiting("D", "c4", 4, gibibyte)}},
		neutral(), config(), nil},
	// plan 2.2: A läuft (c1); B wartet (c1, #1); C wartet (c2, #2, 1 GiB), VM 8 GiB; 18 cores because load is 0, so floor(18 - 0) = 18 while A is active
	{"waiting run skips a busy checkout",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 0, 60), waiting("B", "c1", 1, gibibyte), waiting("C", "c2", 2, gibibyte)}},
		neutral(vm(8 * gibibyte)), config(),
		[]scheduler.Action{{Kind: scheduler.Start, ID: "C", Cores: 18}}},
	// plan 2.2, changed 2026-11-09 (V52): A läuft (c1); B wartet (c2, #1, 8 GiB) seit 99 s, VM 6 GiB; C wartet (c3, #2,
	// 1 GiB) und passt, also zieht C an B vorbei
	{"later run passes a blocked one that waits less than 15 minutes",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 0, 60), waiting("B", "c2", 1, 8*gibibyte), waiting("C", "c3", 2, gibibyte)}},
		neutral(vm(6 * gibibyte)), config(),
		[]scheduler.Action{{Kind: scheduler.Start, ID: "C", Cores: 18}}},
	// V52: B wartet seit 15 Minuten, dann zieht keiner mehr vorbei, damit B nicht verhungert
	{"later run does not pass a blocked one that waits 15 minutes",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 0, 60),
			with(waiting("B", "c2", 1, 8*gibibyte), func(r *scheduler.Run) { r.Arrived = ago(15 * 60) }), waiting("C", "c3", 2, gibibyte)}},
		neutral(vm(6 * gibibyte)), config(), nil},
	// V52: ein Lauf ohne Prognose wartet, ein kleiner mit Prognose dahinter zieht vorbei
	{"later run passes a run without prediction",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 0, 60), waiting("B", "c2", 1, 0), waiting("C", "c3", 2, gibibyte)}},
		neutral(), config(),
		[]scheduler.Action{{Kind: scheduler.Start, ID: "C", Cores: 18}}},
	// plan 2.2: Last je Kern 0,85
	{"load per core too high",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c2", 2, gibibyte)}},
		neutral(load(0.85 * 18)), config(), nil},
	// plan 2.2: Auslagern 5 MiB je 2 s
	{"swap out too high",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c2", 2, gibibyte)}},
		neutral(swap(5 << 20)), config(), nil},
	// plan 2.2: Host 15 %
	{"host memory too low",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c2", 2, gibibyte)}},
		neutral(host(15)), config(), nil},
	// plan 2.2: A (Start 10:00) und B (10:05) laufen, VM 1,2 GiB seit 10 s
	{"pressure pauses the youngest run",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 300), running("B", "c2", 2, 0)}},
		neutral(vm(12*gibibyte/10), pressure(seconds(10))), config(),
		[]scheduler.Action{{Kind: scheduler.Pause, ID: "B", Reason: scheduler.PausedForMemory}}},
	// plan 2.2: wie oben, erst seit 8 s
	{"pressure too short",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 300), running("B", "c2", 2, 0)}},
		neutral(vm(12*gibibyte/10), pressure(seconds(8))), config(), nil},
	// plan 2.2: nur A läuft, VM 1,2 GiB seit 60 s
	{"lone run is never paused",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 300)}},
		neutral(vm(12*gibibyte/10), pressure(seconds(60))), config(), nil},
	// plan 2.2: B pausiert (memory), VM 2,6 GiB und Host 25 % seit 30 s
	{"recovery resumes the paused run",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 300), paused("B", "c2", 2, 200, 100, scheduler.PausedForMemory)}},
		neutral(vm(26*gibibyte/10), host(25), recovered(seconds(30))), config(),
		[]scheduler.Action{{Kind: scheduler.Resume, ID: "B"}}},
	// plan 2.2: wie oben, seit 20 s
	{"recovery too short",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 300), paused("B", "c2", 2, 200, 100, scheduler.PausedForMemory)}},
		neutral(vm(26*gibibyte/10), host(25), recovered(seconds(20))), config(), nil},
	// plan 2.2: B pausiert (user), Erholung seit 30 s
	{"user pause is never resumed",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 300), paused("B", "c2", 2, 200, 100, scheduler.PausedByUser)}},
		neutral(vm(26*gibibyte/10), host(25), recovered(seconds(30))), config(), nil},
	// plan 2.2: B pausiert (memory) seit 60 s, Druck hält an, B restart
	{"restart ends the paused run with restart",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 300), restart(paused("B", "c2", 2, 200, 60, scheduler.PausedForMemory))}},
		neutral(vm(12*gibibyte/10), pressure(seconds(90))), config(),
		[]scheduler.Action{{Kind: scheduler.Terminate, ID: "B"}, {Kind: scheduler.Requeue, ID: "B", Order: 1}}},
	// plan 2.2: B pausiert (memory) seit 60 s, Druck hält an, nur A (älter, läuft) hat restart
	{"restart ends a running run with restart",
		scheduler.State{Runs: []scheduler.Run{restart(running("A", "c1", 1, 300)), paused("B", "c2", 2, 200, 60, scheduler.PausedForMemory)}},
		neutral(vm(12*gibibyte/10), pressure(seconds(90))), config(),
		[]scheduler.Action{{Kind: scheduler.Terminate, ID: "A"}, {Kind: scheduler.Requeue, ID: "A", Order: 0}}},
	// plan 2.2: B pausiert seit 60 s, Druck hält an, kein Lauf mit restart
	{"no run with restart",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 300), paused("B", "c2", 2, 200, 60, scheduler.PausedForMemory)}},
		neutral(vm(12*gibibyte/10), pressure(seconds(90))), config(), nil},
	// plan 2.2: B pausiert (memory), C wartet (c3), VM 3 GiB seit 10 s (keine 30 s Erholung)
	{"paused run blocks admission",
		scheduler.State{Runs: []scheduler.Run{paused("B", "c2", 2, 200, 100, scheduler.PausedForMemory), waiting("C", "c3", 3, gibibyte)}},
		neutral(vm(3*gibibyte), recovered(seconds(10))), config(), nil},
	// plan 2.2: A, B, C (Start A < B < C), C pausiert vor 10 s, Druck hält an
	{"second pause after the spacing",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 300), running("B", "c2", 2, 200), paused("C", "c3", 3, 100, 10, scheduler.PausedForMemory)}},
		neutral(vm(12*gibibyte/10), pressure(seconds(30))), config(),
		[]scheduler.Action{{Kind: scheduler.Pause, ID: "B", Reason: scheduler.PausedForMemory}}},
	// plan 2.2: B und C pausiert (C zuerst, B danach), Erholung 30 s
	{"resume the last paused run first",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 400), paused("C", "c3", 3, 200, 100, scheduler.PausedForMemory), paused("B", "c2", 2, 300, 50, scheduler.PausedForMemory)}},
		neutral(vm(26*gibibyte/10), host(25), recovered(seconds(30))), config(),
		[]scheduler.Action{{Kind: scheduler.Resume, ID: "B"}}},
	// plan 2.2: Kerne bei Last 17,5 und einem laufenden Lauf (load_per_core 1000)
	{"cores never below one",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c2", 2, gibibyte)}},
		neutral(load(17.5)), config(func(c *scheduler.Config) { c.Load.LoadPerCore = 1000 }),
		[]scheduler.Action{{Kind: scheduler.Start, ID: "B", Cores: 1}}},
	// plan 2.2: VM nicht gemessen, Host 48 GiB x 30 % frei
	{"host memory stands in for the VM",
		scheduler.State{Runs: []scheduler.Run{waiting("A", "c1", 1, 3*gibibyte)}},
		neutral(func(m *scheduler.Measurements) { m.VMMeasured = false; m.VMAvailableBytes = 0 }, host(30)), config(),
		[]scheduler.Action{{Kind: scheduler.Start, ID: "A", Cores: 18}}},
	// plan 2.2: Requeue bei wartenden order 5 und 7 bekommt order 4
	{"requeue goes ahead of the waiting runs",
		scheduler.State{Runs: []scheduler.Run{restart(running("A", "c1", 1, 300)), paused("B", "c2", 2, 200, 60, scheduler.PausedForMemory),
			waiting("W5", "c3", 5, gibibyte), waiting("W7", "c4", 7, gibibyte)}},
		neutral(vm(12*gibibyte/10), pressure(seconds(90))), config(),
		[]scheduler.Action{{Kind: scheduler.Terminate, ID: "A"}, {Kind: scheduler.Requeue, ID: "A", Order: 4}}},

	// plan 2.2 note: zweites Anhalten nur 4 s nach dem letzten
	{"second pause too soon",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 300), running("B", "c2", 2, 200), paused("C", "c3", 3, 100, 4, scheduler.PausedForMemory)}},
		neutral(vm(12*gibibyte/10), pressure(seconds(30))), config(), nil},
	// plan 2.2 note: Neustart 30 s nach LastRestart gibt es nicht
	{"no second restart within the hold",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 300), restart(paused("B", "c2", 2, 200, 90, scheduler.PausedForMemory))}, LastRestart: ago(30)},
		neutral(vm(12*gibibyte/10), pressure(seconds(90))), config(), nil},
	// plan 2.2 note: Gegenstück, Neustart 60 s nach LastRestart ist erlaubt
	{"restart again after the hold",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 300), restart(paused("B", "c2", 2, 200, 90, scheduler.PausedForMemory))}, LastRestart: ago(60)},
		neutral(vm(12*gibibyte/10), pressure(seconds(90))), config(),
		[]scheduler.Action{{Kind: scheduler.Terminate, ID: "B"}, {Kind: scheduler.Requeue, ID: "B", Order: 1}}},
	// plan 2.2 note: Fortsetzen 10 s nach LastResume
	{"resume too soon after the last resume",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 300), paused("B", "c2", 2, 200, 100, scheduler.PausedForMemory)}, LastResume: ago(10)},
		neutral(vm(26*gibibyte/10), host(25), recovered(seconds(60))), config(), nil},
	// plan 2.2 note: Druck sperrt die Zulassung eines Laufs, der passen würde
	{"pressure blocks admission",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 60), waiting("B", "c2", 2, gibibyte)}},
		neutral(pressure(seconds(10))), config(), nil},
	// plan 2.2 note: Druck brach nach dem Anhalten ab (PressureFor 20 s < 60 s seit dem Anhalten), kein Neustart, kein Anhalten
	{"no restart when pressure did not hold throughout",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 300), restart(paused("B", "c2", 2, 200, 60, scheduler.PausedForMemory))}},
		neutral(vm(12*gibibyte/10), pressure(seconds(20))), config(), nil},
	// plan 2.2 note: Gegenstück, Druck hielt durchgehend (PressureFor 60 s)
	{"restart when pressure held throughout",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 300), restart(paused("B", "c2", 2, 200, 60, scheduler.PausedForMemory))}},
		neutral(vm(12*gibibyte/10), pressure(seconds(60))), config(),
		[]scheduler.Action{{Kind: scheduler.Terminate, ID: "B"}, {Kind: scheduler.Requeue, ID: "B", Order: 1}}},
	// plan 2.2 note: ein vom User angehaltener Lauf wird nie neu gestartet
	{"user paused run is never restarted",
		scheduler.State{Runs: []scheduler.Run{running("A", "c1", 1, 300), restart(paused("B", "c2", 2, 200, 90, scheduler.PausedByUser)),
			paused("C", "c3", 3, 150, 60, scheduler.PausedForMemory)}},
		neutral(vm(12*gibibyte/10), pressure(seconds(90))), config(), nil},
}

func TestDecide(t *testing.T) {
	for _, c := range decideCases {
		t.Run(c.name, func(t *testing.T) {
			got := scheduler.Decide(c.state, c.m, c.cfg, now)
			if len(c.want) == 0 {
				if len(got) != 0 {
					t.Errorf("Decide() = %#v, want no actions", got)
				}
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("Decide() = %#v\nwant       %#v", got, c.want)
			}
		})
	}
}

// plan 2.2 note: Cores < 1 ist ein Programmierfehler
func TestDecideRejectsMeasurementsWithoutCores(t *testing.T) {
	defer func() {
		got := recover()
		want := "scheduler: measurements with 0 cores"
		if got != want {
			t.Errorf("recovered %#v, want %#v", got, want)
		}
	}()
	scheduler.Decide(scheduler.State{}, neutral(func(m *scheduler.Measurements) { m.Cores = 0 }), config(), now)
}
