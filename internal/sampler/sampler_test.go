package sampler_test

import (
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/sampler"
	"github.com/andreasisaak/heavy/internal/scheduler"
)

const mebibyte = 1 << 20

func TestVMStatParserReadsPageSizeAndSwapouts(t *testing.T) {
	output, err := os.ReadFile("testdata/vm_stat.txt")
	if err != nil {
		t.Fatalf("reading the recorded vm_stat output: %v", err)
	}

	pageSize, swapouts, err := sampler.ParseVMStat(string(output))

	if err != nil || pageSize != 16384 || swapouts != 142542991 {
		t.Fatalf("ParseVMStat = %d, %d, %v; want 16384, 142542991", pageSize, swapouts, err)
	}
}

func TestVMStatWithoutSwapoutsIsAnError(t *testing.T) {
	_, _, err := sampler.ParseVMStat("Mach Virtual Memory Statistics: (page size of 16384 bytes)\nPages free: 1.\n")

	want := "heavy: vm_stat printed no Swapouts line, got \"Mach Virtual Memory Statistics: (page size of 16384 bytes)\\nPages free: 1.\\n\""
	if err == nil || err.Error() != want {
		t.Fatalf("ParseVMStat error = %v, want %q", err, want)
	}
}

func TestSwapOutRateOfTwoSamplesIsBytesPerTwoSeconds(t *testing.T) {
	start := time.Date(2026, 11, 7, 20, 0, 0, 0, time.UTC)
	previous := sampler.HostSample{Time: start, PageSize: 16384, SwapOutPages: 1000}
	cases := []struct {
		after time.Duration
		pages uint64
		want  int64
	}{
		{2 * time.Second, 1256, 4 * mebibyte},
		{4 * time.Second, 1512, 4 * mebibyte},
		{2 * time.Second, 1000, 0},
	}
	for _, current := range cases {
		sample := sampler.HostSample{Time: start.Add(current.after), PageSize: 16384, SwapOutPages: current.pages}
		if got := sampler.SwapOutPer2s(previous, sample); got != current.want {
			t.Errorf("SwapOutPer2s after %s with %d pages = %d, want %d", current.after, current.pages, got, current.want)
		}
	}
}

func TestSeriesDropsValuesOlderThanThirtyMinutes(t *testing.T) {
	start := time.Date(2026, 11, 7, 20, 0, 0, 0, time.UTC)
	var series sampler.Series
	for second := 0; second <= 31*60; second += 60 {
		series.Add(start.Add(time.Duration(second)*time.Second), float64(second))
	}

	// the 31 values from 60 to 1860 remain; with the dropped 0 the mean would be 930
	got := series.Mean(time.Hour)

	if got != 960 {
		t.Fatalf("mean over the series = %v, want 960 of the values from 60 to 1860", got)
	}
}

func TestSeriesTellsHowLongAConditionHeld(t *testing.T) {
	start := time.Date(2026, 11, 7, 20, 0, 0, 0, time.UTC)
	var series sampler.Series
	gibibytes := []float64{3, 3, 1.4, 1.4, 1.2, 1.3, 1.4}
	for index, value := range gibibytes {
		series.Add(start.Add(time.Duration(2*index)*time.Second), value)
	}
	below := func(value float64) bool { return value < 1.5 }

	if got := series.HeldFor(below); got != 8*time.Second {
		t.Fatalf("below 1.5 for %s, want 8s", got)
	}
	if got := series.HeldFor(func(value float64) bool { return value > 2 }); got != 0 {
		t.Fatalf("above 2 for %s, want 0 since the newest value is below", got)
	}
}

func TestSeriesMeanCoversTheLastThirtySeconds(t *testing.T) {
	start := time.Date(2026, 11, 7, 20, 0, 0, 0, time.UTC)
	var series sampler.Series
	for second := 0; second <= 60; second += 2 {
		value := 0.0
		if second > 30 {
			value = 6
		}
		series.Add(start.Add(time.Duration(second)*time.Second), value)
	}

	if got := series.Mean(30 * time.Second); got != 6 {
		t.Fatalf("mean of the last 30 s = %v, want 6", got)
	}
}

func TestHostMeasurementsGiveTheSchedulerItsInput(t *testing.T) {
	start := time.Date(2026, 11, 7, 20, 0, 0, 0, time.UTC)
	host := sampler.NewHost()
	levels := []int{40, 40, 8, 8, 8, 8, 8, 8}
	for index, level := range levels {
		host.Add(sampler.HostSample{
			Time: start.Add(time.Duration(2*index) * time.Second), FreePercent: level, MemoryBytes: 48 << 30, Cores: 18,
			Load1: 6.8, PageSize: 16384, SwapOutPages: uint64(1000 + 128*index),
		})
	}

	got := host.Measurements(lists.DefaultLoad())

	want := scheduler.Measurements{
		Cores: 18, Load1: 6.8, VMMeasured: false, HostFreePercent: 8, HostMemoryBytes: 48 << 30,
		SwapOutBytesPer2s: 2 * mebibyte, PressureFor: 10 * time.Second, RecoveredFor: 0,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Measurements = %#v, want %#v", got, want)
	}
}

func TestHostWithoutSamplesMeasuresNothing(t *testing.T) {
	if got := sampler.NewHost().Measurements(lists.DefaultLoad()); !reflect.DeepEqual(got, scheduler.Measurements{}) {
		t.Fatalf("Measurements = %#v, want zero", got)
	}
}

func TestVMStatWithoutPageSizeIsAnError(t *testing.T) {
	_, _, err := sampler.ParseVMStat("Swapouts: 12.\n")

	want := "heavy: vm_stat printed no page size, got \"Swapouts: 12.\\n\""
	if err == nil || err.Error() != want {
		t.Fatalf("ParseVMStat error = %v, want %q", err, want)
	}
}

func TestSwapOutRateRejectsSamplesOutOfOrder(t *testing.T) {
	start := time.Date(2026, 11, 7, 20, 0, 0, 0, time.UTC)
	cases := []struct {
		name            string
		previous, after sampler.HostSample
		want            string
	}{
		{"same time", sampler.HostSample{Time: start, SwapOutPages: 1}, sampler.HostSample{Time: start, SwapOutPages: 2},
			"sampler: sample at 2026-11-07 20:00:00 +0000 UTC does not follow the one at 2026-11-07 20:00:00 +0000 UTC"},
		{"falling counter", sampler.HostSample{Time: start, SwapOutPages: 5}, sampler.HostSample{Time: start.Add(2 * time.Second), SwapOutPages: 4},
			"sampler: swapouts fell from 5 to 4"},
	}
	for _, current := range cases {
		func() {
			defer func() {
				if recovered := recover(); recovered != current.want {
					t.Errorf("%s: recovered %v, want %q", current.name, recovered, current.want)
				}
			}()
			sampler.SwapOutPer2s(current.previous, current.after)
		}()
	}
}
