package guard

// The texts of the guard of 0.6 (hooks/heavy_guard.py:81-126), with the queue of design G4 instead of its one lock
// (V19).
const (
	contextText = "Queued behind heavy: %s. " +
		"%s, so it starts as soon as this machine has room for it. " +
		"%s " +
		"You will be woken when your run ends: do not poll the queue, and do not stop the run to start it again. " +
		"The heavy monitor of the session reports the start, the first red line and the end of the run with its exit status, " +
		"so build no monitor, tail or watch loop of your own for it. " +
		"Until then keep working without asking the user for approval. " +
		"Change only files the run does not read, or it checks a different state than the one you commit: " +
		"a backend test does not read frontend files, and reading, planning the next task and reviewing the diff change nothing. " +
		"End your turn only when no such work is left. " +
		"A run that names one to three test files stays outside the queue. " +
		"Running: %s"
	filtersNote = " The hook removed %s behind the heavy run, so the user can follow its output live; " +
		"read the end of the task output when you are woken."
	subagentReason = "Subagents start no heavy runs: %s would wait in the machine-wide queue and take a place there for minutes. " +
		"Do not run it in another form either. Finish what you can check without it, and name the exact command " +
		"in your final report: the main agent runs it once for all changes. " +
		"A run that names one to three test files stays outside the queue."
	pollReason = "Your heavy run %s. %s" +
		"You will be woken when it ends, so the queue needs no check: keep working on files the run does not read, " +
		"or end your turn when no such work is left."
	conflictReason = "%s must run through `heavy`, but another hook may rewrite this background call at the same moment " +
		"because %s, and then `heavy` goes missing. Write it in the form such hooks leave alone, with the log " +
		"in an existing directory, for example: set -o pipefail; %s 2>&1 | tee <file>; exit_status=$?; " +
		`echo "%s $exit_status"; exit $exit_status`
	mixedReason = "This call puts other work (%s) before %s, and the hook cannot run it apart from them, " +
		"so it would wait in the queue as well. " +
		"Run the other work in a Bash call of its own first, then %s in a separate call."
	freeText = "No other heavy run is active, your run starts right away."
	// noMonitorNote ends a rewrite context while no monitor of the session is connected (FR-29); no full stop, so
	// the command can be copied as it stands.
	noMonitorNote = " No heavy monitor is connected for this session; start Monitor with: heavy --monitor --session %s"
)

// How the hook queued a call, and what it adds when it also sent the call to the background.
const (
	howInFront = "The hook put `heavy` in front of each heavy command"
	howWhole   = "The hook runs the whole command through `heavy`"
	howDirect  = "The call starts `heavy` itself"
)

var sentToBackground = map[string]string{
	howInFront: " and sent the command to the background",
	howWhole:   " and sent it to the background",
	howDirect:  " and the hook sent it to the background",
}

// wholeShell runs a whole call in a shell of the user's kind.
const wholeShell = `"${SHELL:-/bin/bash}" -c`

// glue are commands that only glue a call together; next to them, several heavy commands run in one hold of the
// lock, and before a heavy command they are no work of their own.
var glue = map[string]bool{
	"cd": true, "set": true, "export": true, "echo": true, "printf": true, "true": true, "false": true, "exit": true,
	"test": true, "[": true, "[[": true, ":": true, "break": true, "continue": true, "wait": true, "tee": true,
	"tail": true, "head": true, "grep": true, "wc": true, "cat": true, "sleep": true, "date": true, "pwd": true,
}

// After the options in front of a run, which take a value, any other option of heavy is an action of its CLI
// (--status, --forward, --stop, --hook …, design G14) and starts no run, nor do status and help, the actions meant
// without dashes, which heavy refuses at once and names the right form.
var (
	noRunWords   = map[string]bool{"status": true, "help": true}
	valueOptions = map[string]bool{"--directory": true, "--agent": true}
)
