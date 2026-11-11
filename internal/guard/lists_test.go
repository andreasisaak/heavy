package guard_test

import (
	"testing"

	"github.com/andreasisaak/heavy/internal/shell"
)

// test_heavy_guard.py:424
func TestListInTheWorktreeQueuesItsCommand(t *testing.T) {
	_, worktree := checkouts(t)
	projectList(t, worktree, "make ci")

	check(t, "updated input", updated(guardOf(t, "make ci", worktree, false)), rewritten(queued(fakeHeavy+" make ci")))
}

// test_heavy_guard.py:431
func TestListOfTheMainCheckoutDoesNotReachAWorktreeWithoutOne(t *testing.T) {
	main, worktree := checkouts(t)
	projectList(t, main, "make ci")

	check(t, "answer", guardOf(t, "make ci", worktree, false), nil)
}

// test_heavy_guard.py:438
func TestPersonalCommandsApplyToEveryWorktree(t *testing.T) {
	main, worktree := checkouts(t)
	current := newWorld(t).withPersonal(t, `{"projects": {"`+main+`": {"enabled": true, "commands": ["make check"]}}}`)

	check(t, "updated input in the main checkout", updated(current.run(t, bash("make check", main, false))), rewritten(queued(fakeHeavy+" make check")))
	check(t, "updated input in the worktree", updated(current.run(t, bash("make check", worktree, false))), rewritten(queued(fakeHeavy+" make check")))
}

// test_heavy_guard.py:446
func TestPersonalSwitchTurnsTheProjectListOff(t *testing.T) {
	main, worktree := checkouts(t)
	projectList(t, worktree, "make ci")
	current := newWorld(t).withPersonal(t, `{"projects": {"`+main+`": {"enabled": false, "commands": []}}}`)

	check(t, "answer", current.run(t, bash("make ci", worktree, false)), nil)
}

// test_heavy_guard.py:453
func TestPersonalCommandsAddToTheProjectList(t *testing.T) {
	main, worktree := checkouts(t)
	projectList(t, worktree, "make ci")
	current := newWorld(t).withPersonal(t, `{"projects": {"`+main+`": {"enabled": true, "commands": ["make e2e"]}}}`)

	got := updated(current.run(t, bash("make ci && make e2e", worktree, false)))

	check(t, "updated input", got, rewritten(queued(fakeHeavy+" "+wholeShell+" "+shell.Quote("make ci && make e2e"))))
}

// test_heavy_guard.py:463
func TestSingleWordEntryMatchesTheProgramOnly(t *testing.T) {
	_, worktree := checkouts(t)
	projectList(t, worktree, "smoke")

	check(t, "answer of uv run smoke", guardOf(t, "uv run smoke", worktree, false), nil)
	check(t, "updated input of smoke -x", updated(guardOf(t, "smoke -x", worktree, false)), rewritten(queued(fakeHeavy+" smoke -x")))
}

// test_heavy_guard.py:471
func TestCatalogueQueuesARunnerBehindALauncherTheEntryMisses(t *testing.T) {
	_, worktree := checkouts(t)
	projectList(t, worktree, "pytest")

	check(t, "updated input", updated(guardOf(t, "uv run pytest", worktree, false)), rewritten(queued(fakeHeavy+" uv run pytest")))
}

// test_heavy_guard.py:478
func TestScriptEntryMatchesEveryWayToStartTheScript(t *testing.T) {
	for _, entry := range []string{"infra-test.sh", "tools/ci/infra-test.sh"} {
		for _, command := range []string{
			"tools/ci/infra-test.sh",
			"./tools/ci/infra-test.sh --full",
			"bash tools/ci/infra-test.sh",
			"sh -x tools/ci/infra-test.sh --full",
		} {
			t.Run(entry+" "+command, func(t *testing.T) {
				_, worktree := checkouts(t)
				projectList(t, worktree, entry)

				check(t, "updated input", updated(guardOf(t, command, worktree, false)), rewritten(queued(fakeHeavy+" "+command)))
			})
		}
	}
}

// test_heavy_guard.py:495
func TestScriptInsideAShellStringIsQueued(t *testing.T) {
	_, worktree := checkouts(t)
	projectList(t, worktree, "infra-test.sh")

	got := updated(guardOf(t, "bash -c 'tools/ci/infra-test.sh'", worktree, false))

	check(t, "updated input", got, rewritten(queued(fakeHeavy+" bash -c 'tools/ci/infra-test.sh'")))
}

// test_heavy_guard.py:504
func TestRunnerOfTheCatalogueIsQueuedWhereHeavyIsOn(t *testing.T) {
	_, worktree := listed(t)
	for _, command := range []string{
		"docker compose exec -T web pnpm exec vitest run --project unit",
		"docker compose exec -T web sh -c 'cd /app/apps/web && pnpm exec vitest run --project storybook'",
		"docker compose exec -T api uv run pytest",
		"docker compose exec -T app php artisan test --parallel",
		"docker compose exec -T web pnpm typecheck",
		"npx vitest run",
	} {
		t.Run(command, func(t *testing.T) {
			check(t, "updated input", updated(guardOf(t, command, worktree, false)), rewritten(queued(fakeHeavy+" "+command)))
		})
	}
}

// test_heavy_guard.py:521
func TestRunnerInAContainerNamesTheProjectOfTheContainer(t *testing.T) {
	_, worktree := listed(t)
	command := "docker exec -w /app/apps/web main-web-1 sh -c 'pnpm exec vitest run --project unit 2>&1 | tail'"
	current := newWorld(t)
	current.containers = map[string]string{"main-web-1": worktree}

	got := updated(current.run(t, bash(command, temporaryDirectory(t), false)))

	check(t, "updated input", got, rewritten(queued(fakeHeavy+" --directory "+worktree+" "+command)))
}
