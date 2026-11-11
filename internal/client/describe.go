package client

import (
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/andreasisaak/heavy/internal/detect"
	"github.com/andreasisaak/heavy/internal/label"
	"github.com/andreasisaak/heavy/internal/lists"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/shell"
)

// describe names the run for the daemon: its label, checkout and list entry, and the session and herdr pane of
// the caller; it also returns the red patterns of the entry (design G8).
func describe(options Options) (protocol.Register, []*regexp.Regexp, error) {
	path, err := lists.PersonalPath()
	if err != nil {
		return protocol.Register{}, nil, err
	}
	personal, err := lists.LoadPersonal(path)
	if err != nil {
		return protocol.Register{}, nil, err
	}
	logger := slog.New(slog.NewTextHandler(options.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	detector := detect.NewDetector(personal, detect.ContainerLookup(os.Getenv("PATH"), logger), environment())
	name, err := label.Label(options.Argv, options.Directory, detector)
	if err != nil {
		return protocol.Register{}, nil, err
	}
	found, err := detector.Heavy(shell.FromArgv(options.Argv), options.Directory)
	if err != nil {
		return protocol.Register{}, nil, err
	}
	var entry lists.Entry
	if len(found) > 0 && found[0].Index == 0 && found[0].Entry != nil {
		entry = *found[0].Entry
	}
	checkout, worktree, err := placeOf(options.Directory)
	if err != nil {
		return protocol.Register{}, nil, err
	}
	return protocol.Register{
		Label: name, Command: options.Argv, Directory: options.Directory, Checkout: checkout, Worktree: worktree,
		Session: os.Getenv("HEAVY_SESSION"), Agent: options.Agent, Pane: os.Getenv("HERDR_PANE_ID"), ClientPID: os.Getpid(),
		Exclusive: entry.Exclusive, Restart: entry.Restart,
	}, entry.Red, nil
}

// placeOf is the main checkout and the worktree of the directory, both the resolved directory outside git.
func placeOf(directory string) (checkout, worktree string, err error) {
	found, inside, err := lists.FindCheckout(directory)
	if err != nil {
		return "", "", err
	}
	if inside {
		return found.Main, found.Toplevel, nil
	}
	if resolved, err := filepath.EvalSymlinks(directory); err == nil {
		return resolved, resolved, nil
	}
	return filepath.Clean(directory), filepath.Clean(directory), nil
}

// environment is the environment heavy runs in, for the exceptions of list entries.
func environment() map[string]string {
	variables := map[string]string{}
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		variables[name] = value
	}
	return variables
}
