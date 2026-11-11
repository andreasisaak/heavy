package catalogue

// Options of docker, docker compose, and docker exec and run that take the next word.
var (
	DockerValues  = set("-c", "--context", "-H", "--host", "-l", "--log-level", "--config")
	ComposeValues = set(
		"-f", "--file", "-p", "--project-name", "--project-directory", "--env-file", "--profile", "--ansi",
		"--progress", "--parallel",
	)
	ExecValues = set("-e", "--env", "--env-file", "-u", "--user", "-w", "--workdir", "--detach-keys", "--index")
	RunValues  = union(ExecValues, set(
		"-v", "--volume", "-p", "--publish", "--name", "--entrypoint", "-l", "--label", "--network", "--net",
		"--platform", "--mount", "--cpus", "-m", "--memory", "--add-host", "-h", "--hostname", "--pull",
		"--restart", "--shm-size", "--tmpfs", "--ulimit", "--cap-add", "--cap-drop", "--device", "--gpus",
		"--ipc", "--pid", "--log-driver", "--log-opt", "--volumes-from", "--expose", "--dns", "--security-opt",
		"--stop-signal", "--stop-timeout", "--userns", "--cidfile", "--memory-swap", "--cpuset-cpus",
		"--env-from-file", "--label-file", "--link", "--runtime",
	))
)

// docker starts the command of `docker [compose|container] exec|run [options] <target> <command>`.
func docker(arguments []string) []target {
	rest := SkipOptions(arguments, DockerValues)
	if len(rest) > 0 && rest[0] == "compose" {
		rest = SkipOptions(rest[1:], ComposeValues)
	} else if len(rest) > 0 && rest[0] == "container" {
		rest = rest[1:]
	}
	if len(rest) == 0 || (rest[0] != "exec" && rest[0] != "run") {
		return nil
	}
	values := ExecValues
	if rest[0] == "run" {
		values = RunValues
	}
	container := SkipOptions(rest[1:], values)
	if len(container) < 2 {
		return nil
	}
	return []target{{command: container[1:]}}
}

func union(sets ...map[string]bool) map[string]bool {
	members := map[string]bool{}
	for _, current := range sets {
		for word := range current {
			members[word] = true
		}
	}
	return members
}
