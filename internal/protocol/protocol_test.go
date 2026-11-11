package protocol_test

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/andreasisaak/heavy/internal/protocol"
)

func TestHelloRoundTripsAsOneJSONLine(t *testing.T) {
	var buffer bytes.Buffer
	sent := protocol.Hello{Protocol: protocol.Version, Version: "1.0.0", Stamp: "abc"}

	if err := protocol.Write(&buffer, sent); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if want := `{"protocol":1,"version":"1.0.0","stamp":"abc"}` + "\n"; buffer.String() != want {
		t.Fatalf("wrote %q, want %q", buffer.String(), want)
	}
	got, err := protocol.ReadHello(bufio.NewReader(&buffer))
	if err != nil || got != sent {
		t.Fatalf("ReadHello = %#v, %v; want %#v", got, err, sent)
	}
}

func TestHelloOfAnotherProtocolStillNamesItsNumber(t *testing.T) {
	got, err := protocol.ReadHello(bufio.NewReader(strings.NewReader(`{"protocol":2,"version":"2.0.0","peer":"x"}` + "\n")))

	if err != nil || got != (protocol.Hello{Protocol: 2}) {
		t.Fatalf("ReadHello = %#v, %v; want protocol 2 alone", got, err)
	}
}

func TestHelloWithUnknownFieldOfThisProtocolIsAnError(t *testing.T) {
	_, err := protocol.ReadHello(bufio.NewReader(strings.NewReader(`{"protocol":1,"version":"1.0.0","peer":"x"}` + "\n")))

	want := `heavy: expected a hello of protocol 1, got {"protocol":1,"version":"1.0.0","peer":"x"}: json: unknown field "peer"`
	if err == nil || err.Error() != want {
		t.Fatalf("ReadHello error = %v, want %q", err, want)
	}
}

func TestMessageReadsTheTypeAndItsPayloadStrictly(t *testing.T) {
	var buffer bytes.Buffer
	sent := protocol.Message{Type: protocol.TypeForward, Forward: &protocol.Forward{Position: 3}}
	if err := protocol.Write(&buffer, sent); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	got, err := protocol.ReadMessage(bufio.NewReader(&buffer))

	if err != nil || !reflect.DeepEqual(got, sent) {
		t.Fatalf("ReadMessage = %#v, %v; want %#v", got, err, sent)
	}
}

func TestMessageWhosePayloadDoesNotFitItsTypeIsAnError(t *testing.T) {
	cases := map[string]string{
		`{"type":"forward"}`: `heavy: expected a message whose payload fits its type, got {"type":"forward"}`,
		`{"type":"forward","forward":{"position":2},"print":{"line":"x"}}`: `heavy: expected a message whose payload fits its type, got {"type":"forward","forward":{"position":2},"print":{"line":"x"}}`,
		`{"type":"jump"}`: `heavy: expected a message whose payload fits its type, got {"type":"jump"}`,
		`{"type":"forward","forward":{"position":2,"x":1}}`: `heavy: expected a message of protocol 1, got {"type":"forward","forward":{"position":2,"x":1}}: json: unknown field "x"`,
	}
	for line, want := range cases {
		_, err := protocol.ReadMessage(bufio.NewReader(strings.NewReader(line + "\n")))
		if err == nil || err.Error() != want {
			t.Errorf("ReadMessage(%s) error = %v, want %q", line, err, want)
		}
	}
}

