package main

import (
	"reflect"
	"testing"

	"github.com/andreasisaak/heavy/internal/sessions"
	"github.com/andreasisaak/heavy/internal/testkit"
)

// heavy-37 2026-11-08: build.sh runs the hook of an existing binary as its child, so the hook takes the PID of
// Claude Code from HEAVY_CLAUDE_PID, which build.sh sets to its own parent, and not from its parent
func TestSessionStartHookRegistersUnderTheClaudePIDThatBuildShNames(t *testing.T) {
	world := newHerdrWorld(t, "{}", "HEAVY_CLAUDE_PID=4242")
	cwd := t.TempDir()

	got := world.run(t, testkit.BuildBinary(t), `{"session_id": "s-pid", "cwd": "`+cwd+`"}`, "--hook", "session-start")

	registration, err := sessions.Read(world.state, 4242)
	want := sessions.Registration{SessionID: "s-pid", Cwd: cwd, Checkout: cwd}
	registration.Time = 0
	if got.exitStatus != 0 || err != nil || !reflect.DeepEqual(registration, want) {
		t.Fatalf("hook = %+v; registration %+v, %v; want exit 0 and %+v", got, registration, err, want)
	}
}

func TestSessionStartHookWithoutTheClaudePIDFails(t *testing.T) {
	world := newHerdrWorld(t, "{}")

	got := world.run(t, testkit.BuildBinary(t), `{"session_id": "s-pid", "cwd": "/tmp"}`, "--hook", "session-start")

	want := outcome{exitStatus: 1, stderr: "heavy: HEAVY_CLAUDE_PID is not set; the SessionStart hook runs through hooks/build.sh, which names the PID of Claude Code\n"}
	if got != want {
		t.Fatalf("hook = %+v, want %+v", got, want)
	}
}
