package sessionstart_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/herdr"
	"github.com/andreasisaak/heavy/internal/sessionstart"
	"github.com/andreasisaak/heavy/internal/testkit"
)

// pluginOf is the herdr plugin of a heavy of version in a plugin cache inside data, generated into data. Its
// binary exists, since a manifest whose heavy is gone is rewritten whatever its version.
func pluginOf(t *testing.T, data, version string) herdr.Plugin {
	t.Helper()
	heavy := filepath.Join(data, "cache", "heavy", version, "bin", "heavy")
	if err := os.MkdirAll(filepath.Dir(heavy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(heavy, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return herdr.Plugin{Data: data, Version: version, Heavy: heavy}
}

// startWith runs the session start with the herdr plugin and returns its exit status, stdout and stderr.
func startWith(t *testing.T, plugin herdr.Plugin) (int, string, string) {
	t.Helper()
	deps := sessionstart.Dependencies{
		Alive: everyProcessLives, EnvFile: filepath.Join(t.TempDir(), "env.sh"), Shims: "/plugins/heavy/shims", StateDirectory: testkit.StateDir(t), ClaudePID: 4242,
		Now: func() time.Time { return started }, HerdrPlugin: plugin,
	}
	var stdout, stderr bytes.Buffer
	status := sessionstart.Run(strings.NewReader(strings.Replace(sessionStart, "%s", t.TempDir(), 1)), &stdout, &stderr, deps)
	return status, stdout.String(), stderr.String()
}

func generated(t *testing.T, plugin herdr.Plugin) {
	t.Helper()
	if err := plugin.Generate(); err != nil {
		t.Fatalf("generating the manifest of %s: %v", plugin.Version, err)
	}
}

func manifestIn(t *testing.T, plugin herdr.Plugin) string {
	t.Helper()
	data, err := os.ReadFile(plugin.ManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// G12: a session of an older heavy leaves the manifest of a newer one
func TestSessionStartOfAnOlderVersionLeavesTheHerdrPluginOfANewerOne(t *testing.T) {
	data := t.TempDir()
	newer := pluginOf(t, data, "1.0.1")
	generated(t, newer)

	status, stdout, stderr := startWith(t, pluginOf(t, data, "1.0.0"))

	got := []any{status, stdout, stderr, manifestIn(t, newer)}
	if want := []any{0, "", "", newer.Manifest()}; !equal(got, want) {
		t.Fatalf("session start = %#v\nwant %#v", got, want)
	}
}

// G12, FR-35: a session of a newer heavy writes the manifest again with the absolute path of its own bin/heavy
func TestSessionStartOfANewerVersionRewritesTheHerdrPlugin(t *testing.T) {
	data := t.TempDir()
	generated(t, pluginOf(t, data, "1.0.0"))
	newer := pluginOf(t, data, "1.0.1")

	status, stdout, stderr := startWith(t, newer)

	got := []any{status, stdout, stderr, manifestIn(t, newer)}
	if want := []any{0, "", "", newer.Manifest()}; !equal(got, want) {
		t.Fatalf("session start = %#v\nwant %#v", got, want)
	}
}

func TestSessionStartWithoutPluginDataSaysSoAndSucceeds(t *testing.T) {
	status, stdout, stderr := startWith(t, herdr.Plugin{Version: "1.0.1", Heavy: "/plugins/heavy/bin/heavy"})

	got := []any{status, stdout, stderr}
	if want := []any{0, "", "heavy: CLAUDE_PLUGIN_DATA is not set, so the herdr plugin is not generated\n"}; !equal(got, want) {
		t.Fatalf("session start = %#v\nwant %#v", got, want)
	}
}

func TestSessionStartFailsWithTheReasonWhenTheHerdrPluginCannotBeGenerated(t *testing.T) {
	plugin := pluginOf(t, t.TempDir(), "1.0.1")
	if err := os.MkdirAll(filepath.Dir(plugin.ManifestPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plugin.ManifestPath(), []byte("id = \"local.heavy-queue\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	status, stdout, stderr := startWith(t, plugin)

	want := []any{1, "", "heavy: " + plugin.ManifestPath() + `: expected a line version = "<numbers separated by dots>", got no version line` + "\n"}
	if got := []any{status, stdout, stderr}; !equal(got, want) {
		t.Fatalf("session start = %#v\nwant %#v", got, want)
	}
}

func equal(got, want []any) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
