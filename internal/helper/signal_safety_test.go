package helper_test

import "testing"

// critic 3.2: PIDs 1 and below never get a signal, even when a /proc entry for them carries the tag
func TestSignalSkipsInitAndTheSpecialPIDs(t *testing.T) {
	root := t.TempDir()
	writeEnvironment(t, root, 1, "HEAVY_RUN=run-1")
	writeEnvironment(t, root, 0, "HEAVY_RUN=run-1")
	kill := &recordingKill{}

	response := signalRequest(t, root, kill.Kill, "run-1", "KILL", []int{1, 0, -1})

	assertSignaled(t, response, nil, []int{1, 0, -1})
	assertKills(t, kill, nil)
}
