package shell

// FromArgv returns the script of one simple command whose words the shell has already split, such as the
// arguments a shim or heavy itself gets; the commands of its shell string follow it. None of its commands has
// an offset or sits in a top-level list.
func FromArgv(argv []string) Script {
	found := &state{}
	current := &walker{state: found, parent: -1}
	current.add(argv, Normalize(argv), -1, placeOther, false)
	return Script{Commands: found.commands}
}
