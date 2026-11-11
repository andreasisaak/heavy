package guard_test

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"testing"
)

// test_heavy_guard.py:250
func TestEveryRewriteIsValidBash(t *testing.T) {
	_, worktree := listed(t)
	for _, command := range []string{
		"make ci",
		"make ci;",
		"make ci ;  ",
		"make ci &",
		"make ci & wait",
		"make ci 2>&1 | tail -50",
		"make ci && echo ok || echo failed",
		"{ make ci; }",
		"if make ci; then echo green; fi",
		"for attempt in 1 2; do make ci && break; done",
		"while ! make ci; do sleep 1; done",
		"! make ci || echo failed",
		"(make ci) && echo 'it''s done'",
	} {
		for _, background := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s background=%v", command, background), func(t *testing.T) {
				input := updated(guardOf(t, command, worktree, background))
				if input == nil {
					t.Fatalf("%q was not rewritten", command)
				}
				var stderr bytes.Buffer
				syntax := exec.Command("/bin/bash", "-n", "-c", input.Command)
				syntax.Stderr = &stderr
				err := syntax.Run()

				exitStatus := 0
				var exitError *exec.ExitError
				if errors.As(err, &exitError) {
					exitStatus = exitError.ExitCode()
				} else if err != nil {
					t.Fatalf("running bash -n: %v", err)
				}
				check(t, "bash -n of "+input.Command, [2]string{fmt.Sprint(exitStatus), stderr.String()}, [2]string{"0", ""})
			})
		}
	}
}
