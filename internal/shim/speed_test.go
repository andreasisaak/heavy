package shim_test

import (
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/testkit"
)

// NFR-1: a light call adds less than two starts of heavy to the real program. The medians of 50 runs each:
// `make --version` through the shim in a checkout where heavy is on, the fake make started directly, and
// `heavy --help`, which only prints. The limit counts in starts of heavy because a fixed time fails on a slow CI
// runner (20.7 ms on macOS, run 43074748417) and a start of sh says nothing about a start of a Go binary (0.7 ms
// against 2.8 ms on Linux, run 43248801419). The shim itself costs about one start. A list read with its git call
// costs 3.5 starts and more on a Mac; on Linux it costs about 1.7 and may pass here, and
// TestVersionOrHelpCallPassesWithoutTheLists catches it without a clock.
func TestLightMakeCallAddsLessThanTwoStartsOfHeavy(t *testing.T) {
	f := newFixture(t)
	real := testkit.FakeProgram(t, f.fakes, "make", "exit 0")
	silent := func(got outcome) {
		if got != (outcome{}) {
			t.Fatalf("make --version = %+v, want exit status 0 and no output", got)
		}
	}

	through := median(func() { silent(f.call(t, f.project, f.environment(), "", "make", "--version")) })
	direct := median(func() { silent(run(t, real, f.project, f.environment(), "", "--version")) })
	start := median(func() {
		if got := run(t, f.heavy, f.project, f.environment(), "", "--help"); got.exitStatus != 0 || got.stderr != "" {
			t.Fatalf("heavy --help = %+v, want exit status 0 and nothing on stderr", got)
		}
	})

	t.Logf("make --version: median %s through %s, %s started directly; heavy --help: %s", through, filepath.Join(f.shims, "make"), direct, start)
	if limit := direct + 2*start; through >= limit {
		t.Fatalf("median %s through the shim, want under %s: the %s of the fake make alone and two starts of heavy of %s", through, limit, direct, start)
	}
}

func median(call func()) time.Duration {
	durations := make([]time.Duration, 50)
	for index := range durations {
		started := time.Now()
		call()
		durations[index] = time.Since(started)
	}
	sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
	return (durations[24] + durations[25]) / 2
}