// critic 5.1: the same version built from other sources takes over only when it is the build now on disk next to
// the daemon's binary; a monitor started before a rebuild keeps its old stamp and must not end the new daemon
func TestNewerComparesVersionsAsNumbers(t *testing.T) {
	cases := []struct {
		client, daemon protocol.Identity
		installed      string
		want           bool
	}{
		{protocol.Identity{Version: "1.0.1"}, protocol.Identity{Version: "1.0.0"}, "", true},
		{protocol.Identity{Version: "1.10.0"}, protocol.Identity{Version: "1.9.0"}, "", true},
		{protocol.Identity{Version: "1.0.0"}, protocol.Identity{Version: "1.0.1"}, "", false},
		{protocol.Identity{Version: "0.3.0"}, protocol.Identity{Version: "0.3"}, "", true},
		{protocol.Identity{Version: "1.0.0", Stamp: "b"}, protocol.Identity{Version: "1.0.0", Stamp: "a"}, "b", true},
		{protocol.Identity{Version: "1.0.0", Stamp: "b"}, protocol.Identity{Version: "1.0.0", Stamp: "a"}, "a", false},
		{protocol.Identity{Version: "1.0.0", Stamp: "b"}, protocol.Identity{Version: "1.0.0", Stamp: "a"}, "c", false},
		{protocol.Identity{Version: "1.0.0", Stamp: "a"}, protocol.Identity{Version: "1.0.0", Stamp: "a"}, "a", false},
		{protocol.Identity{Version: "1.0.0"}, protocol.Identity{}, "", true},
		{protocol.Identity{}, protocol.Identity{Version: "1.0.0"}, "", false},
		{protocol.Identity{Version: "1.x"}, protocol.Identity{Version: "1.0.0"}, "", false},
	}
	for _, current := range cases {
		if got := current.client.Newer(current.daemon, current.installed); got != current.want {
			t.Errorf("%#v.Newer(%#v, %q) = %v, want %v", current.client, current.daemon, current.installed, got, current.want)
		}
	}
}

func TestLocalIdentityReadsThePluginVersionAndTheStamp(t *testing.T) {
	root := t.TempDir()
	for _, folder := range []string{"bin", ".claude-plugin"} {
		if err := os.Mkdir(filepath.Join(root, folder), 0o755); err != nil {
			t.Fatalf("creating %s: %v", folder, err)
		}
	}
	files := map[string]string{
		"bin/heavy":                  "binary",
		"bin/.stamp":                 "f00d\n",
		".claude-plugin/plugin.json": `{"name": "heavy", "version": "1.0.0", "description": "x"}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	got, err := protocol.LocalIdentity(filepath.Join(root, "bin", "heavy"))

	if want := (protocol.Identity{Version: "1.0.0", Stamp: "f00d"}); err != nil || got != want {
		t.Fatalf("LocalIdentity = %#v, %v; want %#v", got, err, want)
	}
}

func TestLocalIdentityOutsideAPluginIsEmpty(t *testing.T) {
	got, err := protocol.LocalIdentity(filepath.Join(t.TempDir(), "heavy"))

	if err != nil || got != (protocol.Identity{}) {
		t.Fatalf("LocalIdentity = %#v, %v; want an empty identity", got, err)
	}
}

func TestLocalIdentityWithABrokenManifestIsAnError(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".claude-plugin"), 0o755); err != nil {
		t.Fatalf("creating the manifest folder: %v", err)
	}
	manifest := filepath.Join(root, ".claude-plugin", "plugin.json")
	if err := os.WriteFile(manifest, []byte(`{"version": 1}`), 0o644); err != nil {
		t.Fatalf("writing the manifest: %v", err)
	}

	_, err := protocol.LocalIdentity(filepath.Join(root, "bin", "heavy"))

	want := "heavy: " + manifest + ": expected a version string: json: cannot unmarshal number into Go struct field manifest.version of type string"
	if err == nil || err.Error() != want {
		t.Fatalf("LocalIdentity error = %v, want %q", err, want)
	}
}

func TestLineOverTheLimitIsAnError(t *testing.T) {
	line := `{"type":"print","print":{"line":"` + strings.Repeat("x", protocol.MaxLine) + `"}}` + "\n"

	_, err := protocol.ReadMessage(bufio.NewReader(strings.NewReader(line)))

	want := "heavy: a line from the other side is longer than 4194304 bytes"
	if err == nil || err.Error() != want {
		t.Fatalf("ReadMessage error = %v, want %q", err, want)
	}
}
