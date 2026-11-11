package guard_test

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// test_heavy_guard.py:190
func TestOtherCommandsPassUnchanged(t *testing.T) {
	_, worktree := listed(t)
	for _, command := range []string{
		"make lint",
		"make test-complete",
		"echo make ci",
		"git commit -m 'make ci'",
		`git commit -m "fix \` + "`make ci\\`" + ` timeout"`,
		"gh pr create --title fix --body 'runs `make ci` now'",
		"echo '$(make ci)'",
	} {
		t.Run(command, func(t *testing.T) {
			check(t, "answer", guardOf(t, command, worktree, false), nil)
		})
	}
}

// test_heavy_guard.py:208
func TestProjectsWithoutAListPassUnchanged(t *testing.T) {
	listed(t)
	directory := temporaryDirectory(t)
	other := filepath.Join(directory, "other")
	git(t, "init", "-q", other)

	check(t, "answer in a checkout without a list", guardOf(t, "make ci", other, false), nil)
	check(t, "answer outside a checkout", guardOf(t, "make ci", directory, false), nil)
}

// test_heavy_guard.py:217
func TestOtherToolsPassUnchanged(t *testing.T) {
	_, worktree := listed(t)
	input, err := json.Marshal(map[string]any{"tool_name": "Read", "cwd": worktree, "tool_input": map[string]string{"file_path": "make ci"}})
	if err != nil {
		t.Fatal(err)
	}

	check(t, "output", newWorld(t).raw(t, string(input)), "")
}

// test_heavy_guard.py:530
func TestLightCallOfARunnerPasses(t *testing.T) {
	_, worktree := listed(t)
	for _, command := range []string{
		"docker compose exec -T web pnpm exec vitest run tests/nuxt/cart.spec.ts",
		"pnpm exec vitest watch",
		"docker compose run --rm --build renderer-test",
	} {
		t.Run(command, func(t *testing.T) {
			check(t, "answer", guardOf(t, command, worktree, false), nil)
		})
	}
}
