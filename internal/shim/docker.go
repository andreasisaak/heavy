package shim

import (
	"strconv"

	"github.com/andreasisaak/heavy/internal/catalogue"
)

// dockerMayBeHeavy is false for docker calls without an exec or run word: only docker exec, docker run and their
// compose and container forms start a runner, so the others skip the verdict and the daemon.
func dockerMayBeHeavy(arguments []string) bool {
	for _, argument := range arguments {
		if argument == "exec" || argument == "run" {
			return true
		}
	}
	return false
}

// carryRun returns the arguments of a docker call inside a run with -e HEAVY_RUN=<id> -e HEAVY_CPUS=<cores> right
// after the exec or run word of `docker exec|run`, `docker container exec|run` and `docker compose exec|run`, so the
// processes in the container carry the run (design G6); any other call keeps its arguments.
func carryRun(arguments []string, id string, cores int) []string {
	rest := catalogue.SkipOptions(arguments, catalogue.DockerValues)
	switch {
	case len(rest) > 0 && rest[0] == "compose":
		rest = catalogue.SkipOptions(rest[1:], catalogue.ComposeValues)
	case len(rest) > 0 && rest[0] == "container":
		rest = rest[1:]
	}
	if len(rest) == 0 || (rest[0] != "exec" && rest[0] != "run") {
		return arguments
	}
	after := len(arguments) - len(rest) + 1
	carried := append([]string{}, arguments[:after]...)
	carried = append(carried, "-e", "HEAVY_RUN="+id, "-e", "HEAVY_CPUS="+strconv.Itoa(cores))
	return append(carried, arguments[after:]...)
}
