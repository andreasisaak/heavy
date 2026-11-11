package sessions_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/andreasisaak/heavy/internal/sessions"
	"github.com/andreasisaak/heavy/internal/testkit"
)

func TestWrittenRegistrationReadsBack(t *testing.T) {
	state := testkit.StateDir(t)
	want := sessions.Registration{SessionID: "s-1", Cwd: "/w/app", Checkout: "/w", Time: 1794139101}

	if err := sessions.Write(state, 4242, want); err != nil {
		t.Fatal(err)
	}
	got, err := sessions.Read(state, 4242)

	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Read = %+v, %v; want %+v", got, err, want)
	}
	if left, _ := filepath.Glob(filepath.Join(state, "sessions", "*.new")); len(left) != 0 {
		t.Fatalf("left %v", left)
	}
}

func TestMissingRegistrationIsAnErrorWithItsPath(t *testing.T) {
	state := testkit.StateDir(t)
	path := filepath.Join(state, "sessions", "7.json")

	_, err := sessions.Read(state, 7)

	want := "heavy: reading the session registration " + path + ": open " + path + ": no such file or directory"
	if err == nil || err.Error() != want {
		t.Fatalf("Read = %v, want %q", err, want)
	}
}

func TestRegistrationWithAnUnknownFieldIsAnError(t *testing.T) {
	state := testkit.StateDir(t)
	path := filepath.Join(state, "sessions", "8.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"session_id":"s","cwd":"/","checkout":"/","time":1,"extra":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := sessions.Read(state, 8)

	want := "heavy: reading the session registration " + path + `: json: unknown field "extra"`
	if err == nil || err.Error() != want {
		t.Fatalf("Read = %v, want %q", err, want)
	}
}

func TestRegistrationThatCannotBeWrittenNamesTheFolder(t *testing.T) {
	err := sessions.Write("/dev/null/state", 9, sessions.Registration{SessionID: "s"})

	want := "heavy: registering the session in /dev/null/state/sessions: mkdir /dev/null: not a directory"
	if err == nil || err.Error() != want {
		t.Fatalf("Write = %v, want %q", err, want)
	}
}

// heavy-37 2026-11-08: registrations are never removed otherwise, so a session start drops those of Claude Code
// processes that ended; a registration of a living process and files that are no registration stay
func TestPruneRemovesTheRegistrationsOfEndedProcesses(t *testing.T) {
	state := testkit.StateDir(t)
	for _, pid := range []int{4242, 4343} {
		if err := sessions.Write(state, pid, sessions.Registration{SessionID: "s"}); err != nil {
			t.Fatal(err)
		}
	}
	other := filepath.Join(state, "sessions", "notes.txt")
	if err := os.WriteFile(other, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	removed, err := sessions.Prune(state, func(pid int) bool { return pid == 4242 })

	if err != nil || !reflect.DeepEqual(removed, []int{4343}) {
		t.Fatalf("Prune = %v, %v; want 4343 removed", removed, err)
	}
	for path, want := range map[string]bool{sessions.Path(state, 4242): true, sessions.Path(state, 4343): false, other: true} {
		if _, err := os.Stat(path); (err == nil) != want {
			t.Fatalf("%s exists: %v, want %v", path, err == nil, want)
		}
	}
}
