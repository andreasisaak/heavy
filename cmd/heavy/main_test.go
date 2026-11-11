package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/andreasisaak/heavy/internal/testkit"
)

type outcome struct {
	exitStatus int
	stdout     string
	stderr     string
}

func runHeavy(t *testing.T, arguments ...string) outcome {
	t.Helper()
	command := exec.Command(testkit.BuildBinary(t), arguments...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	exitStatus := 0
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		exitStatus = exitError.ExitCode()
	} else if err != nil {
		t.Fatalf("running heavy %q: %v", arguments, err)
	}
	return outcome{exitStatus: exitStatus, stdout: stdout.String(), stderr: stderr.String()}
}

func TestHelpPrintsTheUsage(t *testing.T) {
	got := runHeavy(t, "--help")
	want := outcome{exitStatus: 0, stdout: usage + "\n"}
	if got != want {
		t.Fatalf("heavy --help = %+v, want %+v", got, want)
	}
}

func TestUnknownOptionExitsWithUsageError(t *testing.T) {
	got := runHeavy(t, "--nope")
	want := outcome{exitStatus: 64, stderr: "heavy: unknown option --nope. " + usage + "\n"}
	if got != want {
		t.Fatalf("heavy --nope = %+v, want %+v", got, want)
	}
}

func TestOtherProgramNameExitsWithUsageError(t *testing.T) {
	link := filepath.Join(t.TempDir(), "nope")
	if err := os.Symlink(testkit.BuildBinary(t), link); err != nil {
		t.Fatalf("linking %s: %v", link, err)
	}
	var stderr bytes.Buffer
	command := exec.Command(link)
	command.Stderr = &stderr
	err := command.Run()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 64 {
		t.Fatalf("nope ended with %v, want exit status 64", err)
	}
	want := "heavy: started as nope, which is no program heavy shims. " + usage + "\n"
	if stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestNoArgumentsExitWithUsageError(t *testing.T) {
	got := runHeavy(t)
	want := outcome{exitStatus: 64, stderr: "heavy: no command given. " + usage + "\n"}
	if got != want {
		t.Fatalf("heavy = %+v, want %+v", got, want)
	}
}
