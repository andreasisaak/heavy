package shim

import (
	"log/slog"

	"github.com/andreasisaak/heavy/internal/detect"
	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/shell"
)

// verdict tells whether the call is heavy, with the lists and the catalogue of the guard, the words the shell has
// already expanded and the environment the shim sees (COMPOSE_FILE, COMPOSE_PROJECT_DIRECTORY, exceptions). A
// list is read as far as it can be and never stops the call; the hook, the daemon and heavy --status say what it
// holds that heavy does not use, the shim stays silent, since its stderr is that of the program it stands for. Why a container names no compose project goes to stderr.
func verdict(call Call, environment map[string]string) (detect.Match, bool, error) {
	path, err := lists.PersonalPath()
	if err != nil {
		return detect.Match{}, false, err
	}
	personal := lists.LoadPersonal(path)
	logger := slog.New(slog.NewTextHandler(call.Stderr, &slog.HandlerOptions{ReplaceAttr: withoutTime}))
	detector := detect.NewDetector(personal, detect.ContainerLookup(environment["PATH"], logger), environment)
	found, err := detector.Heavy(shell.FromArgv(append([]string{call.Program}, call.Arguments...)), call.Directory)
	if err != nil || len(found) == 0 {
		return detect.Match{}, false, err
	}
	return found[0], true, nil
}

// withoutTime drops the time of a log line: the line lands in the output of the call, next to the program's own.
func withoutTime(groups []string, attribute slog.Attr) slog.Attr {
	if len(groups) == 0 && attribute.Key == slog.TimeKey {
		return slog.Attr{}
	}
	return attribute
}
