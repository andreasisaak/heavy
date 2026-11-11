package sampler

import (
	"reflect"
	"testing"
	"time"
)

// design G13: the dashboard's sparkline takes the lowest value of each step, oldest first, and skips empty steps
func TestLowsAreTheLowestValueOfEachStep(t *testing.T) {
	now := time.Date(2026, 11, 8, 14, 0, 0, 0, time.UTC)
	var series Series
	for _, current := range []struct {
		ago   time.Duration
		value float64
	}{{29 * time.Minute, 9}, {28 * time.Minute, 7}, {10 * time.Minute, 5}, {8 * time.Minute, 6}, {0, 4}} {
		series.Add(now.Add(-current.ago), current.value)
	}

	got := series.Lows(now, 3*time.Minute, 10)

	if want := []float64{7, 5, 6, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Lows = %v, want %v", got, want)
	}
}
