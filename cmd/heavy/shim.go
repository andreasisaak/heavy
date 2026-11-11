package main

import (
	"fmt"
	"io"
	"os"

	"github.com/andreasisaak/heavy/internal/shim"
)

// runShim is heavy started under the name of a program it shims (internal/shim): it replaces this process with the
// real program or with `heavy --directory <dir> <program> …` and returns only when it cannot. The shim writes
// nothing to standard output; that belongs to the program.
func runShim(program string, arguments []string, _, stderr io.Writer) int {
	if !shim.IsProgram(program) {
		fmt.Fprintf(stderr, "heavy: started as %s, which is no program heavy shims. %s\n", program, usage)
		return exitUsage
	}
	binary, err := ownBinary()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	directory, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "heavy: finding the working directory: %v\n", err)
		return 1
	}
	return shim.Run(shim.Call{
		Program: program, Arguments: arguments, Binary: binary, Directory: directory, Environment: os.Environ(),
		Connect: connectOptions, Stderr: stderr,
	})
}
