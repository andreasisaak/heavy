package guard

import (
	"reflect"
	"strings"
	"testing"
)

// strip runs stripFilters on a call with the fake of hooks/test_output_filters.py: a stage runs heavy when its
// text holds make ci, make test or `heavy `.
func strip(t *testing.T, command string) (string, []string) {
	t.Helper()
	current, err := parseCall(command)
	if err != nil {
		return command, nil
	}
	return stripFilters(current, func(start, end int) bool {
		text := command[start:end]
		return strings.Contains(text, "make ci") || strings.Contains(text, "heavy ") || strings.Contains(text, "make test")
	})
}

// test_output_filters.py:13
func TestFilterBehindAHeavyRunGoes(t *testing.T) {
	for _, current := range []struct {
		command, expected string
		removed           []string
	}{
		{"make ci 2>&1 | tail -80", "make ci 2>&1", []string{"| tail -80"}},
		{"make ci |& tail -n 20", "make ci", []string{"|& tail -n 20"}},
		{
			`set -o pipefail; heavy make ci 2>&1 | tee /tmp/ci.log | grep -E "FAIL|passed" | head -60; exit_status=$?`,
			"set -o pipefail; heavy make ci 2>&1 | tee /tmp/ci.log; exit_status=$?",
			[]string{`| grep -E "FAIL|passed" | head -60`},
		},
		{
			"set -o pipefail; (make test && make ci) 2>&1 | tail -12; exit_status=$?",
			"set -o pipefail; (make test && make ci) 2>&1; exit_status=$?",
			[]string{"| tail -12"},
		},
		{
			"heavy make fresh 2>&1 | tail -15 && heavy make ci 2>&1 | tail -15",
			"heavy make fresh 2>&1 && heavy make ci 2>&1",
			[]string{"| tail -15", "| tail -15"},
		},
		{`{ heavy make test 2>&1 | tail -5; echo "done $?"; }`, `{ heavy make test 2>&1; echo "done $?"; }`, []string{"| tail -5"}},
		{"(heavy make ci | tail -3) | grep x", "(heavy make ci)", []string{"| tail -3", "| grep x"}},
		{"make ci 2>&1 | LC_ALL=C grep -v DEBUG", "make ci 2>&1", []string{"| LC_ALL=C grep -v DEBUG"}},
	} {
		t.Run(current.command, func(t *testing.T) {
			stripped, removed := strip(t, current.command)

			if stripped != current.expected || !reflect.DeepEqual(removed, current.removed) {
				t.Errorf("stripFilters = %q, %q; want %q, %q", stripped, removed, current.expected, current.removed)
			}
		})
	}
}

// test_output_filters.py:46
func TestFilterTheCallNeedsStays(t *testing.T) {
	for _, command := range []string{
		// No heavy run in front of the filter
		"git log --oneline | head -5; heavy make ci",
		"cat /tmp/ci.log | tail -20",
		// The call uses the filter's result
		"make ci 2>&1 | grep -q FAIL && echo red",
		"make ci 2>&1 | grep -c FAIL",
		"make ci 2>&1 | grep -Ec 'FAIL|ERROR'",
		"make ci 2>&1 | grep --quiet FAIL",
		"make ci 2>&1 | tail -1 > /tmp/last.txt",
		"make ci 2>&1 | tail -f",
		// A stage after the filter reads it
		"make ci 2>&1 | tail -5 | xargs echo",
		// Inside a substitution or a quoted shell string the filter is text of a word
		`echo "$(make ci | tail -1)"`,
		"heavy bash -c 'make ci | tail -5'",
		// Forms the walk cannot follow
		"make ci | tail -5 <<EOF\nx\nEOF",
		`case "$x" in a) make ci | tail -5;; esac`,
		"make ci | tail -5)",
	} {
		t.Run(command, func(t *testing.T) {
			stripped, removed := strip(t, command)

			if stripped != command || removed != nil {
				t.Errorf("stripFilters = %q, %q; want the call unchanged and nothing removed", stripped, removed)
			}
		})
	}
}
