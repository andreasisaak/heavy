package foreign

import (
	"log/slog"
	"maps"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/helper"
)

func TestNotifierForgetsAContainerFifteenMinutesAfterItsNotice(t *testing.T) {
	start := time.Date(2026, 11, 8, 14, 0, 0, 0, time.UTC)
	id := strings.Repeat("a", 64)
	notifier := NewNotifier(func(string) (string, error) { return "/sites/shopfront", nil })
	log := slog.New(slog.DiscardHandler)
	container := Container{ID: id, Name: "shopfront-spa-watch-1", Service: "spa-watch", WorkingDirectory: "/sites/shopfront-csv-export"}

	notifier.Notices(start, 1<<30, []helper.Container{{ID: id, RSSBytes: 3 << 30}}, []Container{container}, map[string]bool{}, log)
	notifier.Notices(start.Add(10*time.Minute), 1<<30, nil, nil, map[string]bool{}, log)
	kept := maps.Clone(notifier.considered)
	notifier.Notices(start.Add(15*time.Minute), 1<<30, nil, nil, map[string]bool{}, log)

	if want := map[string]time.Time{id: start}; !reflect.DeepEqual(kept, want) {
		t.Fatalf("remembered after 10 min %v, want %v", kept, want)
	}
	if !reflect.DeepEqual(notifier.considered, map[string]time.Time{}) {
		t.Fatalf("remembered after 15 min %v, want nothing", notifier.considered)
	}
}
