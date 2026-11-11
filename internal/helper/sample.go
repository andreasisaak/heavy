package helper

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ReadSample reads the memory, swap, load, processors and pressure below the /proc root of the options.
func ReadSample(options Options) (Sample, error) {
	available, err := Field(filepath.Join(options.Root, "meminfo"), "MemAvailable:", "MemAvailable: <n> kB")
	if err != nil {
		return Sample{}, err
	}
	swapped, err := Field(filepath.Join(options.Root, "vmstat"), "pswpout", "pswpout <n>")
	if err != nil {
		return Sample{}, err
	}
	load, err := readLoad(filepath.Join(options.Root, "loadavg"))
	if err != nil {
		return Sample{}, err
	}
	cpuinfo, err := read(filepath.Join(options.Root, "cpuinfo"))
	if err != nil {
		return Sample{}, err
	}
	cores := 0
	for _, line := range strings.Split(cpuinfo, "\n") {
		if name, _, found := strings.Cut(line, ":"); found && strings.TrimSpace(name) == "processor" {
			cores++
		}
	}
	pressure, err := readPressure(filepath.Join(options.Root, "pressure"))
	if err != nil {
		return Sample{}, err
	}
	return Sample{MemAvailableBytes: available * 1024, SwapOutBytes: swapped * options.PageSize, Load1: load, Cores: cores, Pressure: pressure}, nil
}

func read(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("heavy: reading %s: %w", path, err)
	}
	return string(content), nil
}

// Field is the number after the name at the start of a line of the file, as in meminfo and vmstat.
func Field(path, name, form string) (int64, error) {
	content, err := read(path)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(content, "\n") {
		words := strings.Fields(line)
		if len(words) >= 2 && words[0] == name {
			value, err := strconv.ParseInt(words[1], 10, 64)
			if err != nil {
				return 0, fmt.Errorf("heavy: %s: expected a line %s, got %q", path, form, line)
			}
			return value, nil
		}
	}
	return 0, fmt.Errorf("heavy: %s: expected a line %s, found none", path, form)
}

func readLoad(path string) (float64, error) {
	content, err := read(path)
	if err != nil {
		return 0, err
	}
	words := strings.Fields(content)
	if len(words) == 0 {
		return 0, fmt.Errorf("heavy: %s: expected the load of one minute first, got %q", path, content)
	}
	load, err := strconv.ParseFloat(words[0], 64)
	if err != nil {
		return 0, fmt.Errorf("heavy: %s: expected the load of one minute first, got %q", path, content)
	}
	return load, nil
}

// readPressure reads memory, cpu and io below the pressure folder; without the folder the kernel has no PSI.
func readPressure(folder string) (*Pressure, error) {
	if _, err := os.Stat(folder); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var pressure Pressure
	parts := []struct {
		name  string
		stall *Stall
	}{{"memory", &pressure.Memory}, {"cpu", &pressure.CPU}, {"io", &pressure.IO}}
	for _, part := range parts {
		read, err := readStall(filepath.Join(folder, part.name))
		if err != nil {
			return nil, err
		}
		*part.stall = read
	}
	return &pressure, nil
}

// readStall reads the avg10 values of the some and full lines of a pressure file.
func readStall(path string) (Stall, error) {
	content, err := read(path)
	if err != nil {
		return Stall{}, err
	}
	var stall Stall
	for _, line := range strings.Split(strings.TrimSpace(content), "\n") {
		words := strings.Fields(line)
		if len(words) < 2 || !strings.HasPrefix(words[1], "avg10=") || (words[0] != "some" && words[0] != "full") {
			return Stall{}, fmt.Errorf("heavy: %s: expected lines some|full avg10=<n> …, got %q", path, line)
		}
		value, err := strconv.ParseFloat(strings.TrimPrefix(words[1], "avg10="), 64)
		if err != nil {
			return Stall{}, fmt.Errorf("heavy: %s: expected lines some|full avg10=<n> …, got %q", path, line)
		}
		if words[0] == "some" {
			stall.Some = value
		} else {
			stall.Full = value
		}
	}
	return stall, nil
}
