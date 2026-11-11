package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andreasisaak/heavy/internal/testkit"
)

// release puts a release of built binaries behind the fake curl of the plugin root: curl -o <file> <url> copies
// the file named like the end of the url, logs the url, and fails like GitHub's 404 for a name the release lacks.
// The checksums are those of the files unless a test changes them afterwards.
func (p *pluginRoot) release(t *testing.T, names ...string) string {
	t.Helper()
	folder := t.TempDir()
	var sums []string
	for _, name := range names {
		content := "#!/bin/sh\necho \"downloaded " + name + " got $*\"\n"
		write(t, filepath.Join(folder, name), content)
		sum := sha256.Sum256([]byte(content))
		sums = append(sums, hex.EncodeToString(sum[:])+"  "+name)
	}
	write(t, filepath.Join(folder, "checksums.txt"), strings.Join(sums, "\n")+"\n")
	testkit.FakeProgram(t, p.fakes, "curl", `out=""; url=""
while [ $# -gt 0 ]; do
  case "$1" in -o) out=$2; shift ;; -*) ;; *) url=$1 ;; esac
  shift
done
echo "$url" >> `+folder+`/urls
[ -f "`+folder+`/${url##*/}" ] || exit 22
cat "`+folder+`/${url##*/}" > "$out"`)
	return folder
}

func (p *pluginRoot) stamp(t *testing.T) string {
	t.Helper()
	got := p.run(t, p.path, "stamp")
	if got.exitStatus != 0 || got.stderr != "" || len(got.stdout) != 65 {
		t.Fatalf("build.sh stamp = %+v, want 64 hex digits", got)
	}
	return strings.TrimSpace(got.stdout)
}

func urls(t *testing.T, release string) []string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(release, "urls"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(content)), "\n")
}

// build.sh takes the binaries GitHub built for exactly these sources, so a machine needs no Go.
func TestBuildScriptDownloadsTheBuildOfItsSources(t *testing.T) {
	plugin := newPluginRoot(t)
	release := plugin.release(t, "heavy-darwin-arm64", "heavy-linux-arm64")
	stamp := plugin.stamp(t)

	got := plugin.run(t, plugin.path, "guard")

	base := "https://github.com/andreasisaak/heavy/releases/download/build-" + stamp + "/"
	want := outcome{stdout: "downloaded heavy-darwin-arm64 got --hook guard\n",
		stderr: "heavy: downloaded bin/heavy and bin/heavy-linux for darwin-arm64 from the release build-" + stamp + "\n"}
	if got != want || plugin.builds(t) != 0 {
		t.Fatalf("build.sh = %+v with %d builds, want %+v without a build", got, plugin.builds(t), want)
	}
	helper, helperError := os.ReadFile(filepath.Join(plugin.root, "bin", "heavy-linux"))
	written, stampError := os.ReadFile(filepath.Join(plugin.root, "bin", ".stamp"))
	if helperError != nil || !strings.Contains(string(helper), "downloaded heavy-linux-arm64") || stampError != nil || string(written) != stamp+"\n" {
		t.Fatalf("bin/heavy-linux %q (%v), stamp %q (%v); want the linux build and the stamp %s", helper, helperError, written, stampError, stamp)
	}
	wantURLs := []string{base + "checksums.txt", base + "heavy-darwin-arm64", base + "heavy-linux-arm64"}
	if got := urls(t, release); strings.Join(got, "\n") != strings.Join(wantURLs, "\n") {
		t.Fatalf("curl fetched %q, want %q", got, wantURLs)
	}
	if left, _ := filepath.Glob(filepath.Join(plugin.root, "bin", "*.new*")); len(left) != 0 {
		t.Fatalf("the download left %v", left)
	}
}

