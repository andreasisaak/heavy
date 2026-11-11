package foreign

import (
	"fmt"

	"github.com/andreasisaak/heavy/internal/platform"
	"github.com/andreasisaak/heavy/internal/shell"
)

// noticeLine tells the sessions of a repository (main checkout, any worktree) how little memory the VM has left, which of their containers uses how
// much, and how they could stop it.
func noticeLine(vmAvailableBytes int64, container Container, rssBytes int64) string {
	return fmt.Sprintf("heavy: memory is low (%s %s GiB free); %s of this repository uses %s GiB. If you do not need it now: %s",
		platform.Docker, gibibytes(vmAvailableBytes), container.Name, gibibytes(rssBytes), stopHint(container))
}

// stopHint stops the compose service of a container in its own compose project, named by its working directory, since
// the session may sit in another worktree of the checkout; a container without a service is stopped by name.
func stopHint(container Container) string {
	if container.Service == "" {
		return "docker stop " + container.Name
	}
	return "docker compose --project-directory " + shell.Quote(container.WorkingDirectory) + " stop " + container.Service
}

// gibibytes prints bytes in GiB with one decimal.
func gibibytes(bytes int64) string {
	return fmt.Sprintf("%.1f", float64(bytes)/(1<<30))
}
