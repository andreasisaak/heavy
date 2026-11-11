package shim_test

import (
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/testkit"
)

// NFR-1: a light call reaches the real program in under 15 ms, the median of 50 runs of `make --version` through
// the shim in a checkout where heavy is on. The fake make ends at once; its own start counts, and the test logs
// the median of the fake started directly beside it.
func TestLightMakeCallReachesTheRealMakeWithin15Milliseconds(t *testing.T) {
	f := newFixture(t)
	real := testkit.FakeProgram(t, f.fakes, "make", "exit 0")

	through := median(t, func() outcome { return f.call(t, f.project, f.environment(), "", "make", "--version") })
	direct := median(t, func() outcome { return run(t, real, f.project, f.environment(), "", "--version") })

	t.Logf("make --version: median %s through %s, %s started directly", through, filepath.Join(f.shims, "make"), direct)
	if through >= 15*time.Millisecond {
		t.Fatalf("median %s through the shim, want under 15ms (fake make alone: %s)", through, direct)
	}
}

func median(t *testing.T, call func() outcome) time.Duration {
	t.Helper()
	durations := make([]time.Duration, 50)
	for index := range durations {
		started := time.Now()
		got := call()
		durations[index] = time.Since(started)
		if got != (outcome{}) {
			t.Fatalf("make --version = %+v, want exit status 0 and no output", got)
		}
	}
	sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
	return (durations[24] + durations[25]) / 2
}
