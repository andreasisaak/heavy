package detect_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andreasisaak/heavy/internal/detect"
	"github.com/andreasisaak/heavy/internal/testkit"
)

func TestShimFolderKnowsTheShimsOfEveryHeavyAndNoOtherFolder(t *testing.T) {
	links, broken, scripts, programs, linksElsewhere := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	binary := testkit.FakeProgram(t, t.TempDir(), "heavy", "exit 0")
	testkit.FakeProgram(t, links, "uv", "exit 0")
	if err := os.Symlink(binary, filepath.Join(links, "make")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../bin/heavy", filepath.Join(broken, "make")); err != nil {
		t.Fatal(err)
	}
	testkit.FakeProgram(t, scripts, "heavy-shim", "exit 1")
	testkit.FakeProgram(t, programs, "heavy", "exit 0")
	testkit.FakeProgram(t, programs, "make", "exit 0")
	if err := os.Symlink("/bin/sh", filepath.Join(linksElsewhere, "sh")); err != nil {
		t.Fatal(err)
	}
	file := testkit.FakeProgram(t, t.TempDir(), "shims", "exit 0")

	got := map[string]bool{}
	for name, folder := range map[string]string{
		"links to a heavy binary": links, "links to a heavy binary that is gone": broken, "the scripts of heavy 0.x": scripts,
		"programs, one of them named heavy": programs, "links to other programs": linksElsewhere, "an empty folder": t.TempDir(),
		"a folder that is gone": filepath.Join(t.TempDir(), "gone"), "a file": file, "no folder": "",
	} {
		got[name] = detect.ShimFolder(folder)
	}

	want := map[string]bool{
		"links to a heavy binary": true, "links to a heavy binary that is gone": true, "the scripts of heavy 0.x": true,
		"programs, one of them named heavy": false, "links to other programs": false, "an empty folder": false,
		"a folder that is gone": false, "a file": false, "no folder": false,
	}
	for name, expected := range want {
		if got[name] != expected {
			t.Errorf("ShimFolder of %s = %v, want %v", name, got[name], expected)
		}
	}
}
