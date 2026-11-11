package helper

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// containerID is a container id of docker in a cgroup path, such as /docker/<id> or docker-<id>.scope.
var containerID = regexp.MustCompile(`[0-9a-f]{64}`)

// vanished reports an error of a process that ended while the helper read it.
func vanished(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH)
}

// pids lists the processes below root by PID.
func pids(root string) ([]int, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("heavy: reading %s: %w", root, err)
	}
	var found []int
	for _, entry := range entries {
		if pid, err := strconv.Atoi(entry.Name()); err == nil && entry.IsDir() {
			found = append(found, pid)
		}
	}
	sort.Ints(found)
	return found, nil
}

// tagged reports whether the environment of the process holds HEAVY_RUN=<tag>.
func tagged(root string, pid int, tag string) (bool, error) {
	content, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "environ"))
	if err != nil {
		return false, err
	}
	for _, entry := range strings.Split(string(content), "\x00") {
		if entry == "HEAVY_RUN="+tag {
			return true, nil
		}
	}
	return false, nil
}

// describe reads the resident memory and the container of a process.
func describe(root string, pid int) (Process, error) {
	folder := filepath.Join(root, strconv.Itoa(pid))
	status, err := os.ReadFile(filepath.Join(folder, "status"))
	if err != nil {
		return Process{}, err
	}
	process := Process{PID: pid}
	for _, line := range strings.Split(string(status), "\n") {
		words := strings.Fields(line)
		if len(words) == 3 && words[0] == "VmRSS:" && words[2] == "kB" {
			kilobytes, err := strconv.ParseInt(words[1], 10, 64)
			if err != nil {
				return Process{}, fmt.Errorf("heavy: %s: expected VmRSS: <n> kB, got %q", filepath.Join(folder, "status"), line)
			}
			process.RSSBytes = kilobytes * 1024
		}
	}
	cgroup, err := os.ReadFile(filepath.Join(folder, "cgroup"))
	if err != nil {
		return Process{}, err
	}
	if ids := containerID.FindAllString(string(cgroup), -1); len(ids) > 0 {
		process.Container = ids[len(ids)-1]
	}
	return process, nil
}

// foreign reports an error of a process this user may not read: the process of another user.
func foreign(err error) bool {
	return errors.Is(err, fs.ErrPermission)
}

// Find lists the processes that carry the tag, by PID. Unprivileged, it skips the processes of other users, which
// cannot belong to a run of this one, and those outside containers, which the process group of the run covers.
func Find(options Options, tag string) ([]Process, error) {
	root := options.Root
	all, err := pids(root)
	if err != nil {
		return nil, err
	}
	found := []Process{}
	for _, pid := range all {
		carries, err := tagged(root, pid, tag)
		if err == nil && carries {
			var process Process
			process, err = describe(root, pid)
			if err == nil && (!options.Unprivileged || process.Container != "") {
				found = append(found, process)
			}
		}
		if options.Unprivileged && foreign(err) {
			continue
		}
		if err != nil && !vanished(err) {
			return nil, fmt.Errorf("heavy: reading process %d: %w", pid, err)
		}
	}
	return found, nil
}

// ContainerUsage sums the resident memory of the processes per container, by container id. Unprivileged, it skips
// the processes this user may not read.
func ContainerUsage(options Options) ([]Container, error) {
	root := options.Root
	all, err := pids(root)
	if err != nil {
		return nil, err
	}
	sums := map[string]int64{}
	for _, pid := range all {
		process, err := describe(root, pid)
		if vanished(err) || (options.Unprivileged && foreign(err)) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("heavy: reading process %d: %w", pid, err)
		}
		if process.Container != "" {
			sums[process.Container] += process.RSSBytes
		}
	}
	containers := []Container{}
	for id, bytes := range sums {
		containers = append(containers, Container{ID: id, RSSBytes: bytes})
	}
	sort.Slice(containers, func(left, right int) bool { return containers[left].ID < containers[right].ID })
	return containers, nil
}
