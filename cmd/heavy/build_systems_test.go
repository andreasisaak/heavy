package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/testkit"
)

// On Linux docker runs without a VM, so build.sh builds bin/heavy alone and misses no bin/heavy-linux.
func TestBuildScriptOnLinuxBuildsNoHelperForAVM(t *testing.T) {
	plugin := newLinuxRoot(t)

	first := plugin.run(t, plugin.path, "guard")
	second := plugin.run(t, plugin.path, "guard")

	built := outcome{stdout: "heavy built for host got --hook guard\n", stderr: "heavy: building bin/heavy with " + filepath.Join(plugin.fakes, "go") + "\n"}
	if first != built || second != (outcome{stdout: "heavy built for host got --hook guard\n"}) {
		t.Fatalf("build.sh = %+v, then %+v; want %+v, then the hook without a build", first, second, built)
	}
	if _, err := os.Stat(filepath.Join(plugin.root, "bin", "heavy-linux")); !errors.Is(err, os.ErrNotExist) || plugin.builds(t) != 1 {
		t.Fatalf("bin/heavy-linux: %v, %d builds; want no such file after one build", err, plugin.builds(t))
	}
}

// A failed build on Linux leaves no half-written binary behind.
func TestBuildScriptOnLinuxThatFailsLeavesNothingBehind(t *testing.T) {
	plugin := newLinuxRoot(t)
	write(t, filepath.Join(plugin.fakes, "fail"), "")

	got := plugin.run(t, plugin.path, "guard")

	want := outcome{exitStatus: 2, stderr: "heavy: building bin/heavy with " + filepath.Join(plugin.fakes, "go") + "\n" +
		"internal/x/x.go:1:1: syntax error\nheavy: building heavy failed; the go output above says why\n"}
	left, _ := filepath.Glob(filepath.Join(plugin.root, "bin", "*"))
	if got != want || len(left) != 0 {
		t.Fatalf("build.sh = %+v with %v in bin/, want %+v and an empty bin/", got, left, want)
	}
}

// toolsWithout links the programs build.sh needs into a folder of their own, so a test can leave one out. The
// folder holds no go: /usr/bin has one on some machines, such as the Ubuntu runner of the CI.
func toolsWithout(t *testing.T, missing string) string {
	t.Helper()
	folder := t.TempDir()
	for _, tool := range []string{"sh", "find", "sort", "xargs", "cut", "cat", "mkdir", "sleep", "mv", "dirname", "sed", "head", "chmod", "uname", "shasum", "sha256sum"} {
		path, err := exec.LookPath(tool)
		if tool == missing || err != nil {
			continue
		}
		if err := os.Symlink(path, filepath.Join(folder, tool)); err != nil {
			t.Fatal(err)
		}
	}
	return folder
}

// The stamp is the same with shasum (macOS, perl) and with sha256sum (coreutils), so either machine reads the
// stamp of the other build.
func TestBuildScriptStampsWithSha256sumWhereShasumIsMissing(t *testing.T) {
	plugin := newPluginRoot(t)
	withShasum := toolsWithout(t, "sha256sum")
	withoutShasum := toolsWithout(t, "shasum")
	if _, err := os.Stat(filepath.Join(withoutShasum, "sha256sum")); err != nil {
		// A Mac has no sha256sum before macOS 15; shasum stands in under that name, with the same output.
		shasum, err := exec.LookPath("shasum")
		if err != nil {
			t.Fatal(err)
		}
		testkit.FakeProgram(t, withoutShasum, "sha256sum", `exec `+shasum+` -a 256 "$@"`)
	}
	if _, err := os.Stat(filepath.Join(withShasum, "shasum")); err != nil {
		t.Skip("this machine has no shasum to compare with")
	}

	plugin.run(t, plugin.fakes+":"+withShasum, "guard")
	first, firstError := os.ReadFile(filepath.Join(plugin.root, "bin", ".stamp"))
	if err := os.Remove(filepath.Join(plugin.root, "bin", ".stamp")); err != nil {
		t.Fatal(err)
	}
	got := plugin.run(t, plugin.fakes+":"+withoutShasum, "session-start")
	second, secondError := os.ReadFile(filepath.Join(plugin.root, "bin", ".stamp"))

	if firstError != nil || secondError != nil || len(first) != 65 || string(first) != string(second) {
		t.Fatalf("stamps %q (%v) and %q (%v), want the same 64 hex digits; the second run: %+v", first, firstError, second, secondError, got)
	}
}

// Without shasum and sha256sum build.sh says what it misses instead of stamping every build alike.
func TestBuildScriptWithoutAChecksumProgramSaysSo(t *testing.T) {
	plugin := newPluginRoot(t)
	tools := toolsWithout(t, "shasum")
	if err := os.Remove(filepath.Join(tools, "sha256sum")); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}

	got := plugin.run(t, plugin.fakes+":"+tools, "guard")

	want := outcome{exitStatus: 2, stderr: "heavy: found neither shasum nor sha256sum on PATH; install one and start the session again\n"}
	if got != want {
		t.Fatalf("build.sh = %+v, want %+v", got, want)
	}
}

// On a machine with several users, kill -0 cannot reach the build of another user; its entry in /proc shows that
// it lives, so its lock is not taken over.
func TestBuildScriptMonitorWaitsForTheBuildOfAnotherUser(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		t.Skip("needs /proc and a process of another user, which root signals anyway")
	}
	plugin, lock := lockedRoot(t)
	write(t, filepath.Join(lock, "pid"), strconv.Itoa(1)+"\n")
	done := make(chan outcome, 1)

	go func() { done <- plugin.run(t, plugin.path, "monitor") }()

	select {
	case got := <-done:
		t.Fatalf("build.sh monitor did not wait for the lock of process 1: %+v", got)
	case <-time.After(3 * time.Second):
	}
	if err := os.RemoveAll(lock); err != nil {
		t.Fatal(err)
	}
	if got := <-done; got != (outcome{stdout: "lock free, got --monitor\n"}) {
		t.Fatalf("build.sh monitor = %+v, want it to run once the lock is gone", got)
	}
}

// On the Ubuntu runner of the CI two sessions that started at once both built: each mkdir of the lock folder
// reported success (trace of 2026-11-11). The lock must not depend on mkdir failing for a folder that exists, so
// this mkdir never does.
func TestBuildScriptBuildsOnceAlthoughMkdirSucceedsOnAnExistingFolder(t *testing.T) {
	plugin := newPluginRoot(t)
	mkdir, err := exec.LookPath("mkdir")
	if err != nil {
		t.Fatal(err)
	}
	testkit.FakeProgram(t, plugin.fakes, "mkdir", `exec `+mkdir+` -p "$@"`)
	results := make(chan outcome, 2)
	for range 2 {
		go func() { results <- plugin.run(t, plugin.path, "session-start") }()
	}
	first, second := <-results, <-results

	built := outcome{stdout: "heavy built for host got --hook session-start\n", stderr: plugin.building()}
	waited := outcome{stdout: "heavy built for host got --hook session-start\n"}
	one := (first == built && second == waited) || (first == waited && second == built)
	if !one || plugin.builds(t) != 2 {
		t.Fatalf("results %+v and %+v, %d builds; want one build of each binary and the other session waiting for it", first, second, plugin.builds(t))
	}
}
