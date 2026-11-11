package slots_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andreasisaak/heavy/internal/slots"
)

func configFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadReadsTheLimitAndTheSlotFolder(t *testing.T) {
	path := configFile(t, `{"max_parallel": 8, "slots": "/var/lib/heavy/slots"}`)

	config, found, err := slots.Load(path)

	want := slots.Config{Path: path, MaxParallel: 8, Folder: "/var/lib/heavy/slots"}
	if err != nil || !found || config != want {
		t.Fatalf("Load = %#v, %v, %v, want %#v, true", config, found, err, want)
	}
}

func TestLoadWithoutTheFileFindsNoMachineLimit(t *testing.T) {
	config, found, err := slots.Load(filepath.Join(t.TempDir(), "config.json"))

	if err != nil || found || config != (slots.Config{}) {
		t.Fatalf("Load = %#v, %v, %v, want no config and no error", config, found, err)
	}
}

func TestLoadRejectsEveryOtherShape(t *testing.T) {
	cases := map[string]string{
		"no limit":            `{"slots": "/var/lib/heavy/slots"}`,
		"no folder":           `{"max_parallel": 8}`,
		"limit of zero":       `{"max_parallel": 0, "slots": "/var/lib/heavy/slots"}`,
		"limit as text":       `{"max_parallel": "8", "slots": "/var/lib/heavy/slots"}`,
		"limit with fraction": `{"max_parallel": 2.5, "slots": "/var/lib/heavy/slots"}`,
		"relative folder":     `{"max_parallel": 8, "slots": "slots"}`,
		"null folder":         `{"max_parallel": 8, "slots": null}`,
		"an array":            `[8]`,
		"two documents":       `{"max_parallel": 8, "slots": "/s"} {}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := configFile(t, content)

			_, found, err := slots.Load(path)

			want := "heavy: " + path + `: expected {"max_parallel": 8, "slots": "/var/lib/heavy/slots"} with both keys, "max_parallel" a whole number from 1 and "slots" an absolute folder, got ` + content
			if err == nil || found || err.Error() != want {
				t.Fatalf("Load error = %v, found %v, want %q", err, found, want)
			}
		})
	}
}

// A newer heavy may know more keys of the machine configuration; an older one on the same machine reads the rest.
func TestLoadIgnoresAKeyOfANewerHeavy(t *testing.T) {
	path := configFile(t, `{"max_parallel": 8, "slots": "/var/lib/heavy/slots", "fair": true}`)

	config, found, err := slots.Load(path)

	want := slots.Config{Path: path, MaxParallel: 8, Folder: "/var/lib/heavy/slots"}
	if err != nil || !found || config != want {
		t.Fatalf("Load = %#v, %v, %v, want %#v, true", config, found, err, want)
	}
}

func TestLoadOfAnUnreadableFileIsAnError(t *testing.T) {
	path := t.TempDir()

	_, found, err := slots.Load(path)

	want := "heavy: reading the machine configuration " + path + ": read " + path + ": is a directory"
	if err == nil || found || err.Error() != want {
		t.Fatalf("Load error = %v, found %v, want %q", err, found, want)
	}
}

func TestConfigPathIsTheFileBelowEtcUnlessTheEnvironmentNamesAnother(t *testing.T) {
	t.Setenv("HEAVY_MACHINE_CONFIG", "")
	if path := slots.ConfigPath(); path != "/etc/heavy/config.json" {
		t.Fatalf("ConfigPath = %q, want /etc/heavy/config.json", path)
	}
	t.Setenv("HEAVY_MACHINE_CONFIG", "/tmp/machine.json")
	if path := slots.ConfigPath(); path != "/tmp/machine.json" {
		t.Fatalf("ConfigPath = %q, want /tmp/machine.json", path)
	}
}
