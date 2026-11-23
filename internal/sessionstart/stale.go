package sessionstart

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/andreasisaak/heavy/internal/current"
	"github.com/andreasisaak/heavy/internal/detect"
	"github.com/andreasisaak/heavy/internal/shell"
)

// The line with which every heavy puts a shims folder in front of PATH is pathStart, the quoted folder, pathMiddle,
// the quoted folder and pathEnd.
const (
	pathStart  = `case "$PATH" in `
	pathMiddle = `:*) ;; *) export PATH=`
	pathEnd    = `:"$PATH" ;; esac`
	// retiredStart replaces pathStart and makes the line a comment. It is as long as pathStart, so the file keeps
	// its length.
	retiredStart = `# heavy, stale: `
)

// retireEarlierShims takes the shims folders of earlier heavy versions off the PATH of the session and reports
// whether every env file could be cleaned. `claude --resume` applies the env files of all earlier session starts
// again, and the number in the name of a file changes with the hooks of the installed plugins, so the line of an
// earlier heavy stays in a file next to CLAUDE_ENV_FILE, which may hold the lines of another plugin by now. Other
// hooks of this session start append to these files while the hook runs: it therefore overwrites the start of such
// a line in place and never rewrites a file.
func retireEarlierShims(deps Dependencies, stderr io.Writer) bool {
	if deps.EnvFile == "" {
		return true
	}
	directory := filepath.Dir(deps.EnvFile)
	entries, err := os.ReadDir(directory)
	if err != nil {
		fmt.Fprintf(stderr, "heavy: reading the env files of the session in %s: %v\n", directory, err)
		return false
	}
	cleaned := true
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".sh") {
			continue
		}
		file := filepath.Join(directory, entry.Name())
		folders, err := retireIn(file, current.Shims(deps.StateDirectory))
		if err != nil {
			fmt.Fprintf(stderr, "heavy: taking the shims of an earlier heavy off PATH in %s: %v\n", file, err)
			cleaned = false
		}
		for _, folder := range folders {
			fmt.Fprintf(stderr, "heavy: took the shims of an earlier heavy off PATH: %s in %s\n", folder, file)
		}
	}
	return cleaned
}

// retireIn makes every line of the file a comment that puts a shims folder of heavy other than kept on PATH, and
// returns those folders.
func retireIn(file, kept string) ([]string, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var folders []string
	var offsets []int64
	offset := 0
	for _, line := range strings.SplitAfter(string(content), "\n") {
		folder, found := folderOfPathLine(strings.TrimSuffix(line, "\n"))
		if found && folder != kept && detect.ShimFolder(folder) {
			folders, offsets = append(folders, folder), append(offsets, int64(offset))
		}
		offset += len(line)
	}
	if len(offsets) == 0 {
		return nil, nil
	}
	handle, err := os.OpenFile(file, os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	for _, at := range offsets {
		if _, err := handle.WriteAt([]byte(retiredStart), at); err != nil {
			handle.Close()
			return nil, err
		}
	}
	if err := handle.Close(); err != nil {
		return nil, err
	}
	return folders, nil
}

// folderOfPathLine returns the folder that a line puts in front of PATH; the line must be exactly what heavy writes.
func folderOfPathLine(line string) (string, bool) {
	rest, found := strings.CutPrefix(line, pathStart)
	if !found {
		return "", false
	}
	quoted, second, found := strings.Cut(rest, pathMiddle)
	if !found || second != quoted+pathEnd {
		return "", false
	}
	folder := quoted
	if strings.HasPrefix(quoted, "'") && strings.HasSuffix(quoted, "'") && len(quoted) >= 2 {
		folder = strings.ReplaceAll(quoted[1:len(quoted)-1], `'"'"'`, "'")
	}
	return folder, shell.Quote(folder) == quoted
}
