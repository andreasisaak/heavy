package procs

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/andreasisaak/heavy/internal/dockercli"
)

// psFormat prints the id and state (running, paused) of a container, tab-separated.
const psFormat = `{{.ID}}{{"\t"}}{{.State}}`

// inspectFormat prints id, name, compose working directory, start and paused state of a container, tab-separated.
const inspectFormat = `{{.Id}}{{"\t"}}{{.Name}}{{"\t"}}{{index .Config.Labels "com.docker.compose.project.working_dir"}}{{"\t"}}{{.State.StartedAt}}{{"\t"}}{{.State.Paused}}`

// SystemDocker lists, pauses and unpauses containers through the docker program, each call within the timeout. It
// keeps what docker inspect said of each container: a listing asks docker ps for the ids and states and inspects only
// the containers it has not inspected within refreshEvery, since inspecting every container of the machine at each
// sample took longer than the timeout under load (2026-11-09).
type SystemDocker struct {
	program      string
	timeout      time.Duration
	refreshEvery time.Duration
	now          func() time.Time
	mutex        sync.Mutex
	seen         map[string]inspected
}

// inspected is a container as docker inspect showed it, and when.
type inspected struct {
	container Container
	at        time.Time
}

// NewSystemDocker is a SystemDocker that inspects a container again after refreshEvery, which catches a container
// restarted under its id.
func NewSystemDocker(program string, timeout, refreshEvery time.Duration, now func() time.Time) *SystemDocker {
	return &SystemDocker{program: program, timeout: timeout, refreshEvery: refreshEvery, now: now, seen: map[string]inspected{}}
}

// Containers lists the running and paused containers; a container that ends between the two docker calls fails the
// inspect, so the list is taken once more then.
func (d *SystemDocker) Containers() ([]Container, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	containers, err := d.list()
	if err != nil && strings.Contains(err.Error(), "no such object") {
		return d.list()
	}
	return containers, err
}

func (d *SystemDocker) list() ([]Container, error) {
	listed, err := dockercli.Run(d.program, d.timeout, "", "ps", "--no-trunc", "--format", psFormat)
	if err != nil {
		return nil, fmt.Errorf("heavy: docker ps failed: %w", err)
	}
	now := d.now()
	var ids, stale []string
	paused := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(listed), "\n") {
		if line == "" {
			continue
		}
		id, state, found := strings.Cut(line, "\t")
		if !found {
			return nil, fmt.Errorf("heavy: docker ps printed %q, expected id and state", line)
		}
		ids, paused[id] = append(ids, id), state == "paused"
		if known, ok := d.seen[id]; !ok || now.Sub(known.at) >= d.refreshEvery {
			stale = append(stale, id)
		}
	}
	if err := d.inspect(stale, now); err != nil {
		return nil, err
	}
	containers := []Container{}
	current := map[string]bool{}
	for _, id := range ids {
		// A container that ended between docker ps and docker inspect has no record; it is gone.
		known, found := d.seen[id]
		if !found {
			continue
		}
		container := known.container
		container.Paused = paused[id]
		containers, current[id] = append(containers, container), true
	}
	for id := range d.seen {
		if !current[id] {
			delete(d.seen, id)
		}
	}
	return containers, nil
}

// inspect reads the containers of ids and keeps them as seen at now.
func (d *SystemDocker) inspect(ids []string, now time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	output, err := dockercli.Run(d.program, d.timeout, "", append([]string{"inspect", "--format", inspectFormat}, ids...)...)
	if err != nil {
		return fmt.Errorf("heavy: docker inspect failed: %w", err)
	}
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		container, err := parseContainer(line)
		if err != nil {
			return fmt.Errorf("heavy: docker inspect printed %q, expected id, name, working directory, start and paused: %w", line, err)
		}
		d.seen[container.ID] = inspected{container: container, at: now}
	}
	return nil
}

func parseContainer(line string) (Container, error) {
	fields := strings.Split(line, "\t")
	if len(fields) != 5 {
		return Container{}, fmt.Errorf("%d fields", len(fields))
	}
	started, err := time.Parse(time.RFC3339Nano, fields[3])
	if err != nil {
		return Container{}, err
	}
	if fields[4] != "true" && fields[4] != "false" {
		return Container{}, fmt.Errorf("paused is %q", fields[4])
	}
	directory := fields[2]
	if directory == "<no value>" {
		directory = ""
	}
	return Container{ID: fields[0], Name: strings.TrimPrefix(fields[1], "/"), WorkingDirectory: directory, Started: started, Paused: fields[4] == "true"}, nil
}

// Pause freezes the container.
func (d *SystemDocker) Pause(name string) error {
	if _, err := dockercli.Run(d.program, d.timeout, "", "pause", name); err != nil {
		return fmt.Errorf("heavy: docker pause %s failed: %w", name, err)
	}
	return nil
}

// Unpause thaws the container.
func (d *SystemDocker) Unpause(name string) error {
	if _, err := dockercli.Run(d.program, d.timeout, "", "unpause", name); err != nil {
		return fmt.Errorf("heavy: docker unpause %s failed: %w", name, err)
	}
	return nil
}
