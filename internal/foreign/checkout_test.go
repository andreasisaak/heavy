package foreign_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/andreasisaak/heavy/internal/foreign"
)

func TestMainCheckoutOfAWorktreeIsItsMainCheckout(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving the temporary directory: %v", err)
	}
	main := filepath.Join(directory, "shopfront")
	git := func(arguments ...string) {
		command := exec.Command("git", append([]string{"-c", "user.name=Example", "-c", "user.email=example@example.com"}, arguments...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %q: %v\n%s", arguments, err, output)
		}
	}
	git("init", "-q", main)
	git("-C", main, "commit", "-q", "--allow-empty", "-m", "init")
	git("-C", main, "worktree", "add", "-q", "-b", "csv-export", main+"-csv-export")
	compose := filepath.Join(main+"-csv-export", "docker")
	if err := os.Mkdir(compose, 0o755); err != nil {
		t.Fatalf("creating %s: %v", compose, err)
	}

	got, err := foreign.MainCheckout(compose)

	if err != nil || got != main {
		t.Fatalf("MainCheckout(%s) = %q, %v; want %q", compose, got, err, main)
	}
}

func TestMainCheckoutOfADirectoryInNoCheckoutIsEmpty(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "removed-worktree")

	got, err := foreign.MainCheckout(directory)

	if err != nil || got != "" {
		t.Fatalf("MainCheckout(%s) = %q, %v; want \"\"", directory, got, err)
	}
}
