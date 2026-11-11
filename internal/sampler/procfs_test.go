package sampler_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/andreasisaak/heavy/internal/sampler"
)

// procRoot writes a /proc tree: every key is a path below the root, every value the file's content.
func procRoot(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// boxFiles are the machine-wide files of a Linux box with 125 GiB, 16 GiB swap and four processors.
func boxFiles() map[string]string {
	return map[string]string{
		"meminfo": "MemTotal:       131072000 kB\nMemFree:         1048576 kB\nMemAvailable:   91750400 kB\n" +
			"SwapTotal:      16777216 kB\nSwapFree:       16252928 kB\n",
		"vmstat":  "nr_free_pages 131072\npswpin 12\npswpout 2500\n",
		"loadavg": "14.80 9.10 4.75 9/2048 98765\n",
		"cpuinfo": "processor\t: 0\n\nprocessor\t: 1\n\nprocessor\t: 2\n\nprocessor\t: 3\n",
	}
}

func TestReadProcHostReadsMemorySwapLoadAndProcessors(t *testing.T) {
	root := procRoot(t, boxFiles())

	sample, err := sampler.ReadProcHost(root, 4096)

	if err != nil {
		t.Fatalf("ReadProcHost failed: %v", err)
	}
	sample.Time = sample.Time.Truncate(0)
	if sample.Time.IsZero() {
		t.Fatal("ReadProcHost left the time of the sample zero")
	}
	want := sampler.HostSample{
		Time: sample.Time, FreePercent: 70, MemoryBytes: 131072000 * 1024, Cores: 4, Load1: 14.8,
		SwapUsedBytes: 524288 * 1024, PageSize: 4096, SwapOutPages: 2500,
	}
	if !reflect.DeepEqual(sample, want) {
		t.Fatalf("ReadProcHost = %#v, want %#v", sample, want)
	}
}

func TestReadProcHostWithoutMemAvailableIsAnError(t *testing.T) {
	files := boxFiles()
	files["meminfo"] = "MemTotal:       131072000 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n"
	root := procRoot(t, files)

	_, err := sampler.ReadProcHost(root, 4096)

	want := "heavy: " + root + "/meminfo: expected a line MemAvailable: <n> kB, found none"
	if err == nil || err.Error() != want {
		t.Fatalf("ReadProcHost error = %v, want %q", err, want)
	}
}

func TestReadProcHostWithoutSwapLinesIsAnError(t *testing.T) {
	files := boxFiles()
	files["meminfo"] = "MemTotal:       131072000 kB\nMemAvailable:   91750400 kB\n"
	root := procRoot(t, files)

	_, err := sampler.ReadProcHost(root, 4096)

	want := "heavy: " + root + "/meminfo: expected a line SwapTotal: <n> kB, found none"
	if err == nil || err.Error() != want {
		t.Fatalf("ReadProcHost error = %v, want %q", err, want)
	}
}

func TestReadProcHostWithoutLoadavgIsAnError(t *testing.T) {
	files := boxFiles()
	delete(files, "loadavg")
	root := procRoot(t, files)

	_, err := sampler.ReadProcHost(root, 4096)

	want := "heavy: reading " + root + "/loadavg: open " + root + "/loadavg: no such file or directory"
	if err == nil || err.Error() != want {
		t.Fatalf("ReadProcHost error = %v, want %q", err, want)
	}
}

func TestReadProcHostWithoutProcessorsIsAnError(t *testing.T) {
	files := boxFiles()
	files["cpuinfo"] = "model name\t: none\n"
	root := procRoot(t, files)

	_, err := sampler.ReadProcHost(root, 4096)

	want := "heavy: " + root + "/cpuinfo: expected processor lines, found none"
	if err == nil || err.Error() != want {
		t.Fatalf("ReadProcHost error = %v, want %q", err, want)
	}
}
