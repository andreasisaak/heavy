package shell_test

// Calls of hooks/test_heavy_guard.py: where the guard puts heavy in front of a command, and which commands
// hooks/shell_scan.py could not place, so the guard ran the whole call through heavy.
var guardCases = []parseCase{
	{"test_heavy_guard.py:155", `set -o pipefail; make ci 2>&1 | tee /tmp/ci.log; exit_status=$?; echo "ci $exit_status"; exit $exit_status`, []expected{
		listed("set", "set", "-o", "pipefail"),
		listed("make", "make", "ci"),
		listed("tee", "tee", "/tmp/ci.log"),
		listed("echo", "echo", "ci $exit_status"),
		listed("exit $", "exit", "$exit_status"),
	}},
	{"test_heavy_guard.py:165", "FULL=1 make ci", []expected{listed("make", "FULL=1", "make", "ci").normalized("make", "ci")}},
	{"test_heavy_guard.py:166", "make lint && make ci", []expected{listed("make lint", "make", "lint"), listed("make ci", "make", "ci")}},
	{"test_heavy_guard.py:167", "make test;", []expected{listed("make", "make", "test")}},
	{"test_heavy_guard.py:179", "make -C /sites/main-feature ci", []expected{listed("make", "make", "-C", "/sites/main-feature", "ci")}},
	{"test_heavy_guard.py:196", "git commit -m 'make ci'", []expected{listed("git", "git", "commit", "-m", "make ci")}},
	{"test_heavy_guard.py:197", "git commit -m \"fix \\`make ci\\` timeout\"", []expected{listed("git", "git", "commit", "-m", "fix `make ci` timeout")}},
	{"test_heavy_guard.py:198", "gh pr create --title fix --body 'runs `make ci` now'", []expected{
		listed("gh", "gh", "pr", "create", "--title", "fix", "--body", "runs `make ci` now"),
	}},
	{"test_heavy_guard.py:199", "echo '$(make ci)'", []expected{listed("echo", "echo", "$(make ci)")}},
	{"test_heavy_guard.py:227", "if make ci; then echo green; fi", []expected{clause("make", "make", "ci"), clause("echo", "echo", "green")}},
	{"test_heavy_guard.py:228", "for attempt in 1 2; do make ci && break; done", []expected{clause("make", "make", "ci"), clause("break", "break")}},
	{"test_heavy_guard.py:229", "while ! make ci; do sleep 5; done", []expected{clause("make", "make", "ci"), clause("sleep", "sleep", "5")}},
	{"test_heavy_guard.py:230", "until make ci; do sleep 5; done", []expected{clause("make", "make", "ci"), clause("sleep", "sleep", "5")}},
	{"test_heavy_guard.py:231", "! make ci", []expected{listed("make", "make", "ci")}},
	{"test_heavy_guard.py:232", "{ make ci; }", []expected{clause("make", "make", "ci")}},
	{"test_heavy_guard.py:233", "if true; then echo; else make ci; fi", []expected{
		clause("true", "true"), clause("echo", "echo"), clause("make", "make", "ci"),
	}},
	{"test_heavy_guard.py:245", "make ci &", []expected{listed("make", "make", "ci")}},
	{"test_heavy_guard.py:256", "make ci ;  ", []expected{listed("make", "make", "ci")}},
	{"test_heavy_guard.py:258", "make ci & wait", []expected{listed("make", "make", "ci"), listed("wait", "wait")}},
	{"test_heavy_guard.py:259", "make ci 2>&1 | tail -50", []expected{listed("make", "make", "ci"), listed("tail", "tail", "-50")}},
	{"test_heavy_guard.py:260", "make ci && echo ok || echo failed", []expected{
		listed("make", "make", "ci"), listed("echo ok", "echo", "ok"), listed("echo failed", "echo", "failed"),
	}},
	{"test_heavy_guard.py:265", "! make ci || echo failed", []expected{listed("make", "make", "ci"), listed("echo", "echo", "failed")}},
	{"test_heavy_guard.py:266", "(make ci) && echo 'it''s done'", []expected{clause("make", "make", "ci"), listed("echo", "echo", "its done")}},
	{"test_heavy_guard.py:292", "nice -n 5 make ci", []expected{listed("nice", "nice", "-n", "5", "make", "ci").normalized("make", "ci")}},
	{"test_heavy_guard.py:293", "bash -c 'make ci'", []expected{listed("bash", "bash", "-c", "make ci"), nested(0, "make", "ci")}},
	{"test_heavy_guard.py:324", "nice make ci", []expected{listed("nice", "nice", "make", "ci").normalized("make", "ci")}},
	{"test_heavy_guard.py:324", "timeout 900 make ci", []expected{listed("timeout", "timeout", "900", "make", "ci").normalized("make", "ci")}},
	{"test_heavy_guard.py:324", "timeout -s KILL 15m make ci", []expected{
		listed("timeout", "timeout", "-s", "KILL", "15m", "make", "ci").normalized("make", "ci"),
	}},
	{"test_heavy_guard.py:324", "env FULL=1 make ci", []expected{listed("env", "env", "FULL=1", "make", "ci").normalized("make", "ci")}},
	{"test_heavy_guard.py:335", "(make ci)", []expected{clause("make", "make", "ci")}},
	{"test_heavy_guard.py:336", "echo $(make ci)", []expected{listed("echo", "echo", "$(make ci)"), substitution("make", "make", "ci")}},
	{"test_heavy_guard.py:337", "status=`make ci`", []expected{substitution("make", "make", "ci")}},
	{"test_heavy_guard.py:338", "echo \"result: `make ci`\"", []expected{listed("echo", "echo", "result: `make ci`"), substitution("make", "make", "ci")}},
	{"test_heavy_guard.py:339", `echo "$(make ci)"`, []expected{listed("echo", "echo", "$(make ci)"), substitution("make", "make", "ci")}},
	{"test_heavy_guard.py:340", "cat <<'EOF'\nnotes\nEOF\nmake ci", []expected{listed("cat", "cat"), listed("make", "make", "ci")}},
	{"test_heavy_guard.py:341", "make ci # the full run", []expected{listed("make", "make", "ci")}},
	{"test_heavy_guard.py:369", "cd /sites/main-feature\nmake ci", []expected{listed("cd", "cd", "/sites/main-feature"), listed("make", "make", "ci")}},
	{"test_heavy_guard.py:377", "make ci > /tmp/ci.log 2>&1", []expected{listed("make", "make", "ci")}},
	{"test_heavy_guard.py:379", "make ci &> /tmp/ci.log", []expected{listed("make", "make", "ci")}},
	{"test_heavy_guard.py:410", "mkdir -p /tmp/heavy-missing-directory && make ci 2>&1 | tee /tmp/heavy-missing-directory/ci.log", []expected{
		listed("mkdir", "mkdir", "-p", "/tmp/heavy-missing-directory"),
		listed("make", "make", "ci"),
		listed("tee", "tee", "/tmp/heavy-missing-directory/ci.log"),
	}},
}

