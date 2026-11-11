package main

import (
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/andreasisaak/heavy/internal/helper"
)

// runHelper is `heavy --helper [--root <dir>]`: the side of heavy in the Docker VM, answering on stdin and stdout.
func runHelper(arguments []string, stdout, stderr io.Writer) int {
	root := "/proc"
	for len(arguments) > 0 {
		if arguments[0] != "--root" {
			fmt.Fprintf(stderr, "heavy: unknown option %s. %s\n", arguments[0], usage)
			return exitUsage
		}
		if len(arguments) < 2 {
			fmt.Fprintf(stderr, "heavy: --root needs a directory. %s\n", usage)
			return exitUsage
		}
		root, arguments = arguments[1], arguments[2:]
	}
	if err := helper.Serve(helper.Options{Root: root, PageSize: int64(os.Getpagesize()), Kill: syscall.Kill}, os.Stdin, stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
