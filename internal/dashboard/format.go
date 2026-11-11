package dashboard

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	mebibyte = int64(1) << 20
	gibibyte = int64(1) << 30
)

// size is an amount of memory: one decimal in GiB from 1 GiB on, whole MiB below.
func size(bytes int64) string {
	if bytes >= gibibyte {
		return fmt.Sprintf("%.1f GiB", float64(bytes)/float64(gibibyte))
	}
	return fmt.Sprintf("%d MiB", bytes/mebibyte)
}

// cores is a number of cores, such as 1 core or 9 cores.
func cores(count int) string {
	if count == 1 {
		return "1 core"
	}
	return fmt.Sprintf("%d cores", count)
}

// place is the last part of a checkout or directory, such as shopfront-csv-export.
func place(path string) string {
	return filepath.Base(path)
}

// rightAligned puts right at the end of a line of width and cuts left with … where both do not fit.
func rightAligned(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		left = ansi.Truncate(left, max(0, width-lipgloss.Width(right)-1), "…")
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// fitted cuts every line to the width with …, so no line wraps in the terminal.
func fitted(lines []string, width int) []string {
	cut := make([]string, len(lines))
	for index, line := range lines {
		cut[index] = ansi.Truncate(line, width, "…")
	}
	return cut
}
