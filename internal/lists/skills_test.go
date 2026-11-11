package lists_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/andreasisaak/heavy/internal/lists"
)

// jsonBlocks are the fenced json blocks of a Markdown file.
var jsonBlocks = regexp.MustCompile("(?s)```json\n(.*?)```")

// plan 6.4: every example list in the skills loads as the list it shows, so a session that copies one gets a list
// the hook takes; the setup skill shows at least the entry fields red, exclusive and restart
func TestExampleListsOfTheSkillsLoad(t *testing.T) {
	found := 0
	for _, skill := range []string{"../../skills/setup/SKILL.md", "../../skills/herdr/SKILL.md"} {
		content, err := os.ReadFile(skill)
		if err != nil {
			t.Fatal(err)
		}
		for index, match := range jsonBlocks.FindAllSubmatch(content, -1) {
			found++
			path := filepath.Join(t.TempDir(), "list.json")
			if err := os.WriteFile(path, match[1], 0o644); err != nil {
				t.Fatal(err)
			}
			var keys map[string]json.RawMessage
			if err := json.Unmarshal(match[1], &keys); err != nil {
				t.Fatalf("%s block %d is no JSON object: %v", skill, index+1, err)
			}
			_, personal := keys["projects"]
			_, load := keys["load"]
			if personal || load {
				_, err = lists.LoadPersonal(path)
			} else {
				_, err = lists.LoadProjectList(path)
			}
			if err != nil {
				t.Errorf("%s block %d does not load: %v", skill, index+1, err)
			}
		}
	}
	setup, err := os.ReadFile("../../skills/setup/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"red"`, `"exclusive"`, `"restart"`} {
		if !regexp.MustCompile("(?s)```json\n[^`]*" + regexp.QuoteMeta(field)).Match(setup) {
			t.Errorf("the setup skill shows no example list with %s", field)
		}
	}
	if found == 0 {
		t.Fatalf("the skills hold no example list")
	}
}
