package main

import (
	"os"
	"syscall"

	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/detect"
	"github.com/andreasisaak/heavy/internal/sampler"
)

// measureDocker lets the daemon measure the machine itself: on Linux docker runs without a VM, so the containers
// are processes of this machine, and of this user with rootless docker. Without docker on PATH the daemon still
// measures, and reaches no container.
func measureDocker(options *daemon.Options, _, _ string) {
	options.Local = &sampler.LocalOptions{ProcRoot: "/proc", CgroupRoot: "/sys/fs/cgroup", PageSize: int64(os.Getpagesize()), Kill: syscall.Kill}
	if docker, found := detect.RealProgram("docker", os.Getenv("PATH")); found {
		options.Docker, options.DockerTimeout = docker, vmTimeout
	}
}
