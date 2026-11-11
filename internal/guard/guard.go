// Package guard is the PreToolUse hook for Bash, `heavy --hook guard`: heavy commands queue behind heavy.
//
// A call with a heavy command (internal/detect: an entry of its project's lists, or a runner of the catalogue
// where the lists turn it on) gets `heavy` right in front of that command and runs in the background: heavy waits
// for its turn in the queue, the session keeps working and is woken when its run ends. The additional context
// names the runs in the queue, read from the running daemon, and forbids edits to checked files until the run is
// done. A call that starts heavy itself goes to the background the same way and gets the same context, so the
// session waits to be woken instead of polling the queue or running into the Bash timeout.
//
// A subagent starts no heavy run: a call with agent_id that would queue is denied, and the subagent names the
// command in its report for the main agent instead. A session started with --agent carries only agent_type; the
// guard passes it to heavy as --agent <type>, so the queue shows which agent runs a run.
//
// Filters at the end of a heavy run's pipeline (| tail, | head, | grep) go before the call is queued, so the user
// can follow the run's output live. A call that only reads the queue (heavy --status) while the session's own run
// waits or runs is denied with the state of that run.
//
// heavy goes in front of a heavy command wherever the call runs it as a command of its own: in the top-level list,
// also of a call of several lines, with comments or heredocs, and inside if, loops, { } and ( ). The work before it
// then runs at once in the same shell, and only the heavy command waits. A heavy command elsewhere (substitution,
// shell string of a command that is no heavy one, case or function body) runs with the whole call through
// `heavy "$SHELL" -c '…'`, so no form of a heavy command passes without the queue; with other work before it, the
// call is denied, since that work would wait as well. Several heavy commands of one call run in one hold of the
// queue, through heavy as a whole, unless other work stands before them: then each queues on its own. A background heavy call in a
// form that hooks keeping background output visible rewrite is denied, with the tee form as the way out.
//
// Errors of heavy's own (a queue that cannot be read, hook input the guard cannot read) deny the call and name
// what was expected and what was found (design G15). A list is no such error: heavy reads the personal list and a
// .heavy.json as far as it can, since a newer heavy may have written them, and a list that stopped every call
// would stop every session of an older heavy. What a list holds that heavy does not use goes into the context of a
// queued call and into the log. A call that is no valid Bash passes
// unchanged: Bash reports the error and runs none of it.
//
// /reload-plugins starts no SessionStart, so the guard keeps the herdr plugin current as well: it reads the version
// of the manifest in CLAUDE_PLUGIN_DATA and writes the manifest again when it is older than this heavy (design G12).
package guard

import (
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/andreasisaak/heavy/internal/detect"
	"github.com/andreasisaak/heavy/internal/herdr"
	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// Dependencies are what the guard reads besides the hook input.
type Dependencies struct {
	// Heavy is the path of the heavy binary the rewrites put in front of a heavy command.
	Heavy string
	// PersonalList is the path of the personal list, ~/.config/heavy/config.json.
	PersonalList string
	// Queue reads the queue and the connected monitors of the running daemon; without a daemon both are empty.
	Queue func() (protocol.Status, error)
	// Container returns the compose working directory of a container, for `docker exec <container>`.
	Container detect.ContainerDirectory
	// Now is the time the minutes of the runs in the queue count to.
	Now func() time.Time
	// Log takes the guard's notes, such as a call it lets pass because it is no valid Bash.
	Log *slog.Logger
	// HerdrPlugin is the herdr plugin of this heavy, with Data "" when Claude Code did not set CLAUDE_PLUGIN_DATA.
	HerdrPlugin herdr.Plugin
}

// Run reads the hook input from stdin and writes the hook answer to stdout when there is one: a rewrite or a
// denial. Its own errors become a denial; Run fails only when it cannot write the answer.
func Run(stdin io.Reader, stdout io.Writer, deps Dependencies) error {
	response, err := respond(stdin, deps)
	if err != nil {
		response = denial(err.Error())
	}
	if response == nil {
		return nil
	}
	return write(stdout, *response)
}

// Deny writes a denial with the reason, for an error before the guard can run, such as a state directory heavy
// cannot name.
func Deny(stdout io.Writer, reason string) error {
	return write(stdout, *denial(reason))
}

func respond(stdin io.Reader, deps Dependencies) (*answer, error) {
	if deps.HerdrPlugin.Data != "" {
		if err := deps.HerdrPlugin.Generate(); err != nil {
			return nil, err
		}
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return nil, fmt.Errorf("heavy: reading the hook input from stdin: %w", err)
	}
	input, err := decodeHookInput(data)
	if err != nil || input.ToolName != "Bash" {
		return nil, err
	}
	tool, err := input.bashInput()
	if err != nil {
		return nil, err
	}
	current, err := parseCall(tool.command)
	if err != nil {
		deps.Log.Warn("the call is no valid Bash, so the guard lets it run for Bash to report", "error", err.Error())
		return nil, nil
	}
	personal := lists.LoadPersonal(deps.PersonalList)
	detector := detect.NewDetector(personal, deps.Container, nil)
	found, err := detector.Heavy(current.script, input.Cwd)
	if err != nil {
		return nil, err
	}
	warnings := append(personal.Warnings, detector.Warnings()...)
	for _, warning := range warnings {
		deps.Log.Warn("a list holds something heavy does not use", "warning", warning)
	}
	runs := heavyRuns(current.script)
	if len(found) == 0 && len(runs) == 0 {
		return poll(current.script, input, deps)
	}
	if input.subagent() {
		return denial(fmt.Sprintf(subagentReason, targetList(append(texts(found), runTexts(runs)...)))), nil
	}
	stripped, removed := stripFilters(current, heavyAt(current, found, runs))
	if len(removed) > 0 {
		if current, err = parseCall(stripped); err != nil {
			return nil, fmt.Errorf("heavy: the call without its output filters is no valid Bash, a bug of the guard: %q: %w", stripped, err)
		}
		if found, err = detector.Heavy(current.script, input.Cwd); err != nil {
			return nil, err
		}
		runs = heavyRuns(current.script)
	}
	response, err := route(current, found, runs, tool, input, deps)
	if err != nil {
		return nil, err
	}
	return warned(noting(response, removed), warnings), nil
}

// heavyAt reports whether the text of the call from start to end holds a heavy command or a heavy run.
func heavyAt(current call, found []detect.Match, runs []startedRun) func(start, end int) bool {
	var offsets []int
	for _, match := range found {
		offsets = append(offsets, rootOffset(current.script, match.Index))
	}
	for _, run := range runs {
		offsets = append(offsets, rootOffset(current.script, run.index))
	}
	return func(start, end int) bool {
		for _, offset := range offsets {
			if offset >= start && offset < end {
				return true
			}
		}
		return false
	}
}
