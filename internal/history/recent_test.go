package history_test

import (
	"reflect"
	"testing"

	"github.com/andreasisaak/heavy/internal/history"
)

// design G13: the dashboard shows the last results, newest first, read from the newest files only
func TestRecentGivesTheNewestRecordsFirst(t *testing.T) {
	directory := t.TempDir()
	for _, record := range []history.Record{
		{ID: "a", Label: "old", Ended: 1},
		{ID: "b", Label: "middle", Ended: 2},
		{ID: "c", Label: "new", Ended: 3},
	} {
		if err := history.Append(directory, record); err != nil {
			t.Fatal(err)
		}
	}

	got, err := history.Recent(directory, 2)

	var ids []string
	for _, record := range got {
		ids = append(ids, record.ID)
	}
	if err != nil || !reflect.DeepEqual(ids, []string{"c", "b"}) {
		t.Fatalf("Recent = %v, %v; want c then b", ids, err)
	}
}

func TestRecentOfNoHistoryIsEmpty(t *testing.T) {
	got, err := history.Recent(t.TempDir(), 5)

	if err != nil || len(got) != 0 {
		t.Fatalf("Recent = %v, %v; want nothing", got, err)
	}
}
