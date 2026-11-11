package lists

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// PersonalForm is the shape ~/.config/heavy/config.json must have, as error messages name it.
const PersonalForm = `{"projects": {"<main checkout>": {"enabled": true, "commands": ["make ci"]}}, "load": {"max_parallel": 3}} ` +
	`with every key optional, each command a string or an object as in .heavy.json, and "load" with "max_parallel" (from 1), ` +
	`"reserve_bytes", "vm_pressure_bytes", "vm_recovered_bytes", "swap_bytes_per_2s", "unpredicted_peak_bytes" (whole bytes from 0), ` +
	`"host_pressure_percent", "host_recovered_percent" (0 to 100) and "load_per_core" (from 0)`

// Switch is the personal entry of one main checkout: whether its .heavy.json counts, and commands added to it.
type Switch struct {
	Enabled  bool
	Commands []Entry
}

// Personal is the personal list: switches keyed by resolved main checkout, and the load thresholds.
type Personal struct {
	Projects map[string]Switch
	Load     Load
}

var (
	personalFields = map[string]bool{"projects": true, "load": true}
	switchFields   = map[string]bool{"enabled": true, "commands": true}
)

// PersonalPath is the path of the personal list in the home directory.
func PersonalPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", failure("finding the home directory for the personal list: %v", err)
	}
	return filepath.Join(home, ".config", "heavy", "config.json"), nil
}

// LoadPersonal reads the personal list; no file switches nothing and keeps the default thresholds.
func LoadPersonal(path string) (Personal, error) {
	personal := Personal{Projects: map[string]Switch{}, Load: DefaultLoad()}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return personal, nil
	}
	document, err := readJSON(path, PersonalForm)
	if err != nil {
		return Personal{}, err
	}
	fields, isObject := decodeObject(document.data)
	if !isObject || !onlyKeys(fields, personalFields) {
		return Personal{}, document.shapeError()
	}
	if raw, found := fields["load"]; found {
		load, valid := decodeLoad(raw)
		if !valid {
			return Personal{}, document.shapeError()
		}
		personal.Load = load
	}
	if raw, found := fields["projects"]; found {
		projects, isObject := decodeObject(raw)
		if !isObject {
			return Personal{}, document.shapeError()
		}
		for checkout, value := range projects {
			current, err := decodeSwitch(document, value)
			if err != nil {
				return Personal{}, err
			}
			personal.Projects[realPath(expandHome(checkout))] = current
		}
	}
	return personal, nil
}

func decodeSwitch(document document, raw []byte) (Switch, error) {
	fields, isObject := decodeObject(raw)
	if !isObject || !onlyKeys(fields, switchFields) {
		return Switch{}, document.shapeError()
	}
	current := Switch{Enabled: true}
	if value, found := fields["enabled"]; found && !decodeBool(value, &current.Enabled) {
		return Switch{}, document.shapeError()
	}
	if value, found := fields["commands"]; found {
		commands, err := decodeEntries(document, value)
		if err != nil {
			return Switch{}, err
		}
		current.Commands = commands
	}
	return current, nil
}

// expandHome replaces a leading ~ with the home directory, as os.path.expanduser did.
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	return os.Getenv("HOME") + path[1:]
}
