package history

import (
	"fmt"
	"path/filepath"
	"sort"
)

// Recent returns the last count records, newest first, reading the newest day files only as far as needed; the
// dashboard asks for them every 2 s (design G13).
func Recent(directory string, count int) ([]Record, error) {
	paths, err := filepath.Glob(filepath.Join(directory, "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("heavy: listing %s: %w", directory, err)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	recent := []Record{}
	for _, path := range paths {
		records, err := readFile(path)
		if err != nil {
			return nil, err
		}
		for index := len(records) - 1; index >= 0 && len(recent) < count; index-- {
			recent = append(recent, records[index])
		}
		if len(recent) == count {
			break
		}
	}
	return recent, nil
}
