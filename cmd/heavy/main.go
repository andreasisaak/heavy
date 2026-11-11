// Command heavy queues heavy commands of parallel Claude Code sessions behind one machine-wide
// queue. The same binary serves as the shims: started under the name of a shimmed program, it
// decides whether that call is heavy.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const usage = "Usage: heavy [--directory <dir>] [--agent <name>] <command> [arguments...]" +
	" | heavy --status [--json] | heavy --forward <position>" +
	" | heavy --pause <id> | heavy --resume <id> | heavy --stop <id>" +
	" | heavy --dashboard | heavy --monitor [--session <id>] | heavy --daemon | heavy --helper" +
	" | heavy --hook guard|stop|session-start | heavy --herdr-refresh | heavy --herdr-forward | heavy --help"

// exitUsage is the exit status of a call heavy cannot parse (EX_USAGE of sysexits.h).
const exitUsage = 64

func main() {
	os.Exit(run(filepath.Base(os.Args[0]), os.Args[1:], os.Stdout, os.Stderr))
}

func run(program string, arguments []string, stdout, stderr io.Writer) int {
	if program != "heavy" {
		return runShim(program, arguments, stdout, stderr)
	}
	return command(arguments, stdout, stderr)
}

func command(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 {
		fmt.Fprintf(stderr, "heavy: no command given. %s\n", usage)
		return exitUsage
	}
	switch arguments[0] {
	case "--help":
		fmt.Fprintln(stdout, usage)
		return 0
	case "--daemon":
		return runDaemon(arguments[1:], stderr)
	case "--helper":
		return runHelper(arguments[1:], stdout, stderr)
	case "--hook":
		return runHook(arguments[1:], stdout, stderr)
	case "--status":
		return runStatus(arguments[1:], stdout, stderr)
	case "--forward":
		return runForward(arguments[1:], stdout, stderr)
	case "--pause", "--resume", "--stop":
		return runControl(arguments[0], arguments[1:], stdout, stderr)
	case "--monitor":
		return runMonitor(arguments[1:], stdout, stderr)
	case "--dashboard":
		return runDashboard(arguments[1:], stdout, stderr)
	case "--herdr-refresh":
		return runHerdrRefresh(arguments[1:], stdout, stderr)
	case "--herdr-forward":
		return runHerdrForward(arguments[1:], stdout, stderr)
	default:
		return runCommand(arguments, stdout, stderr)
	}
}

// runHook is `heavy --hook guard|stop|session-start`: the hooks of hooks/hooks.json, started through hooks/build.sh.
func runHook(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) != 1 {
		fmt.Fprintf(stderr, "heavy: --hook needs one of guard, stop, session-start. %s\n", usage)
		return exitUsage
	}
	switch arguments[0] {
	case "guard":
		return runGuardHook(os.Stdin, stdout, stderr)
	case "stop":
		return runStopHook(os.Stdin, stdout, stderr)
	case "session-start":
		return runSessionStartHook(os.Stdin, stdout, stderr)
	}
	fmt.Fprintf(stderr, "heavy: --hook needs one of guard, stop, session-start, got %s. %s\n", arguments[0], usage)
	return exitUsage
}
