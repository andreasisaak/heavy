package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andreasisaak/heavy/internal/testkit"
)

// helperOutcome runs heavy --helper with the given arguments and input.
func helperOutcome(t *testing.T, input string, arguments ...string) outcome {
	t.Helper()
	command := exec.Command(testkit.BuildBinary(t), append([]string{"--helper"}, arguments...)...)
	command.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	exitStatus := 0
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		exitStatus = exitError.ExitCode()
	} else if err != nil {
		t.Fatalf("running heavy --helper: %v", err)
	}
	return outcome{exitStatus: exitStatus, stdout: stdout.String(), stderr: stderr.String()}
}

// plan 3.1: heavy --helper answers JSON lines about the /proc below --root and ends with its input
func TestHelperAnswersAboutTheProcRootUntilItsInputEnds(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "7"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"7/environ": "HEAVY_RUN=r1\x00", "7/status": "VmRSS:\t 64 kB\n", "7/cgroup": "0::/\n"}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := helperOutcome(t, `{"type":"find","tag":"r1"}`+"\n"+`{"type":"nap"}`+"\n", "--root", root)

	want := outcome{stdout: `{"found":{"processes":[{"pid":7,"rss_bytes":65536,"container":""}]}}` + "\n" + `{"error":"unknown request nap"}` + "\n"}
	if got != want {
		t.Fatalf("heavy --helper = %+v, want %+v", got, want)
	}
}

func TestHelperWithAnUnknownOptionIsAUsageError(t *testing.T) {
	got := helperOutcome(t, "", "--proc", "/tmp")

	want := outcome{exitStatus: 64, stderr: "heavy: unknown option --proc. " + usage + "\n"}
	if got != want {
		t.Fatalf("heavy --helper --proc = %+v, want %+v", got, want)
	}
}

func TestHelperWithARootWithoutAFolderIsAUsageError(t *testing.T) {
	got := helperOutcome(t, "", "--root")

	want := outcome{exitStatus: 64, stderr: "heavy: --root needs a directory. " + usage + "\n"}
	if got != want {
		t.Fatalf("heavy --helper --root = %+v, want %+v", got, want)
	}
}

// a request longer than the limit ends the helper with exit 1 and the reason
func TestHelperEndsWithTheErrorOfItsInput(t *testing.T) {
	got := helperOutcome(t, strings.Repeat("x", 4<<20+1)+"\n", "--root", t.TempDir())

	want := outcome{exitStatus: 1, stderr: "heavy: a request is longer than 4194304 bytes\n"}
	if got != want {
		t.Fatalf("heavy --helper = %+v, want %+v", got, want)
	}
}
