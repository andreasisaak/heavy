package shim

import (
	"reflect"
	"strings"
	"testing"
)

// FR-11: -e HEAVY_RUN=<id> -e HEAVY_CPUS=<n> right after the exec or run word of docker, docker container and
// docker compose, behind the options of docker and compose.
func TestCarryRunInsertsTheRunAfterExecOrRun(t *testing.T) {
	cases := []struct {
		arguments string
		want      string
	}{
		{"compose -f x.yml exec -T app php a", "compose -f x.yml exec -e HEAVY_RUN=r7 -e HEAVY_CPUS=4 -T app php a"},
		{"run --rm alpine true", "run -e HEAVY_RUN=r7 -e HEAVY_CPUS=4 --rm alpine true"},
		{"container exec c ls", "container exec -e HEAVY_RUN=r7 -e HEAVY_CPUS=4 c ls"},
		{"container run --rm alpine", "container run -e HEAVY_RUN=r7 -e HEAVY_CPUS=4 --rm alpine"},
		{"exec c ls", "exec -e HEAVY_RUN=r7 -e HEAVY_CPUS=4 c ls"},
		{"--context x -H unix:///d.sock exec c ls", "--context x -H unix:///d.sock exec -e HEAVY_RUN=r7 -e HEAVY_CPUS=4 c ls"},
		{"--log-level=debug exec c ls", "--log-level=debug exec -e HEAVY_RUN=r7 -e HEAVY_CPUS=4 c ls"},
		{"compose --project-directory d -p name run app", "compose --project-directory d -p name run -e HEAVY_RUN=r7 -e HEAVY_CPUS=4 app"},
		{"compose --file=x.yml --profile test exec app", "compose --file=x.yml --profile test exec -e HEAVY_RUN=r7 -e HEAVY_CPUS=4 app"},
		{"compose exec", "compose exec -e HEAVY_RUN=r7 -e HEAVY_CPUS=4"},
		{"ps", "ps"},
		{"compose ps", "compose ps"},
		{"compose up -d run", "compose up -d run"},
		{"container ls", "container ls"},
		{"logs run", "logs run"},
		{"image inspect exec", "image inspect exec"},
		{"", ""},
	}
	for _, current := range cases {
		got := carryRun(strings.Fields(current.arguments), "r7", 4)

		if want := strings.Fields(current.want); !reflect.DeepEqual(got, want) {
			t.Errorf("carryRun(%q) = %q, want %q", current.arguments, got, want)
		}
	}
}

// The arguments the call came with stay as they were: the shim passes a new list on.
func TestCarryRunKeepsTheArgumentsOfTheCall(t *testing.T) {
	arguments := make([]string, 4, 8)
	copy(arguments, []string{"run", "--rm", "alpine", "true"})

	carryRun(arguments, "r7", 4)

	if want := []string{"run", "--rm", "alpine", "true"}; !reflect.DeepEqual(arguments, want) {
		t.Fatalf("arguments after carryRun = %q, want %q", arguments, want)
	}
}
