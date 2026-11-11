package catalogue_test

import (
	"testing"

	"github.com/andreasisaak/heavy/internal/catalogue"
	"github.com/andreasisaak/heavy/internal/shell"
)

// runnerCases hold a call and the runner it starts, "" for a light call.
var runnerCases = []struct {
	origin  string
	command string
	want    string
}{
	// test_heavy_call_names_its_runner
	{"test_heavy_runners.py:15", "docker compose exec -T web pnpm exec vitest run --project unit", "vitest"},
	{"test_heavy_runners.py:16", "docker exec -w /app/apps/web analytics-web-1 sh -c 'pnpm exec vitest run --project unit 2>&1 | tail'", "vitest"},
	{"test_heavy_runners.py:17", "docker compose exec -T web sh -c 'cd /app/apps/web && pnpm exec vitest run --project storybook'", "vitest"},
	{"test_heavy_runners.py:18", "docker compose -f a.yml --project-directory /x run --rm --no-deps -T -w /app/apps/web web pnpm exec vitest run", "vitest"},
	{"test_heavy_runners.py:19", "docker compose exec -T api uv run pytest", "pytest"},
	{"test_heavy_runners.py:20", "docker compose exec -T web pnpm typecheck", "pnpm typecheck"},
	{"test_heavy_runners.py:21", "docker compose exec -T app php artisan test --parallel", "artisan test"},
	{"test_heavy_runners.py:22", "docker compose exec -T app vendor/bin/paratest --processes=4", "paratest"},
	{"test_heavy_runners.py:23", "docker compose exec -T -e XDEBUG_MODE=coverage app vendor/bin/phpunit --group second-server", "phpunit"},
	{"test_heavy_runners.py:24", "docker compose run --rm frontend pnpm exec vitest run --project unit", "vitest"},
	{"test_heavy_runners.py:25", "docker compose exec -T web pnpm exec vitest run --project unit tests/a.spec.ts tests/b.spec.ts tests/c.spec.ts tests/d.spec.ts", "vitest"},
	{"test_heavy_runners.py:27", "npx vitest run", "vitest"},
	{"test_heavy_runners.py:29", "pnpm exec vitest", "vitest"},
	{"test_heavy_runners.py:30", "npx --yes vitest run", "vitest"},
	{"test_heavy_runners.py:31", "pnpm vitest run", "vitest"},
	{"test_heavy_runners.py:32", "pnpm --filter web exec vitest run", "vitest"},
	{"test_heavy_runners.py:33", "pnpm -C apps/web test", "pnpm test"},
	{"test_heavy_runners.py:34", "pnpm run build", "pnpm build"},
	{"test_heavy_runners.py:35", "pnpm build", "pnpm build"},
	{"test_heavy_runners.py:36", "pnpm run test:e2e", "pnpm test:e2e"},
	{"test_heavy_runners.py:37", "npm test", "npm test"},
	{"test_heavy_runners.py:38", "npm run lint", "npm lint"},
	{"test_heavy_runners.py:39", "npm exec -- jest", "jest"},
	{"test_heavy_runners.py:40", "yarn test", "yarn test"},
	{"test_heavy_runners.py:41", "yarn workspace web build", "yarn build"},
	{"test_heavy_runners.py:42", "bun test", "bun test"},
	{"test_heavy_runners.py:43", "bun run typecheck", "bun typecheck"},
	{"test_heavy_runners.py:44", "bunx playwright test", "playwright"},
	{"test_heavy_runners.py:45", "uv run --with pytest --no-project pytest -q", "pytest"},
	{"test_heavy_runners.py:46", "uv run -m pytest", "pytest"},
	{"test_heavy_runners.py:47", "uvx mypy app", "mypy"},
	{"test_heavy_runners.py:48", "python3 -m pytest -x", "pytest"},
	{"test_heavy_runners.py:49", "poetry run pytest", "pytest"},
	{"test_heavy_runners.py:50", "php artisan test", "artisan test"},
	{"test_heavy_runners.py:51", "php -d memory_limit=1G vendor/bin/phpstan analyse", "phpstan"},
	{"test_heavy_runners.py:52", "node node_modules/vitest/vitest.mjs run", "vitest"},
	{"test_heavy_runners.py:53", "composer test", "composer test"},
	{"test_heavy_runners.py:54", "composer run-script phpstan", "composer phpstan"},
	{"test_heavy_runners.py:55", "ddev composer test", "composer test"},
	{"test_heavy_runners.py:56", "ddev exec vendor/bin/phpunit", "phpunit"},
	{"test_heavy_runners.py:57", "FULL=1 nice -n 10 timeout 900 pnpm test", "pnpm test"},
	{"test_heavy_runners.py:58", "bash -c 'if pnpm exec vitest run; then echo green; fi'", "vitest"},
	{"test_heavy_runners.py:60", "vendor/bin/phpstan analyse --memory-limit=1G --no-progress", "phpstan"},
	{"test_heavy_runners.py:61", "vendor/bin/deptrac analyse --no-progress", "deptrac"},
	{"test_heavy_runners.py:62", "pnpm exec vue-tsc --noEmit", "vue-tsc"},
	{"test_heavy_runners.py:63", "pnpm exec eslint .", "eslint"},
	{"test_heavy_runners.py:64", "cargo test --release", "cargo test"},
	{"test_heavy_runners.py:65", "cargo +nightly clippy", "cargo clippy"},
	{"test_heavy_runners.py:66", "go test ./...", "go test"},
	{"test_heavy_runners.py:67", "pnpm exec nuxi typecheck", "nuxi typecheck"},
	{"test_heavy_runners.py:68", "pnpm exec vite build", "vite build"},
	{"test_heavy_runners.py:69", "pnpm exec playwright test", "playwright"},
	{"test_heavy_runners.py:70", "pytest tests/", "pytest"},
	{"test_heavy_runners.py:71", "pytest tests/test_cart.py tests/", "pytest"},
	{"test_heavy_runners.py:72", "vitest run --config vitest.config.ts", "vitest"},
	{"test_heavy_runners.py:74", "vitest run --setupFiles tests/setup.ts", "vitest"},
	{"test_heavy_runners.py:75", "pnpm exec vitest run --globalSetup tests/global.ts", "vitest"},
	{"test_heavy_runners.py:76", "artisan test --filter=CartTest", "artisan test"},
	// test_light_call_has_no_runner
	{"test_heavy_runners.py:87", "docker compose exec -T web pnpm exec vitest run --project unit tests/nuxt/a.spec.ts tests/nuxt/b.spec.ts", ""},
	{"test_heavy_runners.py:88", "docker compose exec -T app php artisan test tests/Feature/CartTest.php", ""},
	{"test_heavy_runners.py:89", "pytest -x tests/test_cart.py::test_total", ""},
	{"test_heavy_runners.py:90", "ddev exec vendor/bin/phpstan analyse --no-progress --level=8 src/Migration/Migration1.php", ""},
	{"test_heavy_runners.py:91", "npx --no-update-notifier eslint server/utils/renderer.ts server/routes/pages.get.ts", ""},
	{"test_heavy_runners.py:92", "pnpm test -- tests/cart.spec.ts", ""},
	{"test_heavy_runners.py:93", "vitest run --reporter=dot tests/cart.spec.ts", ""},
	{"test_heavy_runners.py:95", "pnpm exec vitest watch", ""},
	{"test_heavy_runners.py:96", "pnpm exec vitest --watch", ""},
	{"test_heavy_runners.py:97", "pnpm exec vitest -w", ""},
	{"test_heavy_runners.py:98", "pnpm exec vitest --ui", ""},
	{"test_heavy_runners.py:99", "pnpm exec vue-tsc --noEmit --watch", ""},
	{"test_heavy_runners.py:100", "pnpm test --watch", ""},
	{"test_heavy_runners.py:101", "pnpm run test:watch", ""},
	{"test_heavy_runners.py:102", "pnpm test:ui", ""},
	{"test_heavy_runners.py:103", "npm run build:watch", ""},
	{"test_heavy_runners.py:104", "yarn e2e:open", ""},
	{"test_heavy_runners.py:105", "pnpm run test-serve", ""},
	{"test_heavy_runners.py:106", "pnpm exec playwright test --ui", ""},
	{"test_heavy_runners.py:108", "pnpm exec vitest --version", ""},
	{"test_heavy_runners.py:109", "pnpm exec playwright install", ""},
	{"test_heavy_runners.py:110", "vendor/bin/phpstan clear-result-cache", ""},
	{"test_heavy_runners.py:111", "pnpm install", ""},
	{"test_heavy_runners.py:112", "npm ci", ""},
	{"test_heavy_runners.py:113", "npm run dev", ""},
	{"test_heavy_runners.py:114", "pnpm storybook", ""},
	{"test_heavy_runners.py:115", "composer install", ""},
	{"test_heavy_runners.py:116", "composer dump-autoload", ""},
	{"test_heavy_runners.py:117", "cargo fmt", ""},
	{"test_heavy_runners.py:118", "docker compose up -d --wait --build renderer", ""},
	{"test_heavy_runners.py:119", "docker compose run --rm --build renderer-test", ""},
	{"test_heavy_runners.py:120", "docker compose exec -T web cat vitest.config.ts", ""},
	{"test_heavy_runners.py:121", "docker compose logs -f web", ""},
	{"test_heavy_runners.py:122", "docker ps", ""},
	{"test_heavy_runners.py:123", "grep -rn vitest apps/web", ""},
	{"test_heavy_runners.py:124", "git commit -m 'run vitest'", ""},
	{"test_heavy_runners.py:125", "php -l app/Example.php", ""},
	{"test_heavy_runners.py:126", "php tools/ci/coverage-report.php .build/coverage --min=99", ""},
	{"test_heavy_runners.py:127", "python3 tools/prepare_snapshot.py", ""},
	{"test_heavy_runners.py:128", "node tools/check-tokens.ts", ""},
	{"test_heavy_runners.py:129", "bun run tools/x.ts", ""},
	{"test_heavy_runners.py:130", "make test", ""},
	{"test_heavy_runners.py:131", "heavy docker compose exec web pnpm exec vitest run", ""},
	{"test_heavy_runners.py:132", "echo pytest", ""},
	{"test_heavy_runners.py:133", "", ""},
	// test_linter_given_a_list_of_files_stays_light_at_any_length
	{"test_heavy_runners.py:143", "pnpm exec eslint src/portal/file0.ts src/portal/file1.ts src/portal/file2.ts src/portal/file3.ts src/portal/file4.ts src/portal/file5.ts src/portal/file6.ts src/portal/file7.ts src/portal/file8.ts src/portal/file9.ts src/portal/file10.ts src/portal/file11.ts src/portal/file12.ts src/portal/file13.ts src/portal/file14.ts src/portal/file15.ts src/portal/file16.ts src/portal/file17.ts src/portal/file18.ts src/portal/file19.ts src/portal/file20.ts src/portal/file21.ts src/portal/file22.ts src/portal/file23.ts src/portal/file24.ts", ""},
	{"test_heavy_runners.py:144", "pnpm exec eslint --fix --cache src/a.ts src/b.vue src/c.ts src/d.ts", ""},
	{"test_heavy_runners.py:145", "npx eslint -f json --max-warnings 0 src/a.ts src/b.ts src/c.ts src/d.ts", ""},
	{"test_heavy_runners.py:146", "eslint -c eslint.config.js src/a.ts src/b.ts src/c.ts src/d.ts", ""},
	{"test_heavy_runners.py:147", "stylelint --fix src/a.css src/b.scss src/c.css src/d.scss", ""},
	// test_linter_given_a_directory_a_glob_or_nothing_checks_a_whole_tree
	{"test_heavy_runners.py:157", "pnpm exec eslint .", "eslint"},
	{"test_heavy_runners.py:158", "pnpm exec eslint src", "eslint"},
	{"test_heavy_runners.py:159", "pnpm exec eslint --fix .", "eslint"},
	{"test_heavy_runners.py:160", "pnpm exec eslint 'src/**/*.ts'", "eslint"},
	{"test_heavy_runners.py:161", "pnpm exec eslint", "eslint"},
	{"test_heavy_runners.py:162", "pnpm exec eslint src/a.ts src/lists", "eslint"},
	{"test_heavy_runners.py:163", "stylelint '**/*.scss'", "stylelint"},
	// test_phpstan_flag_in_front_of_a_file_leaves_the_file_a_target
	{"test_heavy_runners.py:174", "docker compose exec app vendor/bin/phpstan analyse --memory-limit=1G --no-progress app/Modules/TravelAgents/Errors/TravelAgentAnonymized.php", ""},
	{"test_heavy_runners.py:175", "vendor/bin/phpstan analyse --debug --xdebug app/Example.php", ""},
	{"test_heavy_runners.py:176", "vendor/bin/phpstan analyse --error-format table --no-progress app/Example.php app/Other.php", ""},
	// test_phpstan_option_value_is_no_target
	{"test_heavy_runners.py:186", "vendor/bin/phpstan analyse --no-progress -a tools/autoload.php", "phpstan"},
	{"test_heavy_runners.py:187", "vendor/bin/phpstan analyse --autoload-file tools/autoload.php", "phpstan"},
	{"test_heavy_runners.py:188", "vendor/bin/phpstan analyse --tmp-file /tmp/edited.php --instead-of app/Example.php", "phpstan"},
	// infection runs the whole suite with mutants, whatever it names (FR-5)
	{"plan 1.3", "infection run --threads=3", "infection"},
	{"plan 1.3", "vendor/bin/infection --filter=src/A.php", "infection"},
	{"plan 1.3", "infection --help", ""},
	{"plan 1.3", "infection -h", ""},
	{"plan 1.3", "docker compose exec -T app vendor/bin/infection --version", ""},
	{"plan 1.3", "docker compose exec -T app php vendor/bin/infection --threads=max", "infection"},
	// An empty word is a subcommand, as in 0.6
	{"plan 1.3", "deptrac ''", ""},
}

// words splits a call as the shell does, with the assignments and wrappers in front of the program.
func words(t *testing.T, command string) []string {
	t.Helper()
	script, err := shell.Parse(command)
	if err != nil {
		t.Fatalf("Parse(%q) failed: %v", command, err)
	}
	if len(script.Commands) == 0 {
		return nil
	}
	return script.Commands[0].Raw
}

func TestRunnerNamesTheRunnerOfAHeavyCall(t *testing.T) {
	for _, current := range runnerCases {
		got, heavy := catalogue.Runner(words(t, current.command))

		if got != current.want || heavy != (current.want != "") {
			t.Errorf("%s: Runner(%q) = (%q, %v), want (%q, %v)", current.origin, current.command, got, heavy, current.want, current.want != "")
		}
	}
}
