package dashboard

import (
	"math"
	"slices"
	"strings"
)

// blocks are the eight heights of a sparkline, lowest first.
var blocks = []rune("▁▂▃▄▅▆▇█")

// flatBlock is the height of every value when all values are equal: the middle, neither empty nor full.
const flatBlock = '▄'

// Sparkline draws one block per value, oldest first: the smallest value of the series becomes ▁, the largest █, the
// values between them the nearest of the eight heights. A series of equal values is a flat line of ▄.
func Sparkline(values []int64) string {
	if len(values) == 0 {
		return ""
	}
	lowest, highest := slices.Min(values), slices.Max(values)
	var line strings.Builder
	for _, value := range values {
		if lowest == highest {
			line.WriteRune(flatBlock)
			continue
		}
		height := math.Round(float64(value-lowest) * float64(len(blocks)-1) / float64(highest-lowest))
		line.WriteRune(blocks[int(height)])
	}
	return line.String()
}
