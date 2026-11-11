package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runStart starts build.sh session-start with the hook input on stdin and CLAUDE_ENV_FILE set.
func (p *pluginRoot) runStart(t *testing.T, input, envFile string, environment ...string) outcome {
	t.Helper()
	command := exec.Command("/bin/sh", filepath.Join(p.root, "hooks", "build.sh"), "session-start")
	command.Env = append([]string{"PATH=" + p.path, "HOME=" + t.TempDir(), "CLAUDE_ENV_FILE=" + envFile}, environment...)
	command.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	status := 0
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		status = exitError.ExitCode()
	} else if err != nil {
		t.Fatalf("running build.sh: %v", err)
	}
	return outcome{exitStatus: status, stdout: stdout.String(), stderr: stderr.String()}
}

const startInput = `{"session_id":"5968af83-2137-45a0-b3d2-6d3a974c6814","hook_event_name":"SessionStart","source":"startup"}`

// probe 2026-11-08: Claude Code takes the environment of an interactive session when it starts its monitors, not
// when the hooks end, so a session that waits for the first build writes the shims and its id before the build
func TestBuildScriptWritesTheSessionEnvironmentBeforeTheFirstBuild(t *testing.T) {
	plugin := newPluginRoot(t)
	envFile := filepath.Join(t.TempDir(), "env.sh")
	atBuild := filepath.Join(plugin.fakes, "env-at-build")
	write(t, filepath.Join(plugin.fakes, "during"), "cat "+envFile+" > "+atBuild+"\n")

	state := filepath.Join(t.TempDir(), "state")

	got := plugin.runStart(t, startInput, envFile, "HEAVY_STATE_DIRECTORY="+state)

	root, err := filepath.EvalSymlinks(plugin.root)
	if err != nil {
		t.Fatal(err)
	}
	if linked, err := os.Readlink(filepath.Join(state, "current")); err != nil || linked != root {
		t.Fatalf("$STATE/current = %q, %v; want a link to %s", linked, err, root)
	}
	shims := filepath.Join(state, "current", "shims")
	want := "case \"$PATH\" in " + shims + ":*) ;; *) export PATH=" + shims + ":\"$PATH\" ;; esac\nexport HEAVY_SESSION=5968af83-2137-45a0-b3d2-6d3a974c6814\n"
	written, err := os.ReadFile(atBuild)
	if err != nil || string(written) != want {
		t.Fatalf("CLAUDE_ENV_FILE during the build = %q, %v; want %q", written, err, want)
	}
	if want := (outcome{stdout: "heavy built for host got --hook session-start\n", stderr: plugin.building()}); got != want {
		t.Fatalf("build.sh = %+v, want %+v", got, want)
	}
	ran, err := os.ReadFile(filepath.Join(plugin.fakes, "order"))
	if want := fmt.Sprintf("ran --hook session-start claude=%d\n", os.Getpid()); err != nil || string(ran) != want {
		t.Fatalf("order = %q, %v; want the hook with the Claude PID %q", ran, err, want)
	}
}

// Without HEAVY_STATE_DIRECTORY the link lies in ~/.cache/heavy, and build.sh leaves a link that is there: the hook
// of the built heavy compares the versions and turns it.
func TestBuildScriptLeavesALinkToAnotherHeavyToTheHook(t *testing.T) {
	plugin := newPluginRoot(t)
	envFile, home, other := filepath.Join(t.TempDir(), "env.sh"), t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".cache", "heavy"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, ".cache", "heavy", "current")
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}

	plugin.runStart(t, startInput, envFile, "HOME="+home)

	shims := filepath.Join(link, "shims")
	want := "case \"$PATH\" in " + shims + ":*) ;; *) export PATH=" + shims + ":\"$PATH\" ;; esac\nexport HEAVY_SESSION=5968af83-2137-45a0-b3d2-6d3a974c6814\n"
	written, err := os.ReadFile(envFile)
	if err != nil || string(written) != want {
		t.Fatalf("CLAUDE_ENV_FILE = %q, %v; want %q", written, err, want)
	}
	if linked, err := os.Readlink(link); err != nil || linked != other {
		t.Fatalf("the link = %q, %v; want it left at %s", linked, err, other)
	}
}

// with a binary of an earlier build, its hook writes the environment first and the changed sources build after it
func TestBuildScriptRunsTheHookOfTheLastBuildBeforeARebuild(t *testing.T) {
	plugin := newPluginRoot(t)
	plugin.run(t, plugin.path, "session-start")
	order := filepath.Join(plugin.fakes, "order")
	write(t, order, "")
	write(t, filepath.Join(plugin.fakes, "during"), "echo build >> "+order+"\n")
	write(t, filepath.Join(plugin.root, "internal", "x", "x.go"), "package x\n\nconst changed = true\n")

	got := plugin.runStart(t, startInput, filepath.Join(t.TempDir(), "env.sh"))

	ran, err := os.ReadFile(order)
	// heavy-37 2026-11-08: the hook runs as a child of build.sh now, so build.sh names the PID of Claude Code, its
	// own parent, here the test
	if want := fmt.Sprintf("ran --hook session-start claude=%d\nbuild\nbuild\n", os.Getpid()); err != nil || string(ran) != want {
		t.Fatalf("order = %q, %v; want %q: the hook with the Claude PID, then both builds", ran, err, want)
	}
	if want := (outcome{stdout: "heavy built for host got --hook session-start\n", stderr: plugin.building()}); got != want {
		t.Fatalf("build.sh = %+v, want %+v", got, want)
	}
}
