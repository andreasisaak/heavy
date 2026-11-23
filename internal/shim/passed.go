package shim

import (
	"strconv"
	"strings"
)

// passedVariable carries the note of a pass: `<pid>:<program>:<path>[:<path>…]`, the process in which a shim
// became a real program, the program's name and every path it became there. An exec keeps the process, so a shim
// that finds its own process and name in the note was handed the call back by the last path, as by the shim of a
// version manager that holds no version of the program and so takes the first one on PATH outside its own folder.
// Without the note the two shims would replace each other without end.
const passedVariable = "HEAVY_SHIM_PASSED"

// handedBack returns the real programs that handed this call back to the shims: the paths of a note written in
// this process for this program. The note of another process or program, inherited by a child, names none.
func handedBack(note string, process int, program string) []string {
	fields := strings.Split(note, ":")
	if len(fields) < 3 || fields[0] != strconv.Itoa(process) || fields[1] != program {
		return nil
	}
	return fields[2:]
}

// withPassed returns environment with the note of this pass in place of any earlier one.
func withPassed(environment []string, process int, program string, paths []string) []string {
	next := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, passedVariable+"=") {
			next = append(next, entry)
		}
	}
	return append(next, passedVariable+"="+strconv.Itoa(process)+":"+program+":"+strings.Join(paths, ":"))
}
