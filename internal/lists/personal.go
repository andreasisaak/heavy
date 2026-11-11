package lists

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Switch is the personal entry of one main checkout: whether its .heavy.json counts, and commands added to it.
type Switch struct {
	Enabled  bool
	Commands []Entry
}

// Personal is the personal list: switches keyed by resolved main checkout, and the load thresholds. Warnings name
// what heavy left out of the file; Broken says that a value or the file itself could not be used, which a key
// heavy does not know alone does not.
type Personal struct {
	Projects map[string]Switch
	Load     Load
	Warnings []string
	Broken   bool
}

// PersonalPath is the path of the personal list in the home directory.
func PersonalPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", failure("finding the home directory for the personal list: %v", err)
	}
	return filepath.Join(home, ".config", "heavy", "config.json"), nil
}

// LoadPersonal reads the personal list as far as it can be read; no file switches nothing and keeps the default
// thresholds. The list never fails a call: what heavy does not know or cannot use is in Warnings.
func LoadPersonal(path string) Personal {
	personal := Personal{Projects: map[string]Switch{}, Load: DefaultLoad()}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return personal
	}
	n := &notes{path: path}
	fields, found := readObject(n)
	for _, key := range sortedKeys(fields) {
		switch key {
		case "load":
			personal.Load = decodeLoad(fields[key], n)
		case "projects":
			decodeProjects(fields[key], personal.Projects, n)
		default:
			n.unknown(key)
		}
	}
	personal.Warnings, personal.Broken = n.warnings, n.broken || !found
	return personal
}

// decodeProjects reads the switches of the "projects" section into switches, keyed by resolved main checkout.
func decodeProjects(raw json.RawMessage, switches map[string]Switch, n *notes) {
	projects, isObject := decodeObject(raw)
	if !isObject {
		n.unusable("projects", raw, "an object", "heavy reads no project from it")
		return
	}
	for _, checkout := range sortedKeys(projects) {
		if current, valid := decodeSwitch(projects[checkout], "projects."+checkout, n); valid {
			switches[realPath(expandHome(checkout))] = current
		}
	}
}

// decodeSwitch reads the entry of one project; one heavy cannot use is noted and left out.
func decodeSwitch(raw json.RawMessage, where string, n *notes) (Switch, bool) {
	fields, isObject := decodeObject(raw)
	if !isObject {
		n.unusable(where, raw, "an object", "heavy leaves this project out")
		return Switch{}, false
	}
	current := Switch{Enabled: true}
	if value, found := fields["enabled"]; found && !decodeBool(value, &current.Enabled) {
		n.unusable(where+".enabled", value, "true or false", "heavy leaves this project out")
		return Switch{}, false
	}
	for _, key := range sortedKeys(fields) {
		switch key {
		case "enabled":
		case "commands":
			current.Commands = decodeEntries(fields[key], where+".commands", n)
		default:
			n.unknown(where + "." + key)
		}
	}
	return current, true
}

// expandHome replaces a leading ~ with the home directory, as os.path.expanduser did.
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	return os.Getenv("HOME") + path[1:]
}
