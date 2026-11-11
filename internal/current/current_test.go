package current_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andreasisaak/heavy/internal/current"
)

// plugin makes the folder of a heavy of the given version with a build stamp, as an install of the plugin has it.
func plugin(t *testing.T, version, stamp string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		".claude-plugin/plugin.json": `{"name": "heavy", "version": "` + version + `"}`,
		"bin/.stamp":                 stamp + "\n",
		"shims/make":                 "",
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func target(t *testing.T, state string) string {
	t.Helper()
	found, err := os.Readlink(filepath.Join(state, "current"))
	if err != nil {
		t.Fatalf("reading the link: %v", err)
	}
	return found
}

func point(t *testing.T, state, root string, starting bool) {
	t.Helper()
	if err := current.Point(state, root, starting); err != nil {
		t.Fatalf("Point failed: %v", err)
	}
}

// The shims of every session lie behind one path without a version in it, $STATE/current/shims, so a session keeps
// its PATH over an update of heavy and still reaches the shims of the newest version.
func TestShimsLieBehindALinkWithoutAVersion(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	root := plugin(t, "1.2.0", "aaa")

	point(t, state, root, true)

	if got := target(t, state); got != root || current.Shims(state) != filepath.Join(state, "current", "shims") {
		t.Fatalf("link = %s, shims = %s; want the link at %s and the shims below %s/current", got, current.Shims(state), root, state)
	}
	if _, err := os.Stat(filepath.Join(current.Shims(state), "make")); err != nil {
		t.Fatalf("the shim behind the link: %v", err)
	}
}

// A session of 1.2.0 runs, heavy 1.3.0 is installed, and a hook of 1.3.0 runs in any session: from then on the
// PATH of the old session leads to the shims of 1.3.0. A hook of the old version that runs later does not take
// the link back.
func TestANewerVersionTakesTheLinkAndAnOlderDoesNotTakeItBack(t *testing.T) {
	for name, starting := range map[string]bool{"a hook of any call": false, "a session start": true} {
		t.Run(name, func(t *testing.T) {
			state := t.TempDir()
			older, newer := plugin(t, "1.2.0", "aaa"), plugin(t, "1.3.0", "bbb")
			point(t, state, older, true)

			point(t, state, newer, starting)
			afterUpdate := target(t, state)
			point(t, state, older, false)
			point(t, state, older, true)

			if afterUpdate != newer || target(t, state) != newer {
				t.Fatalf("link after the newer hook = %s, after the older ones = %s; want %s both times", afterUpdate, target(t, state), newer)
			}
		})
	}
}

func TestVersionsCompareAsNumbers(t *testing.T) {
	state := t.TempDir()
	older, newer := plugin(t, "1.9.0", "aaa"), plugin(t, "1.10.0", "bbb")
	point(t, state, older, true)

	point(t, state, newer, false)

	if got := target(t, state); got != newer {
		t.Fatalf("link = %s, want 1.10.0 at %s over 1.9.0", got, newer)
	}
}

// Another build of the same version, such as a checkout under development, takes the link at its session start
// and not with every call, so two installs of one version do not take it from each other call by call.
func TestAnotherBuildOfTheSameVersionTakesTheLinkOnlyAtASessionStart(t *testing.T) {
	state := t.TempDir()
	installed, checkout := plugin(t, "1.3.0", "aaa"), plugin(t, "1.3.0", "bbb")
	point(t, state, installed, true)

	point(t, state, checkout, false)
	afterCall := target(t, state)
	point(t, state, checkout, true)

	if afterCall != installed || target(t, state) != checkout {
		t.Fatalf("link after a call = %s, after the session start = %s; want %s, then %s", afterCall, target(t, state), installed, checkout)
	}
}

func TestTheSameBuildInAnotherFolderLeavesTheLink(t *testing.T) {
	state := t.TempDir()
	first, second := plugin(t, "1.3.0", "aaa"), plugin(t, "1.3.0", "aaa")
	point(t, state, first, true)

	point(t, state, second, true)

	if got := target(t, state); got != first {
		t.Fatalf("link = %s, want it left at %s: both folders hold the same build", got, first)
	}
}

// A link to a folder that is gone, as after the old version was removed, goes to whoever asks.
func TestALinkToAFolderThatIsGoneGoesToTheHeavyThatAsks(t *testing.T) {
	state := t.TempDir()
	gone, older := plugin(t, "9.0.0", "aaa"), plugin(t, "1.2.0", "bbb")
	point(t, state, gone, true)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	point(t, state, older, false)

	if got := target(t, state); got != older {
		t.Fatalf("link = %s, want %s", got, older)
	}
}

func TestSomethingElseAtThePlaceOfTheLinkIsAnError(t *testing.T) {
	state := t.TempDir()
	if err := os.MkdirAll(filepath.Join(state, "current", "kept"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := current.Point(state, plugin(t, "1.3.0", "aaa"), true)

	want := "heavy: " + filepath.Join(state, "current") + " is no link to a plugin folder of heavy; remove it, heavy makes the link again"
	if err == nil || err.Error() != want {
		t.Fatalf("Point error = %v, want %q", err, want)
	}
}

func TestAManifestThatCannotBeReadIsAnError(t *testing.T) {
	state := t.TempDir()
	older, broken := plugin(t, "1.2.0", "aaa"), plugin(t, "1.3.0", "bbb")
	point(t, state, older, true)
	manifest := filepath.Join(broken, ".claude-plugin", "plugin.json")
	if err := os.WriteFile(manifest, []byte(`{"version": `), 0o644); err != nil {
		t.Fatal(err)
	}

	err := current.Point(state, broken, true)

	want := "heavy: " + manifest + ": expected a version string: unexpected end of JSON input"
	if err == nil || err.Error() != want || target(t, state) != older {
		t.Fatalf("Point error = %v with the link at %s, want %q and the link left at %s", err, target(t, state), want, older)
	}
}

// A heavy outside a plugin folder, such as one built for a test, has no shims and takes no link.
func TestAHeavyWithoutShimsMakesNoLink(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")

	point(t, state, t.TempDir(), true)

	if _, err := os.Lstat(state); !os.IsNotExist(err) {
		t.Fatalf("the state directory after Point: %v, want none", err)
	}
}
