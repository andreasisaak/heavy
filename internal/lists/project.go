package lists

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Project is heavy in one checkout: whether the catalogue of heavy runners counts, and the listed commands.
// The zero value is heavy off.
type Project struct {
	Catalogue bool
	Commands  []Entry
}

// ProjectOf returns heavy in directory: its checkout's .heavy.json plus the personal entry of its main checkout.
// Either list turns the catalogue on; "enabled": false in the personal entry turns the .heavy.json and the
// catalogue off and keeps the personal commands. Outside a checkout heavy is off.
func ProjectOf(directory string, personal Personal) (Project, error) {
	checkout, found, err := FindCheckout(directory)
	if err != nil || !found {
		return Project{}, err
	}
	current, switched := personal.Projects[checkout.Main]
	if switched && !current.Enabled {
		return Project{Catalogue: false, Commands: current.Commands}, nil
	}
	path := filepath.Join(checkout.Toplevel, ProjectListName)
	information, err := os.Stat(path)
	listed := err == nil && information.Mode().IsRegular()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Project{}, failure("%v", err)
	}
	var commands []Entry
	if listed {
		if commands, err = LoadProjectList(path); err != nil {
			return Project{}, err
		}
	}
	return Project{Catalogue: listed || switched, Commands: append(commands, current.Commands...)}, nil
}
