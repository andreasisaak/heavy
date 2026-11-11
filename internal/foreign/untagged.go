package foreign

import "github.com/andreasisaak/heavy/internal/helper"

// Untagged is the memory of each container without the processes that carry the tag of a run: a run that works in
// a container started before it (docker compose exec) brings its own load, which is no foreign load (design G6).
func Untagged(usage []helper.Container, tagged []helper.Process) []helper.Container {
	byContainer := map[string]int64{}
	for _, process := range tagged {
		if process.Container != "" {
			byContainer[process.Container] += process.RSSBytes
		}
	}
	untagged := make([]helper.Container, len(usage))
	for index, container := range usage {
		untagged[index] = helper.Container{ID: container.ID, RSSBytes: max(0, container.RSSBytes-byContainer[container.ID])}
	}
	return untagged
}
