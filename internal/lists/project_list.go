package lists

// ProjectListName is the file name of the project list at the root of a checkout.
const ProjectListName = ".heavy.json"

// LoadProjectList returns the commands of a .heavy.json, each split into words with its exceptions, patterns and
// flags, and the notes on what heavy left out of it.
func LoadProjectList(path string) (entries []Entry, warnings []string) {
	n := &notes{path: path}
	fields, found := readObject(n)
	if !found {
		return nil, n.warnings
	}
	commands, listed := fields["commands"]
	if !listed {
		n.unusable("commands", nil, "a list", "heavy reads no command from it")
	} else {
		entries = decodeEntries(commands, "commands", n)
	}
	for _, key := range sortedKeys(fields) {
		if key != "commands" {
			n.unknown(key)
		}
	}
	return entries, n.warnings
}
