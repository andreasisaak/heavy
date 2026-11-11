// Package cgroup reads the memory limits of cgroup v2 that hold for the daemon: on a Linux box each user slice may
// carry MemoryHigh and MemoryMax, which the free memory of the machine does not show.
package cgroup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Headroom is the memory the cgroup of this process may still take: the smallest difference between a limit
// (memory.high or memory.max) and the memory in use over every level from its own group up to the root, at least 0.
// In use is memory.current without the file cache (inactive_file and active_file of memory.stat): the kernel gives
// the cache back before a limit holds anything up, and MemAvailable of the machine counts it as available too.
// limited is false when no level sets a limit, or the process is in no cgroup v2 group.
func Headroom(procRoot, cgroupRoot string) (headroom int64, limited bool, err error) {
	path := filepath.Join(procRoot, "self", "cgroup")
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, false, fmt.Errorf("heavy: reading %s: %w", path, err)
	}
	group, found := "", false
	for _, line := range strings.Split(string(content), "\n") {
		if rest, isUnified := strings.CutPrefix(line, "0::"); isUnified {
			group, found = filepath.Clean("/"+rest), true
		}
	}
	if !found {
		return 0, false, nil
	}
	for ; group != "/"; group = filepath.Dir(group) {
		folder := filepath.Join(cgroupRoot, group)
		limit, set, err := lowestLimit(folder)
		if err != nil {
			return 0, false, err
		}
		if !set {
			continue
		}
		current, _, err := readBytes(filepath.Join(folder, "memory.current"), false)
		if err != nil {
			return 0, false, err
		}
		cache, err := fileCache(filepath.Join(folder, "memory.stat"))
		if err != nil {
			return 0, false, err
		}
		if left := max(0, limit-max(0, current-cache)); !limited || left < headroom {
			headroom, limited = left, true
		}
	}
	return headroom, limited, nil
}

// lowestLimit is the smaller of memory.high and memory.max of a group; set is false when both say max or the group
// has no memory controller.
func lowestLimit(folder string) (limit int64, set bool, err error) {
	for _, name := range []string{"memory.high", "memory.max"} {
		value, limited, err := readBytes(filepath.Join(folder, name), true)
		if err != nil {
			return 0, false, err
		}
		if limited && (!set || value < limit) {
			limit, set = value, true
		}
	}
	return limit, set, nil
}

// fileCache sums inactive_file and active_file of a memory.stat; a group without the file has none.
func fileCache(path string) (int64, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("heavy: reading %s: %w", path, err)
	}
	var cache int64
	for _, line := range strings.Split(string(content), "\n") {
		words := strings.Fields(line)
		if len(words) != 2 || (words[0] != "inactive_file" && words[0] != "active_file") {
			continue
		}
		value, err := strconv.ParseInt(words[1], 10, 64)
		if err != nil || value < 0 {
			return 0, fmt.Errorf("heavy: %s: expected %s <bytes>, got %q", path, words[0], line)
		}
		cache += value
	}
	return cache, nil
}

// readBytes reads a file with a number of bytes. An optional file may be missing or say max; set is false then.
func readBytes(path string, optional bool) (value int64, set bool, err error) {
	content, err := os.ReadFile(path)
	if optional && errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("heavy: reading %s: %w", path, err)
	}
	text := strings.TrimSpace(string(content))
	if optional && text == "max" {
		return 0, false, nil
	}
	value, err = strconv.ParseInt(text, 10, 64)
	if err != nil || value < 0 {
		form := "bytes"
		if optional {
			form = "bytes or max"
		}
		return 0, false, fmt.Errorf("heavy: %s: expected %s, got %q", path, form, content)
	}
	return value, true, nil
}
