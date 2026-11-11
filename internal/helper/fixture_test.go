package helper_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// containerA and containerB are container ids in the cgroup paths of the fixtures.
const (
	containerA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	containerB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// fixture writes a /proc tree: every key is a path below the root, every value the file's content.
func fixture(t *testing.T, files map[string]string) string {
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

// process returns the files of one process: its environment, resident memory in kB and cgroup line.
func process(pid string, environment []string, rssKilobytes string, cgroup string) map[string]string {
	status := "Name:\tphp\nState:\tS (sleeping)\n"
	if rssKilobytes != "" {
		status += "VmRSS:\t   " + rssKilobytes + " kB\n"
	}
	status += "Threads:\t1\n"
	return map[string]string{
		pid + "/environ": strings.Join(environment, "\x00") + "\x00",
		pid + "/status":  status,
		pid + "/cgroup":  cgroup,
	}
}

func merge(parts ...map[string]string) map[string]string {
	merged := map[string]string{}
	for _, part := range parts {
		for name, content := range part {
			merged[name] = content
		}
	}
	return merged
}

// hostFiles are the machine-wide files of the fixture /proc.
func hostFiles() map[string]string {
	return map[string]string{
		"meminfo":         "MemTotal:       25165824 kB\nMemFree:          524288 kB\nMemAvailable:    3145728 kB\nBuffers:           10240 kB\n",
		"vmstat":          "nr_free_pages 131072\npswpin 12\npswpout 2500\npgfault 99\n",
		"loadavg":         "3.52 2.10 1.75 4/512 98765\n",
		"cpuinfo":         "processor\t: 0\nBogoMIPS\t: 48.00\n\nprocessor\t: 1\nBogoMIPS\t: 48.00\n\nprocessor\t: 2\n\nprocessor\t: 3\n",
		"pressure/memory": "some avg10=1.50 avg60=0.80 avg300=0.20 total=123\nfull avg10=0.75 avg60=0.40 avg300=0.10 total=45\n",
		"pressure/cpu":    "some avg10=12.00 avg60=8.00 avg300=4.00 total=999\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n",
		"pressure/io":     "some avg10=0.25 avg60=0.00 avg300=0.00 total=7\nfull avg10=0.10 avg60=0.00 avg300=0.00 total=3\n",
	}
}
