package foreign

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/andreasisaak/heavy/internal/dockercli"
)

// psFormat prints id, name, compose service and compose working directory of a container, tab-separated; docker ps
// turns the `\t` of a format into tabs and prints "" for a label a container lacks.
const psFormat = `{{.ID}}\t{{.Names}}\t{{.Label "com.docker.compose.service"}}\t{{.Label "com.docker.compose.project.working_dir"}}`

// Container is a running container as docker ps names it: the full id, the name, and the compose service and working
// directory, "" for a container started outside compose.
type Container struct {
	ID, Name, Service, WorkingDirectory string
}

// List asks docker ps once, within the timeout, for the running containers.
func List(program string, timeout time.Duration) ([]Container, error) {
	output, err := dockercli.Run(program, timeout, "", "ps", "--no-trunc", "--format", psFormat)
	if err != nil {
		return nil, fmt.Errorf("heavy: docker ps failed: %w", err)
	}
	containers := []Container{}
	if strings.TrimSpace(output) == "" {
		return containers, nil
	}
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			return nil, fmt.Errorf("heavy: docker ps printed %q, expected 4 tab-separated fields (id, name, compose service, compose working directory), got %d", line, len(fields))
		}
		containers = append(containers, Container{ID: fields[0], Name: fields[1], Service: fields[2], WorkingDirectory: fields[3]})
	}
	return containers, nil
}

// LogUnattributed records that docker ps failed under memory pressure, so the containers outside the runs stay
// without a notice this time.
func LogUnattributed(logger *slog.Logger, err error) {
	logger.Warn("foreign load not attributed", "error", err.Error())
}
