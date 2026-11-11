package dashboard

import "testing"

// plan 6.3: ten values of the free memory become exactly ten blocks, the smallest ▁ and the largest █
func TestSparklineMapsTenValuesFromTheirMinimumToTheirMaximum(t *testing.T) {
	values := []int64{8 * gibibyte, 8 * gibibyte, 7 * gibibyte, 6 * gibibyte, 4 * gibibyte, 3 * gibibyte, 3 * gibibyte, 2 * gibibyte, 3 * gibibyte, 4 * gibibyte}

	got := Sparkline(values)

	if want := "██▇▆▃▂▂▁▂▃"; got != want {
		t.Fatalf("Sparkline(%v) = %q, want %q", values, got, want)
	}
}

func TestSparklineOfEqualValuesIsAFlatMiddleLine(t *testing.T) {
	values := []int64{5 * gibibyte, 5 * gibibyte, 5 * gibibyte}

	got := Sparkline(values)

	if want := "▄▄▄"; got != want {
		t.Fatalf("Sparkline(%v) = %q, want %q", values, got, want)
	}
}

func TestSparklineOfNoValuesIsEmpty(t *testing.T) {
	got := Sparkline(nil)

	if got != "" {
		t.Fatalf("Sparkline(nil) = %q, want \"\"", got)
	}
}
