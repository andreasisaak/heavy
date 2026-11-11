package shim

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/detect"
)

// Call is one call that reached a shim.
type Call struct {
	// Program is the name heavy was started under, Arguments are the words after it.
	Program   string
	Arguments []string
	// Binary is the heavy binary the shims link to, <plugin>/bin/heavy.
	Binary string
	// Directory is the working directory of the call.
	Directory string
	// Environment is the environment of the call; the program it becomes gets it unchanged.
	Environment []string
	// Connect names the daemon that knows the runs; the shim calls it only when HEAVY_RUN is set.
	Connect func() (daemon.ConnectOptions, error)
	Stderr  io.Writer
}

// target is the program the call becomes, and the note it prints first.
type target struct {
	path string
	argv []string
	note string
}

// Run replaces the process with the program the call becomes, so signals, standard input and the exit status are
// that program's. It returns only when that fails, with the exit status to end with.
func Run(call Call) int {
	environment := variables(call.Environment)
	real, found := detect.RealProgram(call.Program, environment["PATH"])
	if !found {
		shims := filepath.Join(filepath.Dir(filepath.Dir(call.Binary)), "shims")
		fmt.Fprintf(call.Stderr, "heavy: found no %s on PATH outside the heavy shims (%s)\n", call.Program, shims)
		return 127
	}
	next, err := choose(call, real, environment)
	if err != nil {
		fmt.Fprintln(call.Stderr, err)
		return 1
	}
	fmt.Fprint(call.Stderr, next.note)
	err = syscall.Exec(next.path, next.argv, call.Environment)
	fmt.Fprintf(call.Stderr, "heavy: starting %s failed: %v\n", next.path, err)
	if errors.Is(err, syscall.ENOENT) {
		return 127
	}
	return 126
}

// choose is the real program for a light call and inside an active run, there with the run carried into docker
// exec and run; a heavy call becomes `heavy --directory <dir> <program> …`, which waits in the queue.
func choose(call Call, real string, environment map[string]string) (target, error) {
	passOn := target{path: real, argv: append([]string{real}, call.Arguments...)}
	if (call.Program == "docker" && !dockerMayBeHeavy(call.Arguments)) || onlyInformational(call.Arguments) {
		return passOn, nil
	}
	if id := environment["HEAVY_RUN"]; id != "" {
		run, active, reached, err := activeRun(call.Connect, id)
		if err != nil {
			return target{}, err
		}
		if !reached {
			return passOn, nil
		}
		if active {
			if call.Program == "docker" {
				passOn.argv = append([]string{real}, carryRun(call.Arguments, id, run.Budget)...)
			}
			return passOn, nil
		}
	}
	match, heavy, err := verdict(call, environment)
	if err != nil {
		return target{}, err
	}
	if !heavy {
		return passOn, nil
	}
	return target{
		path: call.Binary,
		argv: append([]string{call.Binary, "--directory", match.Directory, call.Program}, call.Arguments...),
		note: fmt.Sprintf("heavy: `%s` is heavy and reached %s without the queue, so it waits here in the machine-wide queue. "+
			"Start heavy calls with run_in_background, then the hook queues them up front.\n", match.Text, call.Program),
	}, nil
}

// variables maps the names of the environment to their values; the first of duplicate names counts, as for getenv.
func variables(environment []string) map[string]string {
	found := make(map[string]string, len(environment))
	for _, entry := range environment {
		name, value, _ := strings.Cut(entry, "=")
		if _, seen := found[name]; !seen {
			found[name] = value
		}
	}
	return found
}
