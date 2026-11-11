package herdr

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// agentEntry is the part of an agent in `herdr agent list` the sidebar reads. herdr sends more fields and adds
// some between releases, so the decoding ignores unknown ones.
type agentEntry struct {
	PaneID string            `json:"pane_id"`
	Cwd    string            `json:"cwd"`
	Tokens map[string]string `json:"tokens"`
}

type agentListing struct {
	Result *struct {
		Agents []agentEntry `json:"agents"`
	} `json:"result"`
}

// agents lists the agents herdr knows with their working directory and current tokens.
func agents(herdr string) ([]agentEntry, error) {
	output, err := call(herdr, "agent", "list")
	if err != nil {
		return nil, fmt.Errorf("heavy: %w", err)
	}
	var listing agentListing
	if err := json.Unmarshal(output, &listing); err != nil || listing.Result == nil {
		return nil, fmt.Errorf(`heavy: herdr agent list: expected {"result": {"agents": [...]}}, got %s`, strings.TrimSpace(string(output)))
	}
	return listing.Result.Agents, nil
}

// belongs reports whether run is a run of the agent in pane with the working directory cwd. A run started in
// herdr belongs to the agent of its pane only, so two agents in one checkout do not both show it. Another run
// belongs to the agents in its directory, or above it when the agent's directory is a git checkout, so an agent
// started on ~/sites does not collect the runs of every project below it.
func belongs(run protocol.RunStatus, pane, cwd string, checkout bool) bool {
	if run.Pane != "" {
		return run.Pane == pane
	}
	return cwd != "" && (run.Directory == cwd || checkout && strings.HasPrefix(run.Directory, cwd+"/"))
}

// isCheckout reports whether directory is a git checkout: .git is there, as a folder or as the file of a worktree.
func isCheckout(directory string) (bool, error) {
	if directory == "" {
		return false, nil
	}
	_, err := os.Stat(filepath.Join(directory, ".git"))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		return false, nil
	}
	return false, fmt.Errorf("heavy: checking for a git checkout in %s: %w", directory, err)
}
