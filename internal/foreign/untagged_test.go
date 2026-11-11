package foreign_test

import (
	"reflect"
	"testing"

	"github.com/andreasisaak/heavy/internal/foreign"
	"github.com/andreasisaak/heavy/internal/helper"
)

// critic 5.1: a run's tagged processes inside a container that ran before the run (docker compose exec) are the
// run's load, not foreign load, so they leave the container's memory
func TestUntaggedTakesTheTaggedProcessesOutOfTheirContainers(t *testing.T) {
	usage := []helper.Container{{ID: "app", RSSBytes: 3 << 30}, {ID: "db", RSSBytes: 1 << 30}}
	tagged := []helper.Process{
		{PID: 7, Container: "app", RSSBytes: 2 << 30},
		{PID: 8, Container: "app", RSSBytes: 512 << 20},
		{PID: 9, Container: "", RSSBytes: 4 << 30},
	}

	got := foreign.Untagged(usage, tagged)

	want := []helper.Container{{ID: "app", RSSBytes: 512 << 20}, {ID: "db", RSSBytes: 1 << 30}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Untagged = %+v, want %+v", got, want)
	}
	if usage[0].RSSBytes != 3<<30 {
		t.Fatalf("Untagged changed its input: %+v", usage)
	}
}
