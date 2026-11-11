// Package procfs reads the per-process files of a Linux /proc that more than one package needs.
package procfs

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Stat is /proc/<pid>/stat: the name of the program as the kernel keeps it, and the fields after it, so Fields[0]
// is the state (field 3 of proc(5)), Fields[1] the parent and Fields[2] the process group. Raw is the whole file,
// for the error message of a caller that misses a field; a file without a name in parentheses has no fields.
type Stat struct {
	Path   string
	Raw    string
	Name   string
	Fields []string
}

// ReadStat reads the stat file of a process. The name may hold spaces and parentheses, so it ends at the last
// closing parenthesis. A process that ended gives the error of os.ReadFile, fs.ErrNotExist.
func ReadStat(root string, pid int) (Stat, error) {
	path := filepath.Join(root, strconv.Itoa(pid), "stat")
	content, err := os.ReadFile(path)
	if err != nil {
		return Stat{}, err
	}
	stat := Stat{Path: path, Raw: string(content)}
	open, end := strings.IndexByte(stat.Raw, '('), strings.LastIndexByte(stat.Raw, ')')
	if open >= 0 && end > open {
		stat.Name, stat.Fields = stat.Raw[open+1:end], strings.Fields(stat.Raw[end+1:])
	}
	return stat, nil
}
