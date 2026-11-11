package herdr_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/andreasisaak/heavy/internal/herdr"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// refreshError runs a refresh that has to fail and returns its error text.
func refreshError(t *testing.T, deps herdr.RefreshDependencies) string {
	t.Helper()
	var stdout bytes.Buffer
	err := herdr.Refresh(deps, &stdout)
	if err == nil {
		t.Fatalf("refresh succeeded with stdout %q, want an error", stdout.String())
	}
	check(t, "stdout", stdout.String(), "")
	return err.Error()
}

func TestRefreshFailsWithTheQueueErrorBeforeAskingHerdr(t *testing.T) {
	fake := newHerdr(t, nil, "")
	deps := dependencies(t, fake, nil)
	deps.Runs = func() ([]protocol.RunStatus, error) {
		return nil, errors.New("heavy: reading the queue from the daemon: EOF")
	}

	got := refreshError(t, deps)

	check(t, "error", got, "heavy: reading the queue from the daemon: EOF")
	check(t, "herdr calls", fake.recorded(t), [][]string(nil))
}

func TestRefreshFailsWithHerdrsAnswerWhenTheAgentListFails(t *testing.T) {
	fake := newHerdr(t, nil, "agent list")

	got := refreshError(t, dependencies(t, fake, nil))

	check(t, "error", got, "heavy: "+fake.path+" agent list failed: exit status 3: herdr: example failure of agent list")
}

func TestRefreshFailsWithTheListingWhenTheAgentListIsNoListing(t *testing.T) {
	fake := newHerdrPrinting(t, t.TempDir(), `{"error": {"code": "server_unreachable"}}`, "")

	got := refreshError(t, dependencies(t, fake, nil))

	check(t, "error", got, `heavy: herdr agent list: expected {"result": {"agents": [...]}}, got {"error": {"code": "server_unreachable"}}`)
}

func TestRefreshFailsWithThePaneWhenAReportFails(t *testing.T) {
	main := filepath.Join(checkouts(t, "main"), "main")
	fake := newHerdr(t, []agent{agentOf("w1:p1", main, nil)}, "pane report-metadata")

	got := refreshError(t, dependencies(t, fake, []protocol.RunStatus{running(365, main, "ci")}))

	check(t, "error", got, "heavy: reporting the heavy rows of pane w1:p1: "+fake.path+
		" pane report-metadata w1:p1 --source heavy-queue --token heavy=▶ 6m · ci failed: exit status 3: herdr: example failure of pane report-metadata")
}

func TestRefreshFailsOnARunOfAnUnknownState(t *testing.T) {
	main := filepath.Join(checkouts(t, "main"), "main")
	fake := newHerdr(t, []agent{agentOf("w1:p1", main, nil)}, "")
	run := record("sleeping", 30, main, "ci")
	run.ID = "r7"

	got := refreshError(t, dependencies(t, fake, []protocol.RunStatus{run}))

	check(t, "error", got, "heavy: run r7 has the state sleeping, expected waiting, running, paused or finished")
}

func TestRefreshFailsWithThePathWhenTheLockCannotBeOpened(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	fake := newHerdr(t, nil, "")
	deps := dependencies(t, fake, nil)
	deps.Lock = filepath.Join(blocker, "herdr-refresh.lock")

	got := refreshError(t, deps)

	check(t, "error", got, "heavy: creating the folder of the lock "+deps.Lock+": mkdir "+blocker+": not a directory")
	check(t, "herdr calls", fake.recorded(t), [][]string(nil))
}

// queue.sh ran its passes one after another under a lock, so a slow earlier pass cannot report an older state
// after a later one
func TestRefreshWaitsWhileAnotherHoldsTheLock(t *testing.T) {
	main := filepath.Join(checkouts(t, "main"), "main")
	fake := newHerdr(t, []agent{agentOf("w1:p1", main, nil)}, "")
	deps := dependencies(t, fake, []protocol.RunStatus{running(365, main, "ci")})
	holder, err := os.OpenFile(deps.Lock, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if err := unix.Flock(int(holder.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	done := make(chan error)
	go func() { done <- herdr.Refresh(deps, &bytes.Buffer{}) }()

	time.Sleep(300 * time.Millisecond)
	whileHeld := fake.recorded(t)
	if err := unix.Flock(int(holder.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("refresh after the lock was free: %v", err)
	}

	check(t, "herdr calls while the lock was held", whileHeld, [][]string(nil))
	check(t, "herdr calls after", fake.recorded(t), [][]string{agentList, report("w1:p1", "--token", "heavy=▶ 6m · ci")})
}

func TestRefreshFailsWithTheFolderWhenItsCheckoutCannotBeChecked(t *testing.T) {
	locked := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	fake := newHerdr(t, []agent{agentOf("w1:p1", locked, nil)}, "")

	got := refreshError(t, dependencies(t, fake, nil))

	check(t, "error", got, "heavy: checking for a git checkout in "+locked+": stat "+filepath.Join(locked, ".git")+": permission denied")
}
