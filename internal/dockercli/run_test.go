package dockercli_test

import (
	"os/exec"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/dockercli"
	"github.com/andreasisaak/heavy/internal/testkit"
)

// fake writes a docker program and starts it once, because macOS checks a new executable on its first start.
func fake(t *testing.T, script string) string {
	t.Helper()
	path := testkit.FakeProgram(t, t.TempDir(), "docker", script)
	_ = exec.Command(path, "warm-up").Run()
	return path
}

func TestRunGivesTheOutputAndPassesTheInput(t *testing.T) {
	program := fake(t, `[ "$1" = warm-up ] && exit 0; printf '%s|' "$@"; cat`)

	output, err := dockercli.Run(program, time.Second, "from stdin", "build", "-f", "-")

	if want := "build|-f|-|from stdin"; err != nil || output != want {
		t.Fatalf("Run = %q, %v; want %q", output, err, want)
	}
}

func TestRunNamesTheExitAndStderrOfAFailure(t *testing.T) {
	program := fake(t, `[ "$1" = warm-up ] && exit 0; echo "Error: No such container: db" >&2; exit 1`)

	_, err := dockercli.Run(program, time.Second, "", "pause", "db")

	if want := "exit status 1: Error: No such container: db"; err == nil || err.Error() != want {
		t.Fatalf("Run error = %v, want %s", err, want)
	}
}

func TestRunEndsACallThatHangs(t *testing.T) {
	program := fake(t, `[ "$1" = warm-up ] && exit 0; exec sleep 30`)
	begin := time.Now()

	_, err := dockercli.Run(program, 200*time.Millisecond, "", "version")

	if want := "docker version did not end within 200ms"; err == nil || err.Error() != want || time.Since(begin) > 5*time.Second {
		t.Fatalf("Run error = %v after %s, want %s", err, time.Since(begin), want)
	}
}

func TestRunOfAMissingProgramIsAnError(t *testing.T) {
	_, err := dockercli.Run("/nonexistent/docker", time.Second, "", "ps")

	if want := "fork/exec /nonexistent/docker: no such file or directory"; err == nil || err.Error() != want {
		t.Fatalf("Run error = %v, want %q", err, want)
	}
}
