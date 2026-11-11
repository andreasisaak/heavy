//go:build darwin

package sampler_test

import (
	"testing"

	"github.com/andreasisaak/heavy/internal/sampler"
	"github.com/andreasisaak/heavy/internal/testkit"
)

func TestReadHostOnThisMac(t *testing.T) {
	sample, err := sampler.ReadHost()

	if err != nil {
		t.Fatalf("ReadHost failed: %v", err)
	}
	t.Logf("host sample: %+v", sample)
	if sample.FreePercent < 0 || sample.FreePercent > 100 || sample.Cores < 1 || sample.MemoryBytes < 1<<30 || sample.PageSize < 4096 {
		t.Fatalf("ReadHost = %#v, want a level from 0 to 100, cores, memory and a page size", sample)
	}
}

func TestReadHostWithoutVMStatIsAnError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := sampler.ReadHost()

	want := `heavy: vm_stat failed: exec: "vm_stat": executable file not found in $PATH`
	if err == nil || err.Error() != want {
		t.Fatalf("ReadHost error = %v, want %q", err, want)
	}
}

func TestReadHostWithAFailingVMStatIsAnError(t *testing.T) {
	directory := t.TempDir()
	testkit.FakeProgram(t, directory, "vm_stat", "echo broken >&2; exit 3")
	t.Setenv("PATH", directory)

	_, err := sampler.ReadHost()

	want := "heavy: vm_stat failed: exit status 3"
	if err == nil || err.Error() != want {
		t.Fatalf("ReadHost error = %v, want %q", err, want)
	}
}
