package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/detect"
	"github.com/andreasisaak/heavy/internal/sampler"
)

// measureDocker lets the daemon measure the Docker VM of the Mac through its helper container.
func measureDocker(options *daemon.Options, binary, stamp string) {
	options.VM, options.WithoutVM = vmOptions(binary, stamp)
}

// vmOptions reach the helper in the Docker VM: the real docker on PATH, bin/heavy-linux next to this binary, the
// image heavy-helper:<stamp> and a container named after the daemon (design G6). Without docker or without a build
// stamp the daemon measures no VM and says why.
func vmOptions(binary, stamp string) (*sampler.VMOptions, string) {
	docker, found := detect.RealProgram("docker", os.Getenv("PATH"))
	if !found {
		return nil, "docker not found"
	}
	if stamp == "" {
		return nil, "no build stamp in bin/.stamp"
	}
	return &sampler.VMOptions{
		Docker: docker, LinuxBinary: filepath.Join(filepath.Dir(binary), "heavy-linux"), Image: "heavy-helper:" + stamp,
		Container: fmt.Sprintf("heavy-helper-%d", os.Getpid()), Timeout: vmTimeout,
	}, ""
}
