package guard_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andreasisaak/heavy/internal/herdr"
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

func manifestIn(t *testing.T, plugin herdr.Plugin) string {
	t.Helper()
	data, err := os.ReadFile(plugin.ManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// G12: /reload-plugins starts no SessionStart, so the guard of the reloaded, newer heavy writes the manifest of
// an older one again; a manifest of the same or a newer version stays
func TestGuardAfterAReloadRewritesTheHerdrPluginOfAnOlderVersion(t *testing.T) {
	for _, current := range []struct{ present, this, after string }{
		{"1.0.0", "1.0.1", "1.0.1"},
		{"1.0.1", "1.0.1", "1.0.1"},
		{"1.0.2", "1.0.1", "1.0.2"},
	} {
		t.Run(current.present+" then "+current.this, func(t *testing.T) {
			data := t.TempDir()
			if err := pluginOf(t, data, current.present).Generate(); err != nil {
				t.Fatal(err)
			}
			world := newWorld(t)
			world.herdr = pluginOf(t, data, current.this)

			got := world.run(t, bash("git status", t.TempDir(), false))

			check(t, "answer", got, nil)
			check(t, "manifest", manifestIn(t, world.herdr), pluginOf(t, data, current.after).Manifest())
		})
	}
}

// design G15: an error of heavy's own denies the call and names the file, the expected form and what it found
func TestGuardDeniesWhenTheHerdrPluginCannotBeGenerated(t *testing.T) {
	world := newWorld(t)
	world.herdr = pluginOf(t, t.TempDir(), "1.0.1")
	if err := os.MkdirAll(filepath.Dir(world.herdr.ManifestPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(world.herdr.ManifestPath(), []byte("version = \"one\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := world.run(t, bash("git status", t.TempDir(), false))

	check(t, "answer", got, denial("heavy: "+world.herdr.ManifestPath()+`: expected a line version = "<numbers separated by dots>", got version = "one"`))
}
