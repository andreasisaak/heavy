// Package history keeps one record per ended run (design G11) in $STATE/history/YYYY-MM-DD.jsonl, by the local
// date of the end, and predicts the memory peak of a run from the records of its label and checkout.
package history

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Record is one ended run.
type Record struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	Command       string `json:"command"`
	Checkout      string `json:"checkout"`
	Directory     string `json:"directory"`
	Session       string `json:"session"`
	Agent         string `json:"agent"`
	Since         int64  `json:"since"`
	Started       int64  `json:"started"`
	Ended         int64  `json:"ended"`
	ExitStatus    int    `json:"exit_status"`
	Reason        string `json:"reason"`
	PeakRSSBytes  int64  `json:"peak_rss_bytes"`
	Budget        int    `json:"budget"`
	PausedSeconds int64  `json:"paused_seconds"`
	Restarts      int    `json:"restarts"`
}

// recordFields counts the fields a record line must have.
const recordFields = 16

// maxRecord is the longest record line a history file holds, as long as the longest protocol line.
const maxRecord = 4 << 20

// Append adds a record to the file of the local date it ended.
func Append(directory string, record Record) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("heavy: creating the history folder %s: %w", directory, err)
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("heavy: encoding the history record of %s: %w", record.ID, err)
	}
	path := filepath.Join(directory, time.Unix(record.Ended, 0).Local().Format("2006-01-02")+".jsonl")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("heavy: opening %s: %w", path, err)
	}
	defer file.Close()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("heavy: writing %s: %w", path, err)
	}
	return nil
}

// Read returns the records of every history file, file by file in date order.
func Read(directory string) ([]Record, error) {
	paths, err := filepath.Glob(filepath.Join(directory, "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("heavy: listing %s: %w", directory, err)
	}
	sort.Strings(paths)
	var records []Record
	for _, path := range paths {
		found, err := readFile(path)
		if err != nil {
			return nil, err
		}
		records = append(records, found...)
	}
	return records, nil
}

func readFile(path string) ([]Record, error) {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("heavy: opening %s: %w", path, err)
	}
	defer file.Close()
	var records []Record
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), maxRecord)
	for number := 1; scanner.Scan(); number++ {
		current, err := decode(scanner.Bytes())
		if err != nil {
			return nil, fmt.Errorf("heavy: %s:%d: %w", path, number, err)
		}
		records = append(records, current)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("heavy: reading %s: %w", path, err)
	}
	return records, nil
}

// decode reads one line strictly: every field of a record and no other.
func decode(line []byte) (Record, error) {
	var current Record
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&current); err != nil {
		return Record{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(line, &fields); err != nil || len(fields) != recordFields {
		return Record{}, fmt.Errorf("expected a history record with every field of design G11, got %s", line)
	}
	return current, nil
}
