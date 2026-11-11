package shim

// informational are the options that only print the version or the help of a shimmed program.
var informational = map[string]bool{"--version": true, "-v": true, "-V": true, "--help": true, "-h": true}

// onlyInformational reports a call whose arguments only ask for the version or the help: it runs nothing, so it
// skips the lists, whose checkout lookup starts git and costs more than the call itself (NFR-1).
func onlyInformational(arguments []string) bool {
	for _, argument := range arguments {
		if !informational[argument] {
			return false
		}
	}
	return len(arguments) > 0
}
