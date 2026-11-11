package main

import (
	"fmt"
	"io"

	"github.com/andreasisaak/heavy/internal/lists"
)

// unpredictedPeak is unpredicted_peak_bytes of the personal list, the peak the daemon plans a run without history
// with; the status and the dashboard show it beside such a run. What the list holds that heavy does not use goes
// to notes.
func unpredictedPeak(notes io.Writer) int64 {
	path, err := lists.PersonalPath()
	if err != nil {
		fmt.Fprintln(notes, err)
		return 0
	}
	personal := lists.LoadPersonal(path)
	for _, warning := range personal.Warnings {
		fmt.Fprintln(notes, warning)
	}
	return personal.Load.UnpredictedPeakBytes
}
