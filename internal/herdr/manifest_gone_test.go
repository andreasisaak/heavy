package herdr_test

import (
	"os"
	"path/filepath"
	"testing"
)

// refresh_link.sh moved the link when the linked version's folder was gone: a manifest whose heavy no longer
// exists runs nothing, so any heavy writes it again, also one of an older or the same version
func TestManifestOfAHeavyThatIsGoneIsRewrittenWhateverItsVersion(t *testing.T) {
	for _, current := range []struct {
		name, present string
		gone          bool
	}{
		{"newer and gone", "1.0.1", true},
		{"same and gone", "1.0.0", true},
		{"newer and installed", "1.0.1", false},
	} {
		t.Run(current.name, func(t *testing.T) {
			plugin := pluginOf(t, "1.0.0")
			heavy := installed(t, current.present)
			present := manifestAt(t, plugin, current.present, heavy)
			if current.gone {
				if err := os.Remove(heavy); err != nil {
					t.Fatal(err)
				}
			}

			err := plugin.Generate()

			want := present
			if current.gone {
				want = plugin.Manifest()
			}
			check(t, "error", errorText(err), "<nil>")
			check(t, "manifest", readManifest(t, plugin), want)
		})
	}
}

func TestManifestWithoutACommandLineFailsWithWhatItFound(t *testing.T) {
	plugin := pluginOf(t, "1.0.0")
	content := "id = \"local.heavy-queue\"\nversion = \"1.0.1\"\n"
	if err := os.MkdirAll(filepath.Dir(plugin.ManifestPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plugin.ManifestPath(), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	err := plugin.Generate()

	check(t, "error", errorText(err), "heavy: "+plugin.ManifestPath()+`: expected a line command = ["<heavy binary>", ...], got no command line`)
	check(t, "manifest", readManifest(t, plugin), content)
}

func TestManifestFailsWithThePathWhenItsHeavyCannotBeChecked(t *testing.T) {
	plugin := pluginOf(t, "1.0.0")
	heavy := installed(t, "1.0.1")
	manifestAt(t, plugin, "1.0.1", heavy)
	locked := filepath.Dir(heavy)
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	err := plugin.Generate()

	check(t, "error", errorText(err), "heavy: checking the heavy binary of the herdr plugin: stat "+heavy+": permission denied")
}
