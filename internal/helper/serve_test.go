package helper_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/andreasisaak/heavy/internal/helper"
)

// askRaw serves one request line and returns the one answer line without its newline.
func askRaw(t *testing.T, options helper.Options, request string) string {
	t.Helper()
	var out bytes.Buffer
	if err := helper.Serve(options, strings.NewReader(request+"\n"), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("Serve answered %q, want one line", out.String())
	}
	return lines[0]
}

// ask serves one request line and decodes the answer strictly.
func ask(t *testing.T, options helper.Options, request string) helper.Response {
	t.Helper()
	line := askRaw(t, options, request)
	decoder := json.NewDecoder(strings.NewReader(line))
	decoder.DisallowUnknownFields()
	var response helper.Response
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("decoding %s: %v", line, err)
	}
	return response
}

// plan 3.1: a request of an unknown type gets {"error": "unknown request <type>"}
func TestUnknownRequestIsAnsweredWithAnError(t *testing.T) {
	line := askRaw(t, helper.Options{Root: t.TempDir(), PageSize: 4096}, `{"type":"reboot"}`)

	if want := `{"error":"unknown request reboot"}`; line != want {
		t.Fatalf("answer = %s, want %s", line, want)
	}
}

func TestRequestThatIsNoJSONObjectIsAnsweredWithAnError(t *testing.T) {
	response := ask(t, helper.Options{Root: t.TempDir(), PageSize: 4096}, `sample please`)

	want := helper.Response{Error: `heavy: expected a JSON request, got "sample please": invalid character 's' looking for beginning of value`}
	if !reflect.DeepEqual(response, want) {
		t.Fatalf("answer = %#v, want %#v", response, want)
	}
}

func TestRequestWithAnUnknownFieldIsAnsweredWithAnError(t *testing.T) {
	response := ask(t, helper.Options{Root: t.TempDir(), PageSize: 4096}, `{"type":"sample","verbose":true}`)

	want := helper.Response{Error: `heavy: expected a JSON request, got "{\"type\":\"sample\",\"verbose\":true}": json: unknown field "verbose"`}
	if !reflect.DeepEqual(response, want) {
		t.Fatalf("answer = %#v, want %#v", response, want)
	}
}

// every request gets one answer line in order; the end of the input ends Serve without an error
func TestServeAnswersEachRequestUntilTheInputEnds(t *testing.T) {
	root := findFixture(t)
	var out bytes.Buffer

	err := helper.Serve(helper.Options{Root: root, PageSize: 4096}, strings.NewReader("{\"type\":\"find\",\"tag\":\"r2\"}\n{\"type\":\"nap\"}\n{\"type\":\"containers\"}\n"), &out)

	want := `{"found":{"processes":[]}}` + "\n" + `{"error":"unknown request nap"}` + "\n" +
		`{"usage":{"containers":[{"id":"` + containerA + `","rss_bytes":6291456},{"id":"` + containerB + `","rss_bytes":524288}]}}` + "\n"
	if err != nil || out.String() != want {
		t.Fatalf("Serve = %v, answered %q, want %q", err, out.String(), want)
	}
}

func TestRequestLongerThanFourMebibytesEndsServeWithAnError(t *testing.T) {
	var out bytes.Buffer
	request := `{"type":"find","tag":"` + strings.Repeat("x", 4<<20) + `"}` + "\n"

	err := helper.Serve(helper.Options{Root: t.TempDir(), PageSize: 4096}, strings.NewReader(request), &out)

	if err == nil || err.Error() != "heavy: a request is longer than 4194304 bytes" || out.Len() != 0 {
		t.Fatalf("Serve = %v, answered %q", err, out.String())
	}
}

// a failed write of an answer ends Serve with the error
func TestAnswerThatCannotBeWrittenEndsServe(t *testing.T) {
	err := helper.Serve(helper.Options{Root: t.TempDir(), PageSize: 4096}, strings.NewReader(`{"type":"nap"}`+"\n"), failingWriter{})

	if err == nil || err.Error() != "heavy: writing an answer: the pipe is gone" {
		t.Fatalf("Serve = %v", err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("the pipe is gone") }

// critic 3.1: a request line holds one JSON object and nothing after it
func TestRequestWithDataAfterTheObjectIsAnsweredWithAnError(t *testing.T) {
	response := ask(t, helper.Options{Root: t.TempDir(), PageSize: 4096}, `{"type":"sample"} {"type":"find"}`)

	want := helper.Response{Error: `heavy: expected one JSON request per line, got "{\"type\":\"sample\"} {\"type\":\"find\"}"`}
	if !reflect.DeepEqual(response, want) {
		t.Fatalf("answer = %#v, want %#v", response, want)
	}
}
