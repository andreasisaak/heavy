package foreign_test

import (
	"bytes"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/foreign"
	"github.com/andreasisaak/heavy/internal/helper"
	"github.com/andreasisaak/heavy/internal/platform"
	"github.com/andreasisaak/heavy/internal/protocol"
)

const gibibyte = int64(1) << 30

var (
	start = time.Date(2026, 11, 8, 14, 0, 0, 0, time.UTC)
	// vmFree is 1.4 GiB, printed with one decimal.
	vmFree = gibibyte * 14 / 10
	spa    = foreign.Container{ID: identifier('a'), Name: "shopfront-spa-watch-1", Service: "spa-watch", WorkingDirectory: "/sites/shopfront-csv-export"}
	// spaLine is the notice about spa using 1.5 GiB while 1.4 GiB of the VM are free.
	spaLine  = "heavy: memory is low (" + platform.Docker + " 1.4 GiB free); shopfront-spa-watch-1 of this repository uses 1.5 GiB. If you do not need it now: docker compose --project-directory /sites/shopfront-csv-export stop spa-watch"
	spaEvent = protocol.Event{Checkout: "/sites/shopfront", Line: spaLine}
)

// identifier is a full container id of 64 hex digits made of one digit.
func identifier(digit byte) string {
	return strings.Repeat(string(digit), 64)
}

func resident(id string, rssBytes int64) []helper.Container {
	return []helper.Container{{ID: id, RSSBytes: rssBytes}}
}

// logger writes JSON records without their time into a buffer.
func logger() (*slog.Logger, *bytes.Buffer) {
	var buffer bytes.Buffer
	handler := slog.NewJSONHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelDebug, ReplaceAttr: func(groups []string, attribute slog.Attr) slog.Attr {
		if attribute.Key == slog.TimeKey && len(groups) == 0 {
			return slog.Attr{}
		}
		return attribute
	}})
	return slog.New(handler), &buffer
}

// fakeCheckouts answers the main checkout of a directory from a table, "" for a directory in no checkout, and
// records which directories it was asked for.
type fakeCheckouts struct {
	main     map[string]string
	failures map[string]error
	asked    []string
}

func (f *fakeCheckouts) lookup(directory string) (string, error) {
	f.asked = append(f.asked, directory)
	if err, failed := f.failures[directory]; failed {
		return "", err
	}
	return f.main[directory], nil
}

func shopCheckouts() *fakeCheckouts {
	return &fakeCheckouts{main: map[string]string{"/sites/shopfront-csv-export": "/sites/shopfront"}}
}

func TestContainerWithoutARunUnderPressureGetsOneNotice(t *testing.T) {
	checkouts := shopCheckouts()
	log, records := logger()

	events := foreign.NewNotifier(checkouts.lookup).Notices(start, vmFree, resident(spa.ID, gibibyte*15/10), []foreign.Container{spa}, map[string]bool{}, log)

	if want := []protocol.Event{spaEvent}; !reflect.DeepEqual(events, want) {
		t.Fatalf("Notices = %#v, want %#v", events, want)
	}
	if want := []string{"/sites/shopfront-csv-export"}; !reflect.DeepEqual(checkouts.asked, want) || records.Len() != 0 {
		t.Fatalf("asked for %q and logged %q, want %q and nothing", checkouts.asked, records.String(), want)
	}
}

func TestNoticeOfAContainerComesAtMostEveryFifteenMinutes(t *testing.T) {
	notifier := foreign.NewNotifier(shopCheckouts().lookup)
	log, _ := logger()
	notices := func(at time.Time) []protocol.Event {
		return notifier.Notices(at, vmFree, resident(spa.ID, gibibyte*15/10), []foreign.Container{spa}, map[string]bool{}, log)
	}

	got := [][]protocol.Event{notices(start), notices(start.Add(10 * time.Minute)), notices(start.Add(16 * time.Minute))}

	if want := [][]protocol.Event{{spaEvent}, nil, {spaEvent}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Notices at 0, 10 and 16 min = %#v, want %#v", got, want)
	}
}

