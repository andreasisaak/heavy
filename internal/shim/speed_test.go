package shim_test

import (
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/testkit"
)

// NFR-1: a light call costs less than 2.5 starts of the real program, the median of 50 runs of `make --version`
// through the shim in a checkout where heavy is on against the median of the fake make started directly. The limit
// is a ratio, because the start of a process takes 3 ms on a developer Mac and 13 ms on a CI runner; the shim adds
// one start of heavy, and a git call or a list read on the light path adds more than the limit leaves.
func TestLightMakeCallCostsLessThanTwoAndAHalfStartsOfTheRealMake(t *testing.T) {
	f := newFixture(t)
	real := testkit.FakeProgram(t, f.fakes, "make", "exit 0")

	through := median(t, func() outcome { return f.call(t, f.project, f.environment(), "", "make", "--version") })
	direct := median(t, func() outcome { return run(t, real, f.project, f.environment(), "", "--version") })

	t.Logf("make --version: median %s through %s, %s started directly", through, filepath.Join(f.shims, "make"), direct)
	if limit := direct * 5 / 2; through >= limit {
		t.Fatalf("median %s through the shim, want under %s, 2.5 times the %s of the fake make alone", through, limit, direct)
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
