//go:build !darwin && !linux

package main

import "github.com/andreasisaak/heavy/internal/daemon"

// measureDocker measures nothing: heavy knows docker on macOS and Linux only.
func measureDocker(options *daemon.Options, _, _ string) {
	options.WithoutVM = "heavy measures docker on macOS and Linux only"
}