func TestNoticeComesAgainAfterExactlyFifteenMinutes(t *testing.T) {
	notifier := foreign.NewNotifier(shopCheckouts().lookup)
	log, _ := logger()
	notices := func(at time.Time) []protocol.Event {
		return notifier.Notices(at, vmFree, resident(spa.ID, gibibyte*15/10), []foreign.Container{spa}, map[string]bool{}, log)
	}

	got := [][]protocol.Event{notices(start), notices(start.Add(15 * time.Minute))}

	if want := [][]protocol.Event{{spaEvent}, {spaEvent}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Notices at 0 and 15 min = %#v, want %#v", got, want)
	}
}

func TestContainerOfExactlyOneGibibyteGetsANotice(t *testing.T) {
	log, _ := logger()

	events := foreign.NewNotifier(shopCheckouts().lookup).Notices(start, vmFree, resident(spa.ID, gibibyte), []foreign.Container{spa}, map[string]bool{}, log)

	line := "heavy: memory is low (" + platform.Docker + " 1.4 GiB free); shopfront-spa-watch-1 of this repository uses 1.0 GiB. If you do not need it now: docker compose --project-directory /sites/shopfront-csv-export stop spa-watch"
	if want := []protocol.Event{{Checkout: "/sites/shopfront", Line: line}}; !reflect.DeepEqual(events, want) {
		t.Fatalf("Notices = %#v, want %#v", events, want)
	}
}

func TestContainersThatGetNoNotice(t *testing.T) {
	withoutDirectory := spa
	withoutDirectory.WorkingDirectory = ""
	cases := []struct {
		name      string
		usage     []helper.Container
		container foreign.Container
		owned     map[string]bool
	}{
		{"below one gibibyte", resident(spa.ID, gibibyte*9/10), spa, map[string]bool{}},
		{"of a run", resident(spa.ID, gibibyte*15/10), spa, map[string]bool{spa.ID: true}},
		{"without compose working directory", resident(spa.ID, gibibyte*15/10), withoutDirectory, map[string]bool{}},
		{"absent from the usage", resident(identifier('f'), gibibyte*15/10), spa, map[string]bool{}},
	}
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			checkouts := shopCheckouts()
			log, records := logger()

			events := foreign.NewNotifier(checkouts.lookup).Notices(start, vmFree, current.usage, []foreign.Container{current.container}, current.owned, log)

			if events != nil || checkouts.asked != nil || records.Len() != 0 {
				t.Fatalf("Notices = %#v, asked for %q, logged %q; want no notice, no question, no record", events, checkouts.asked, records.String())
			}
		})
	}
}

func TestContainerWithoutComposeServiceGetsTheDockerStopHint(t *testing.T) {
	lonely := spa
	lonely.Service = ""
	log, _ := logger()

	events := foreign.NewNotifier(shopCheckouts().lookup).Notices(start, vmFree, resident(spa.ID, gibibyte*15/10), []foreign.Container{lonely}, map[string]bool{}, log)

	line := "heavy: memory is low (" + platform.Docker + " 1.4 GiB free); shopfront-spa-watch-1 of this repository uses 1.5 GiB. If you do not need it now: docker stop shopfront-spa-watch-1"
	if want := []protocol.Event{{Checkout: "/sites/shopfront", Line: line}}; !reflect.DeepEqual(events, want) {
		t.Fatalf("Notices = %#v, want %#v", events, want)
	}
}

func TestNoticeQuotesAWorkingDirectoryThatNeedsQuoting(t *testing.T) {
	copied := spa
	copied.WorkingDirectory = "/sites/shopfront copy"
	checkouts := &fakeCheckouts{main: map[string]string{"/sites/shopfront copy": "/sites/shopfront copy"}}
	log, _ := logger()

	events := foreign.NewNotifier(checkouts.lookup).Notices(start, vmFree, resident(spa.ID, gibibyte*15/10), []foreign.Container{copied}, map[string]bool{}, log)

	line := "heavy: memory is low (" + platform.Docker + " 1.4 GiB free); shopfront-spa-watch-1 of this repository uses 1.5 GiB. If you do not need it now: docker compose --project-directory '/sites/shopfront copy' stop spa-watch"
	if want := []protocol.Event{{Checkout: "/sites/shopfront copy", Line: line}}; !reflect.DeepEqual(events, want) {
		t.Fatalf("Notices = %#v, want %#v", events, want)
	}
}