// Calls where 0.6 differed from Bash (specs/heavy-go/analysis.md, Tokenizer-Grenzfälle); Bash holds now.
var correctedCases = []parseCase{
	// shlex took `#b` for a comment.
	{"plan 1.2", "echo a#b make ci", []expected{listed("echo", "echo", "a#b", "make", "ci")}},
	// shlex took `<` for a redirect and lost the substitution.
	{"plan 1.2", "diff <(make ci) b", []expected{listed("diff", "diff", "<(make ci)", "b"), substitution("make", "make", "ci")}},
	// for, do and done were words of pseudo commands.
	{"plan 1.2", "for f in a; do make ci; done", []expected{clause("make", "make", "ci")}},
	{"plan 1.2", "make lint\nmake ci", []expected{listed("make lint", "make", "lint"), listed("make ci", "make", "ci")}},
	{"plan 1.2", "cat <<'EOF'\nmake ci\nEOF", []expected{listed("cat", "cat")}},
	// Bash expands the substitutions of a heredoc with an unquoted delimiter; 0.6 skipped every heredoc body.
	{"plan 1.2", "cat <<EOF\n$(make ci)\nEOF", []expected{listed("cat", "cat"), substitution("make", "make", "ci")}},
	// The remnant "ci`" of a split backtick substitution was a command of its own.
	{"analysis.md", "x=`make ci` && echo", []expected{substitution("make", "make", "ci"), listed("echo", "echo")}},
	// A shell that does not execute runs none of its string.
	{"plan 1.2", "bash -o noexec -c 'make ci'", []expected{listed("bash", "bash", "-o", "noexec", "-c", "make ci")}},
}

// Shell strings the shell parses again, and the forms of the plan for task 1.2.
var nestedCases = []parseCase{
	{"plan 1.2", `eval "make ci"`, []expected{listed("eval", "eval", "make ci"), nested(0, "make", "ci")}},
	{"plan 1.2", "eval make ci", []expected{listed("eval", "eval", "make", "ci"), nested(0, "make", "ci")}},
	{"plan 1.2", "ssh host 'make ci'", []expected{listed("ssh", "ssh", "host", "make ci"), nested(0, "make", "ci")}},
	{"plan 1.2", "ssh host make ci", []expected{listed("ssh", "ssh", "host", "make", "ci")}},
	{"plan 1.2", "nice bash -c 'make ci'", []expected{listed("nice", "nice", "bash", "-c", "make ci").normalized("bash", "-c", "make ci"), nested(0, "make", "ci")}},
	{"plan 1.2", `bash -c "sh -c 'make ci'"`, []expected{
		listed("bash", "bash", "-c", "sh -c 'make ci'"), nested(0, "sh", "-c", "make ci"), nested(1, "make", "ci"),
	}},
	{"plan 1.2", "echo $(bash -c 'make ci')", []expected{
		listed("echo", "echo", "$(bash -c 'make ci')"),
		substitution("bash", "bash", "-c", "make ci"),
		{raw: []string{"make", "ci"}, place: placeOther, substituted: true, parent: 1},
	}},
	{"plan 1.2", "command make ci", []expected{listed("command", "command", "make", "ci").normalized("make", "ci")}},
	{"plan 1.2", "ddev exec make ci", []expected{listed("ddev", "ddev", "exec", "make", "ci").normalized("make", "ci")}},
	{"plan 1.2", "sudo -u www make ci", []expected{listed("sudo", "sudo", "-u", "www", "make", "ci").normalized("make", "ci")}},
	{"plan 1.2", "xargs -n 1 make", []expected{listed("xargs", "xargs", "-n", "1", "make").normalized("make")}},
	{"plan 1.2", "caffeinate -i make ci", []expected{listed("caffeinate", "caffeinate", "-i", "make", "ci").normalized("make", "ci")}},
	{"plan 1.2", "time make ci", []expected{listed("make", "make", "ci")}},
	{"plan 1.2", "exec make ci", []expected{listed("exec", "exec", "make", "ci").normalized("make", "ci")}},
	{"plan 1.2", `\make ci`, []expected{listed("\\make", "make", "ci")}},
	{"plan 1.2", "export A=1 && make ci", []expected{listed("export", "export", "A=1"), listed("make", "make", "ci")}},
	{"plan 1.2", "case $x in a) make ci;; esac", []expected{other("make", "make", "ci")}},
	{"plan 1.2", "f() { make ci; }", []expected{other("make", "make", "ci")}},
	{"plan 1.2", "env", nil},
}
