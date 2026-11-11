package detect

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	composeLabel   = `{{index .Config.Labels "com.docker.compose.project.working_dir"}}`
	inspectTimeout = 10 * time.Second
)

// ContainerLookup returns the compose working directory of a container from its label, read with the real docker
// on path (path without heavy's shims). A docker inspect that fails, or no docker, is logged and finds nothing:
// the call then counts in the project of its cwd alone.
func ContainerLookup(path string, logger *slog.Logger) ContainerDirectory {
	return func(name string) (string, bool) {
		docker, found := RealProgram("docker", path)
		if !found {
			logger.Warn("found no docker on PATH, so the container names no compose project", "container", name)
			return "", false
		}
		deadline, cancel := context.WithTimeout(context.Background(), inspectTimeout)
		defer cancel()
		var stdout, stderr bytes.Buffer
		command := exec.CommandContext(deadline, docker, "inspect", "--format", composeLabel, name)
		command.Stdout, command.Stderr = &stdout, &stderr
		if err := command.Run(); err != nil {
			logger.Warn("docker inspect failed, so the container names no compose project",
				"container", name, "docker", docker, "error", err.Error(), "stderr", strings.TrimSpace(stderr.String()))
			return "", false
		}
		directory := strings.TrimSpace(stdout.String())
		return directory, directory != ""
	}
}

// RealProgram returns the first program of that name on path that is no heavy shim: a shim is a link to the heavy
// binary, so its target is named heavy.
func RealProgram(name, path string) (string, bool) {
	for _, directory := range filepath.SplitList(path) {
		if directory == "" {
			continue
		}
		candidate := filepath.Join(directory, name)
		information, err := os.Stat(candidate)
		if err != nil || information.IsDir() || information.Mode()&0o111 == 0 {
			continue
		}
		if target, err := filepath.EvalSymlinks(candidate); err == nil && filepath.Base(target) == "heavy" {
			continue
		}
		return candidate, true
	}
	return "", false
}