func TestNoticesComeInTheOrderOfTheContainerNames(t *testing.T) {
	database := foreign.Container{ID: identifier('d'), Name: "shopfront-db-1", Service: "db", WorkingDirectory: "/sites/shopfront-csv-export"}
	application := foreign.Container{ID: identifier('b'), Name: "shopfront-app-1", Service: "app", WorkingDirectory: "/sites/shopfront-csv-export"}
	usage := []helper.Container{{ID: database.ID, RSSBytes: gibibyte * 2}, {ID: application.ID, RSSBytes: gibibyte * 10}}
	log, _ := logger()

	events := foreign.NewNotifier(shopCheckouts().lookup).Notices(start, vmFree, usage, []foreign.Container{database, application}, map[string]bool{}, log)

	want := []protocol.Event{
		{Checkout: "/sites/shopfront", Line: "heavy: memory is low (" + platform.Docker + " 1.4 GiB free); shopfront-app-1 of this repository uses 10.0 GiB. If you do not need it now: docker compose --project-directory /sites/shopfront-csv-export stop app"},
		{Checkout: "/sites/shopfront", Line: "heavy: memory is low (" + platform.Docker + " 1.4 GiB free); shopfront-db-1 of this repository uses 2.0 GiB. If you do not need it now: docker compose --project-directory /sites/shopfront-csv-export stop db"},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("Notices = %#v, want %#v", events, want)
	}
}

func TestFailingCheckoutLogsAWarningAndGivesNoNotice(t *testing.T) {
	failure := errors.New(`heavy: git failed in /sites/shopfront-csv-export: exec: "git": executable file not found in $PATH`)
	checkouts := &fakeCheckouts{failures: map[string]error{"/sites/shopfront-csv-export": failure}}
	log, records := logger()

	events := foreign.NewNotifier(checkouts.lookup).Notices(start, vmFree, resident(spa.ID, gibibyte*15/10), []foreign.Container{spa}, map[string]bool{}, log)

	record := `{"level":"WARN","msg":"finding the checkout of a foreign container failed","container":"shopfront-spa-watch-1",` +
		`"working_directory":"/sites/shopfront-csv-export","error":"heavy: git failed in /sites/shopfront-csv-export: exec: \"git\": executable file not found in $PATH"}` + "\n"
	if events != nil || records.String() != record {
		t.Fatalf("Notices = %#v and logged %q, want no notice and %q", events, records.String(), record)
	}
}

func TestFailingCheckoutIsAskedAgainOnlyAfterFifteenMinutes(t *testing.T) {
	checkouts := &fakeCheckouts{failures: map[string]error{"/sites/shopfront-csv-export": errors.New("heavy: git failed")}}
	notifier := foreign.NewNotifier(checkouts.lookup)
	log, records := logger()
	notices := func(at time.Time) []protocol.Event {
		return notifier.Notices(at, vmFree, resident(spa.ID, gibibyte*15/10), []foreign.Container{spa}, map[string]bool{}, log)
	}

	got := [][]protocol.Event{notices(start), notices(start.Add(time.Minute)), notices(start.Add(15 * time.Minute))}

	directory := "/sites/shopfront-csv-export"
	if want := [][]protocol.Event{nil, nil, nil}; !reflect.DeepEqual(got, want) || !reflect.DeepEqual(checkouts.asked, []string{directory, directory}) {
		t.Fatalf("Notices at 0, 1 and 15 min = %#v, asked for %q; want no notices and two questions", got, checkouts.asked)
	}
	if lines := strings.Count(records.String(), "\n"); lines != 2 {
		t.Fatalf("logged %q, want two warnings", records.String())
	}
}

func TestContainerInNoCheckoutGetsNoNotice(t *testing.T) {
	checkouts := &fakeCheckouts{}
	log, records := logger()

	events := foreign.NewNotifier(checkouts.lookup).Notices(start, vmFree, resident(spa.ID, gibibyte*15/10), []foreign.Container{spa}, map[string]bool{}, log)

	record := `{"level":"INFO","msg":"a foreign container lies in no checkout, so no session hears of it","container":"shopfront-spa-watch-1",` +
		`"working_directory":"/sites/shopfront-csv-export"}` + "\n"
	if events != nil || records.String() != record {
		t.Fatalf("Notices = %#v and logged %q, want no notice and %q", events, records.String(), record)
	}
}
