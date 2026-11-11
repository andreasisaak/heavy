// Package testkit holds the helpers the black-box tests share: the heavy binary
// built from this module, fake programs, and temporary state and home directories.
package testkit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var (
	buildOnce   sync.Once
	builtBinary string
	buildError  error
)

// BuildBinary builds cmd/heavy once per test package, starts it once and returns the path of the binary.
// The binary lands next to the test executable, in the work directory that go test removes
// after the run.
func BuildBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		builtBinary, buildError = build()
	})
	if buildError != nil {
		t.Fatalf("building heavy: %v", buildError)
	}
	return builtBinary
}

func build() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("finding the test executable: %w", err)
	}
	directory := filepath.Join(filepath.Dir(executable), "heavy-binary")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", directory, err)
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("found no go on PATH: %w", err)
	}
	binary := filepath.Join(directory, "heavy")
	command := exec.Command(goTool, "build", "-o", binary, "github.com/andreasisaak/heavy/cmd/heavy")
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("go build -o %s failed: %w\n%s", binary, err, output)
	}
	// macOS checks a new executable on its first start, which takes some hundred milliseconds on an idle Mac
	// (probe 2026-11-11: 0.3 to 0.5 s against 4 ms for the second start) and more beside other test packages. The
	// first start happens here, so no test pays for it inside one of its short timeouts. The helper without input
	// reads nothing and ends at once.
	if output, err := exec.Command(binary, "--helper", "--root", directory).CombinedOutput(); err != nil {
		return "", fmt.Errorf("the first start of %s failed: %w\n%s", binary, err, output)
	}
	return binary, nil
}