// On Linux the one binary is the build for Linux, named bin/heavy; x86_64 is amd64.
func TestBuildScriptOnLinuxDownloadsTheLinuxBuild(t *testing.T) {
	plugin := newLinuxRoot(t)
	release := plugin.release(t, "heavy-linux-amd64")
	stamp := plugin.stamp(t)

	got := plugin.run(t, plugin.path, "guard")

	want := outcome{stdout: "downloaded heavy-linux-amd64 got --hook guard\n",
		stderr: "heavy: downloaded bin/heavy for linux-amd64 from the release build-" + stamp + "\n"}
	if got != want || plugin.builds(t) != 0 || len(urls(t, release)) != 2 {
		t.Fatalf("build.sh = %+v with %d builds and the downloads %q, want %+v from two downloads", got, plugin.builds(t), urls(t, release), want)
	}
}

// A download that does not match its checksum is thrown away, and the local Go builds.
func TestBuildScriptRejectsADownloadThatDoesNotMatchItsChecksum(t *testing.T) {
	plugin := newPluginRoot(t)
	release := plugin.release(t, "heavy-darwin-arm64", "heavy-linux-arm64")
	write(t, filepath.Join(release, "heavy-linux-arm64"), "#!/bin/sh\necho tampered\n")

	got := plugin.run(t, plugin.path, "guard")

	want := outcome{stdout: "heavy built for host got --hook guard\n",
		stderr: "heavy: the download of heavy-linux-arm64 does not match its checksum, so heavy does not use it\n" + plugin.building()}
	if got != want || plugin.builds(t) != 2 {
		t.Fatalf("build.sh = %+v with %d builds, want %+v from the local build", got, plugin.builds(t), want)
	}
	if left, _ := filepath.Glob(filepath.Join(plugin.root, "bin", "*.new*")); len(left) != 0 {
		t.Fatalf("the rejected download left %v", left)
	}
}

// A release that lacks the build for this machine, or a checksum line for it, is no build to take.
func TestBuildScriptBuildsItselfWhenTheReleaseLacksItsMachine(t *testing.T) {
	for name, prepare := range map[string]func(t *testing.T, plugin *pluginRoot){
		"no binary for the machine": func(t *testing.T, plugin *pluginRoot) { plugin.release(t, "heavy-linux-arm64") },
		"no checksum for the binary": func(t *testing.T, plugin *pluginRoot) {
			release := plugin.release(t, "heavy-darwin-arm64", "heavy-linux-arm64")
			write(t, filepath.Join(release, "checksums.txt"), "0000  heavy-linux-arm64\n")
		},
	} {
		t.Run(name, func(t *testing.T) {
			plugin := newPluginRoot(t)
			prepare(t, plugin)

			got := plugin.run(t, plugin.path, "guard")

			if got.stdout != "heavy built for host got --hook guard\n" || got.exitStatus != 0 || plugin.builds(t) != 2 {
				t.Fatalf("build.sh = %+v with %d builds, want the local build", got, plugin.builds(t))
			}
		})
	}
}

// Without a release for the sources and without Go, build.sh names both ways out.
func TestBuildScriptWithoutAReleaseAndWithoutGoNamesBothWays(t *testing.T) {
	plugin := newPluginRoot(t)

	got := plugin.run(t, toolsWithout(t, "go"), "guard")

	want := outcome{exitStatus: 2, stderr: "heavy: found no build of these sources to download and no go on PATH; wait a few minutes for the build on GitHub, " +
		"or install Go 1.27 (https://go.dev/dl/, on a Mac: brew install go), and start the session again\n"}
	if got != want {
		t.Fatalf("build.sh = %+v, want %+v", got, want)
	}
	if _, err := os.Stat(filepath.Join(plugin.root, "bin", "heavy")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("bin/heavy: %v, want none", err)
	}
}

// HEAVY_RELEASES names the releases of a fork.
func TestBuildScriptTakesTheReleasesOfAForkFromTheEnvironment(t *testing.T) {
	plugin := newPluginRoot(t)
	release := plugin.release(t, "heavy-darwin-arm64", "heavy-linux-arm64")
	stamp := plugin.stamp(t)
	plugin.runWith(t, plugin.path, "guard", "HEAVY_RELEASES=https://example.com/fork/releases/download")

	if got := urls(t, release)[0]; got != "https://example.com/fork/releases/download/build-"+stamp+"/checksums.txt" {
		t.Fatalf("curl fetched %q first, want the checksums of the fork", got)
	}
}
