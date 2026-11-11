package shell_test

import (
	"reflect"
	"testing"

	"github.com/andreasisaak/heavy/internal/shell"
)

func TestDirectoriesFollowCd(t *testing.T) {
	cases := []struct {
		origin string
		input  string
		want   []string
	}{
		// test_heavy_detect.py:171
		{"test_heavy_detect.py:171", "make lint; cd /tmp/sub && make ci; cd ..; pytest", []string{"/start", "/start", "/tmp/sub", "/tmp/sub", "/tmp"}},
		{"test_heavy_detect.py:219", "cd $D && pnpm build", []string{"/start", "/start"}},
		{"test_heavy_detect.py:163", "bash -c 'cd /sites/main && make ci'", []string{"/start", "/start", "/sites/main"}},
		{"plan 1.2", "cd sub/./x/ && make ci", []string{"/start", "/start/sub/x"}},
		{"plan 1.2", "cd ~/sites && make ci", []string{"/start", "/home/example/sites"}},
		{"plan 1.2", "cd ~ && make ci", []string{"/start", "/home/example"}},
		{"plan 1.2", "cd ~other/sites && make ci", []string{"/start", "/start"}},
		{"plan 1.2", "cd `pwd` && make ci", []string{"/start", "/start", "/start"}},
		// 0.6 semantics: a cd moves every later command, inside a subshell too.
		{"plan 1.2", "(cd /sites/main && make ci); make test", []string{"/start", "/sites/main", "/sites/main"}},
		{"plan 1.2", "cd && make ci", []string{"/start", "/start"}},
	}
	t.Setenv("HOME", "/home/example")
	for _, current := range cases {
		script, err := shell.Parse(current.input)
		if err != nil {
			t.Fatalf("%s: Parse(%q) failed: %v", current.origin, current.input, err)
		}

		got := script.Directories("/start")

		if !reflect.DeepEqual(got, current.want) {
			t.Errorf("%s: Directories of %q = %q, want %q", current.origin, current.input, got, current.want)
		}
	}
}
