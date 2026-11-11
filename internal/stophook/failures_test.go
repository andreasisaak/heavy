package stophook_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/stophook"
)

// design G15: a queue the hook cannot read denies the stop with the error, and only the queue call fails
func TestStopWhileTheQueueCannotBeReadIsDeniedWithTheError(t *testing.T) {
	f := newFixture(t)
	failing := func() ([]protocol.RunStatus, error) {
		return nil, errors.New("heavy: reading the queue from the daemon: EOF")
	}

	got := f.stopWith(t, f.input(t, "task_id", queuedTask), failing, now)

	expect(t, "stop", got, denial(t, "heavy: reading the queue from the daemon: EOF"))
	if f.queueCalls != 1 {
		t.Fatalf("the hook asked for the queue %d times, want 1", f.queueCalls)
	}
}

// design G15: a transcript the hook cannot read or parse denies the stop with the place and the problem
func TestStopWithAnUnreadableTranscriptIsDeniedWithTheProblem(t *testing.T) {
	queuedLine := `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_ci","content":"with ID: ` + queuedTask + `."}]}}`
	cases := []struct {
		name    string
		prepare func(t *testing.T, transcript string)
		reason  func(transcript string) string
	}{
		{"directory", func(t *testing.T, transcript string) {
			if err := os.Mkdir(transcript, 0o755); err != nil {
				t.Fatal(err)
			}
		}, func(transcript string) string {
			return "heavy: reading the transcript " + transcript + ": read " + transcript + ": is a directory"
		}},
		{"line that is no JSON", func(t *testing.T, transcript string) {
			writeFile(t, transcript, `{"type":"user"}`+"\n"+`Queued behind heavy`+"\n")
		}, func(transcript string) string {
			return "heavy: " + transcript + ":2: expected the entry as a JSON object, got invalid character 'Q' looking for beginning of value"
		}},
		{"attachment without toolUseID", func(t *testing.T, transcript string) {
			writeFile(t, transcript, `{"type":"attachment","attachment":{"type":"hook_success","content":"Queued behind heavy"}}`+"\n")
		}, func(transcript string) string {
			return "heavy: " + transcript + ":1: the hook_success attachment has no toolUseID"
		}},
		{"tool_result without tool_use_id", func(t *testing.T, transcript string) {
			writeFile(t, transcript, strings.Replace(queuedLine, `"tool_use_id":"toolu_ci",`, "", 1)+"\n")
		}, func(transcript string) string {
			return "heavy: " + transcript + ":1: the tool_result has no tool_use_id"
		}},
		{"message that is no object", func(t *testing.T, transcript string) {
			writeFile(t, transcript, `{"type":"user","message":"with ID: `+queuedTask+`."}`+"\n")
		}, func(transcript string) string {
			return "heavy: " + transcript + ":1: expected the message as a JSON object, got string"
		}},
	}
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			f := newFixture(t)
			f.transcript = filepath.Join(f.directory, "broken.jsonl")
			current.prepare(t, f.transcript)

			got := f.stop(t, f.input(t, "task_id", queuedTask), []protocol.RunStatus{own("waiting")}, now)

			expect(t, "stop", got, denial(t, current.reason(f.transcript)))
		})
	}
}

