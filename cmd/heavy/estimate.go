package main

import (
	"fmt"
	"io"

	"github.com/andreasisaak/heavy/internal/lists"
)

// unpredictedPeak is unpredicted_peak_bytes of the personal list, the peak the daemon plans a run without history
// with; the status and the dashboard show it beside such a run. A list that cannot be read shows no estimate and
// says why on notes: the daemon keeps its last thresholds then, which this process does not know.
func unpredictedPeak(notes io.Writer) int64 {
	path, err := lists.PersonalPath()
	if err != nil {
		fmt.Fprintln(notes, err)
		return 0
	}
	personal, err := lists.LoadPersonal(path)
	if err != nil {
		fmt.Fprintln(notes, err)
		return 0
	}
	return personal.Load.UnpredictedPeakBytes
}
