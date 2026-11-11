package slots_test

import (
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"

	"github.com/andreasisaak/heavy/internal/slots"
)

func machine(t *testing.T, limit int) slots.Config {
	t.Helper()
	return slots.Config{Path: "/etc/heavy/config.json", MaxParallel: limit, Folder: t.TempDir()}
}

func take(t *testing.T, pool *slots.Pool, config slots.Config) (int, bool) {
	t.Helper()
	slot, taken, err := pool.Acquire(config)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	return slot, taken
}

func others(t *testing.T, pool *slots.Pool, config slots.Config) int {
	t.Helper()
	count, err := pool.Others(config)
	if err != nil {
		t.Fatalf("Others failed: %v", err)
	}
	return count
}

func TestTwoDaemonsShareTheSlotsOfTheMachine(t *testing.T) {
	config := machine(t, 2)
	first, second := slots.NewPool(), slots.NewPool()

	one, tookOne := take(t, first, config)
	two, tookTwo := take(t, second, config)
	three, tookThree := take(t, first, config)

	got := []any{one, tookOne, two, tookTwo, three, tookThree}
	if want := []any{1, true, 2, true, 0, false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("acquired %v, want %v: the third run finds no slot", got, want)
	}
	if mine, theirs := others(t, first, config), others(t, second, config); mine != 1 || theirs != 1 {
		t.Fatalf("others = %d and %d, want each daemon to count the one slot of the other", mine, theirs)
	}
}

func TestAReleasedSlotGoesToTheNextDaemon(t *testing.T) {
	config := machine(t, 1)
	first, second := slots.NewPool(), slots.NewPool()
	slot, _ := take(t, first, config)
	if _, taken := take(t, second, config); taken {
		t.Fatal("the second daemon took the only slot while the first held it")
	}

	first.Release(slot)
	again, taken := take(t, second, config)

	if !taken || again != 1 || others(t, first, config) != 1 || others(t, second, config) != 0 {
		t.Fatalf("after the release: slot %d, taken %v, want slot 1 with the second daemon alone", again, taken)
	}
}

func TestReleasingASlotTwiceOrAnUnknownOnePanics(t *testing.T) {
	pool := slots.NewPool()
	defer func() {
		if recovered := recover(); recovered != "slots: releasing slot 3, which this daemon does not hold" {
			t.Fatalf("Release panicked with %v, want the message that names slot 3", recovered)
		}
	}()

	pool.Release(3)
}

func TestSlotFilesAreOpenToEveryUserWhateverTheUmask(t *testing.T) {
	config := machine(t, 1)
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)

	take(t, slots.NewPool(), config)

	info, err := os.Stat(filepath.Join(config.Folder, "slot-1"))
	if err != nil || info.Mode().Perm() != 0o666 {
		t.Fatalf("slot-1 has mode %v, %v, want 0666 so the daemon of every user can lock it", info.Mode().Perm(), err)
	}
}

func TestALowerLimitLeavesTheSlotsAboveItAlone(t *testing.T) {
	config := machine(t, 2)
	pool, other := slots.NewPool(), slots.NewPool()
	take(t, pool, config)
	take(t, other, config)
	config.MaxParallel = 1

	_, taken := take(t, other, config)

	if taken || others(t, pool, config) != 0 || others(t, other, config) != 1 {
		t.Fatal("with the limit lowered to 1, a daemon took a slot or counted the slot above the limit")
	}
}

func TestWithoutTheSlotFolderAcquireSaysHowToCreateIt(t *testing.T) {
	config := slots.Config{Path: "/etc/heavy/config.json", MaxParallel: 2, Folder: filepath.Join(t.TempDir(), "slots")}

	_, taken, err := slots.NewPool().Acquire(config)

	want := "heavy: the slot folder " + config.Folder + " of /etc/heavy/config.json is missing; create it with mode 1777"
	if err == nil || taken || err.Error() != want {
		t.Fatalf("Acquire error = %v, taken %v, want %q", err, taken, want)
	}
}

func TestWithoutTheSlotFolderOthersSaysHowToCreateIt(t *testing.T) {
	config := slots.Config{Path: "/etc/heavy/config.json", MaxParallel: 2, Folder: filepath.Join(t.TempDir(), "slots")}

	_, err := slots.NewPool().Others(config)

	want := "heavy: the slot folder " + config.Folder + " of /etc/heavy/config.json is missing; create it with mode 1777"
	if err == nil || err.Error() != want {
		t.Fatalf("Others error = %v, want %q", err, want)
	}
}

func TestASlotFileThatCannotBeOpenedIsAnError(t *testing.T) {
	config := machine(t, 1)
	if err := os.Mkdir(filepath.Join(config.Folder, "slot-1"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, taken, err := slots.NewPool().Acquire(config)

	want := "heavy: opening the slot " + config.Folder + "/slot-1: open " + config.Folder + "/slot-1: is a directory"
	if err == nil || taken || err.Error() != want {
		t.Fatalf("Acquire error = %v, taken %v, want %q", err, taken, want)
	}
}

// A slot file another daemon has just created, not yet opened to every user, counts as taken: that daemon locks it.
func TestASlotFileNotYetOpenToThisUserCountsAsTaken(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root opens every file")
	}
	config := machine(t, 2)
	if err := os.WriteFile(filepath.Join(config.Folder, "slot-1"), nil, 0); err != nil {
		t.Fatal(err)
	}
	pool := slots.NewPool()

	slot, taken := take(t, pool, config)

	if slot != 2 || !taken || others(t, pool, config) != 1 {
		t.Fatalf("Acquire = slot %d, %v, others %d; want slot 2 and slot 1 counted as held by another", slot, taken, others(t, pool, config))
	}
}