// design G15: a stop marker the hook cannot create, read, parse, remove or write denies the stop with the problem
func TestStopWithABrokenStopMarkerIsDeniedWithTheProblem(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, f *fixture, stops string)
		at      time.Time
		reason  func(f *fixture, stops string) string
	}{
		{"state directory that is a file", func(t *testing.T, f *fixture, stops string) {
			writeFile(t, filepath.Join(f.state, "file"), "")
			f.state = filepath.Join(f.state, "file")
		}, now, func(f *fixture, stops string) string {
			return "heavy: creating the stop markers directory: mkdir " + f.state + ": not a directory"
		}},
		{"marker that is a directory", func(t *testing.T, f *fixture, stops string) {
			mkdir(t, filepath.Join(stops, queuedTask), 0o700)
		}, now, func(f *fixture, stops string) string {
			return "heavy: reading the stop marker: read " + filepath.Join(stops, queuedTask) + ": is a directory"
		}},
		{"marker without a time", func(t *testing.T, f *fixture, stops string) {
			mkdir(t, stops, 0o700)
			writeFile(t, filepath.Join(stops, queuedTask), "soon")
		}, now, func(f *fixture, stops string) string {
			return "heavy: " + filepath.Join(stops, queuedTask) + `: expected the Unix time of the denied stop, got "soon"; remove the file to reset it`
		}},
		{"marker that cannot be removed", func(t *testing.T, f *fixture, stops string) {
			mkdir(t, stops, 0o700)
			writeFile(t, filepath.Join(stops, queuedTask), "1800000000")
			readOnly(t, stops)
		}, now.Add(120 * time.Second), func(f *fixture, stops string) string {
			return "heavy: removing the stop marker: remove " + filepath.Join(stops, queuedTask) + ": permission denied"
		}},
		{"marker that cannot be written", func(t *testing.T, f *fixture, stops string) {
			mkdir(t, stops, 0o700)
			readOnly(t, stops)
		}, now, func(f *fixture, stops string) string {
			return "heavy: writing the stop marker: open " + filepath.Join(stops, queuedTask) + ": permission denied"
		}},
	}
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			f := newFixture(t)
			stops := filepath.Join(f.state, "stops")
			current.prepare(t, f, stops)

			got := f.stop(t, f.input(t, "task_id", queuedTask), []protocol.RunStatus{own("waiting")}, current.at)

			expect(t, "stop", got, denial(t, current.reason(f, stops)))
		})
	}
}

// design G15: hook input the hook cannot use denies the stop with what is wrong
func TestStopWithBrokenHookInputIsDenied(t *testing.T) {
	cases := []struct{ name, input, reason string }{
		{"no JSON", "{", "heavy: reading the hook input: unexpected end of JSON input"},
		{"task id that is no string", `{"tool_name":"KillShell","tool_input":{"shell_id":7},"session_id":"s","transcript_path":"/t"}`,
			`heavy: expected task_id or shell_id as a string in the input of KillShell, got {"shell_id":7}`},
		{"no tool input", `{"tool_name":"TaskStop","session_id":"s","transcript_path":"/t"}`, "heavy: the hook input of TaskStop has no tool_input"},
		{"no transcript path", `{"tool_name":"TaskStop","tool_input":{"task_id":"b1"},"session_id":"s"}`, "heavy: the hook input of TaskStop has no transcript_path"},
		{"no session id", `{"tool_name":"TaskStop","tool_input":{"task_id":"b1"},"transcript_path":"/t"}`, "heavy: the hook input of TaskStop has no session_id"},
	}
	for _, current := range cases {
		t.Run(current.name, func(t *testing.T) {
			f := newFixture(t)

			got := f.stop(t, current.input, nil, now)

			expect(t, "stop", got, denial(t, current.reason))
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("example write failure") }

// an answer the hook cannot write is its error
func TestDenialThatCannotBeWrittenIsTheError(t *testing.T) {
	f := newFixture(t)
	deps := stophook.Dependencies{
		StateDirectory: f.state,
		Queue:          func() ([]protocol.RunStatus, error) { return []protocol.RunStatus{own("waiting")}, nil },
		Now:            func() time.Time { return now },
	}

	err := stophook.Run(strings.NewReader(f.input(t, "task_id", queuedTask)), failingWriter{}, deps)

	if err == nil || err.Error() != "heavy: writing the hook answer: example write failure" {
		t.Fatalf("Run = %v, want heavy: writing the hook answer: example write failure", err)
	}
}

func mkdir(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(path, mode); err != nil {
		t.Fatal(err)
	}
}

// readOnly takes the write permission of directory for the test and gives it back afterwards, so the state
// directory can be removed.
func readOnly(t *testing.T, directory string) {
	t.Helper()
	if err := os.Chmod(directory, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(directory, 0o700); err != nil {
			t.Errorf("giving back the write permission of %s: %v", directory, err)
		}
	})
}
