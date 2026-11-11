package daemon_test

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// estimated adds unpredicted_peak_bytes to the thresholds of the scene.
func (s *loadScene) estimated(t *testing.T, bytes string) {
	t.Helper()
	write(t, s.configuration, `{`+strings.Replace(neutralLoad, `"load": {`, `"load": {"unpredicted_peak_bytes": `+bytes+`, `, 1)+`}`)
	s.pass(t, 1)
}

// With unpredicted_peak_bytes runs without history start beside each other while the machine has room for the
// estimate of each: of 8 GiB two runs of 3 GiB take 6, and the third waits. heavy --status names the estimate.
func TestRunsWithoutHistoryStartBesideEachOtherOnTheEstimate(t *testing.T) {
	scene := startLoadDaemon(t, plenty)
	scene.estimated(t, "3221225472")

	first := scene.start(t, repository(t), "sleep", "300.11")
	scene.step(t, 3, "start of the first run", func() bool { return first.has(startedLine + `sleep 300.11\n`) })
	second := scene.start(t, repository(t), "sleep", "300.12")
	scene.step(t, 3, "start of the second run beside the first", func() bool { return second.has(startedLine + `sleep 300.12\n`) })
	third := scene.start(t, repository(t), "sleep", "300.13")
	scene.pass(t, 3)

	if third.has(startedLine) {
		t.Fatalf("the third run printed %q, want it waiting: 8 GiB hold two estimates of 3 GiB, not three", third.output())
	}
	output, err := exec.Command(scene.binary, "--status").Output()
	want := regexp.MustCompile(`^running    \S+  sleep 300\.11  \S+  0\.0 GiB \(est\. 3\.0 GiB\)  since \d{2}:\d{2}\n` +
		`running    \S+  sleep 300\.12  \S+  0\.0 GiB \(est\. 3\.0 GiB\)  since \d{2}:\d{2}\n` +
		`waiting 1  \S+  sleep 300\.13  \S+  ~3\.0 GiB est\.           since \d{2}:\d{2}\n$`)
	if err != nil || !want.Match(output) {
		t.Fatalf("heavy --status = %q, %v; want two running runs and a waiting one, each with the estimate", output, err)
	}

	scene.end(t, first)
	scene.step(t, 4, "start of the third run after the first ended", func() bool { return third.has(startedLine + `sleep 300.13\n`) })
}

// Without the key a run without history starts only alone, as before.
func TestWithoutTheEstimateARunWithoutHistoryWaitsForAnIdleQueue(t *testing.T) {
	scene := startLoadDaemon(t, plenty)

	first := scene.start(t, repository(t), "sleep", "300.21")
	scene.step(t, 3, "start of the first run", func() bool { return first.has(startedLine + `sleep 300.21\n`) })
	second := scene.start(t, repository(t), "sleep", "300.22")
	scene.pass(t, 3)

	output, err := exec.Command(scene.binary, "--status").Output()
	if second.has(startedLine) || err != nil || !strings.Contains(string(output), "  ~?  ") || strings.Contains(string(output), "est.") {
		t.Fatalf("the second run printed %q and heavy --status %q, %v; want it waiting with ~? and no estimate", second.output(), output, err)
	}
}
