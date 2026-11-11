package shell_test

// Commands of hooks/test_heavy_detect.py, split as hooks/shell_tokens.py split them for the detection.
var detectCases = []parseCase{
	{"test_heavy_detect.py:63", "tests/Complete/checkall.sh", []expected{listed("tests", "tests/Complete/checkall.sh")}},
	{"test_heavy_detect.py:63", "CHECK_WORKER=12 CHECKALL_CASES='Apple' tests/Complete/checkall.sh", []expected{
		listed("tests", "CHECK_WORKER=12", "CHECKALL_CASES=Apple", "tests/Complete/checkall.sh").normalized("tests/Complete/checkall.sh"),
	}},
	{"test_heavy_detect.py:64", "env CHECKALL_CASES=x tests/Complete/checkall.sh", []expected{
		listed("env", "env", "CHECKALL_CASES=x", "tests/Complete/checkall.sh").normalized("tests/Complete/checkall.sh"),
	}},
	{"test_heavy_detect.py:65", "CHECKALL_CASES= tests/Complete/checkall.sh", []expected{
		listed("tests", "CHECKALL_CASES=", "tests/Complete/checkall.sh").normalized("tests/Complete/checkall.sh"),
	}},
	{"test_heavy_detect.py:65", "timeout 600 nice -n 5 env CHECKALL_CASES=x tests/Complete/checkall.sh", []expected{
		listed("timeout", "timeout", "600", "nice", "-n", "5", "env", "CHECKALL_CASES=x", "tests/Complete/checkall.sh").
			normalized("tests/Complete/checkall.sh"),
	}},
	{"test_heavy_detect.py:66", "CHECKALL_CASES='' bash tests/Complete/checkall.sh", []expected{
		listed("bash", "CHECKALL_CASES=", "bash", "tests/Complete/checkall.sh").normalized("bash", "tests/Complete/checkall.sh"),
	}},
	{"test_heavy_detect.py:67", "bash -c 'CHECKALL_CASES=Apple tests/Complete/checkall.sh'", []expected{
		listed("bash", "bash", "-c", "CHECKALL_CASES=Apple tests/Complete/checkall.sh"),
		nested(0, "CHECKALL_CASES=Apple", "tests/Complete/checkall.sh").normalized("tests/Complete/checkall.sh"),
	}},
	{"test_heavy_detect.py:69", `make test-complete TEST_WORKERS="1 2"`, []expected{
		listed("make", "make", "test-complete", "TEST_WORKERS=1 2"),
	}},
	{"test_heavy_detect.py:69", "TEST_WORKERS=12 make test-complete", []expected{
		listed("make", "TEST_WORKERS=12", "make", "test-complete").normalized("make", "test-complete"),
	}},
	{"test_heavy_detect.py:74", "make test-complete TEST_WORKERS=12 && CHECK_WORKER=12 CHECKALL_CASES='Apple|Orange' tests/Complete/checkall.sh", []expected{
		listed("make", "make", "test-complete", "TEST_WORKERS=12"),
		listed("tests", "CHECK_WORKER=12", "CHECKALL_CASES=Apple|Orange", "tests/Complete/checkall.sh").normalized("tests/Complete/checkall.sh"),
	}},
	{"test_heavy_detect.py:111", "docker compose exec -T web pnpm exec vitest run --project unit", []expected{
		listed("docker", "docker", "compose", "exec", "-T", "web", "pnpm", "exec", "vitest", "run", "--project", "unit"),
	}},
	{"test_heavy_detect.py:133", "make lint && make ci; docker compose exec -T api uv run pytest -x", []expected{
		listed("make lint", "make", "lint"),
		listed("make ci", "make", "ci"),
		listed("docker", "docker", "compose", "exec", "-T", "api", "uv", "run", "pytest", "-x"),
	}},
	// 0.6 kept the keywords as words (start 1) and `fi` as a command of its own; the keywords are syntax now.
	{"test_heavy_detect.py:148", "if pnpm test; then echo green; fi", []expected{
		clause("pnpm", "pnpm", "test"),
		clause("echo", "echo", "green"),
	}},
	{"test_heavy_detect.py:156", "cd /sites/main && pnpm exec vitest run", []expected{
		listed("cd", "cd", "/sites/main"),
		listed("pnpm", "pnpm", "exec", "vitest", "run"),
	}},
	{"test_heavy_detect.py:163", "bash -c 'cd /sites/main && make ci'", []expected{
		listed("bash", "bash", "-c", "cd /sites/main && make ci"),
		nested(0, "cd", "/sites/main"),
		nested(0, "make", "ci"),
	}},
	{"test_heavy_detect.py:171", "make lint; cd /tmp/sub && make ci; cd ..; pytest", []expected{
		listed("make lint", "make", "lint"),
		listed("cd /tmp", "cd", "/tmp/sub"),
		listed("make ci", "make", "ci"),
		listed("cd ..", "cd", ".."),
		listed("pytest", "pytest"),
	}},
	{"test_heavy_detect.py:178", "docker compose --project-directory=/sites/main run --rm --no-deps web pnpm exec vitest run", []expected{
		listed("docker", "docker", "compose", "--project-directory=/sites/main", "run", "--rm", "--no-deps", "web", "pnpm", "exec", "vitest", "run"),
	}},
	// A shell string inside a container is the launcher's business, not a nested string of this shell.
	{"test_heavy_detect.py:190", "docker compose exec web sh -c 'tail -f /tmp/log; pnpm test'", []expected{
		listed("docker", "docker", "compose", "exec", "web", "sh", "-c", "tail -f /tmp/log; pnpm test"),
	}},
	{"test_heavy_detect.py:216", "D=/sites/other; docker compose --project-directory $D exec -T web pnpm build", []expected{
		listed("docker", "docker", "compose", "--project-directory", "$D", "exec", "-T", "web", "pnpm", "build"),
	}},
	{"test_heavy_detect.py:217", "docker compose -f ${ROOT}/compose.yaml exec -T web pnpm build", []expected{
		listed("docker", "docker", "compose", "-f", "${ROOT}/compose.yaml", "exec", "-T", "web", "pnpm", "build"),
	}},
	{"test_heavy_detect.py:219", "cd $D && pnpm build", []expected{
		listed("cd", "cd", "$D"),
		listed("pnpm", "pnpm", "build"),
	}},
	{"test_heavy_detect.py:230", "docker exec -w /app/apps/web main-web-1 sh -c 'pnpm exec vitest run --project unit'", []expected{
		listed("docker", "docker", "exec", "-w", "/app/apps/web", "main-web-1", "sh", "-c", "pnpm exec vitest run --project unit"),
	}},
	{"test_heavy_detect.py:247", "/opt/heavy/bin/heavy pnpm test", []expected{listed("/opt", "/opt/heavy/bin/heavy", "pnpm", "test")}},
	{"test_heavy_detect.py:247", "echo make ci", []expected{listed("echo", "echo", "make", "ci")}},
	{"test_heavy_detect.py:294", "bash -n tests/Complete/checkall.sh", []expected{listed("bash", "bash", "-n", "tests/Complete/checkall.sh")}},
	{"test_heavy_detect.py:296", "bash -xn tests/Complete/checkall.sh", []expected{listed("bash", "bash", "-xn", "tests/Complete/checkall.sh")}},
	{"test_heavy_detect.py:297", "bash -o noexec tests/Complete/checkall.sh", []expected{listed("bash", "bash", "-o", "noexec", "tests/Complete/checkall.sh")}},
	{"test_heavy_detect.py:298", "bash -n -c 'make ci'", []expected{listed("bash", "bash", "-n", "-c", "make ci")}},
	{"test_heavy_detect.py:310", "bash -x tests/Complete/checkall.sh", []expected{listed("bash", "bash", "-x", "tests/Complete/checkall.sh")}},
}
