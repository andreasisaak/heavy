package slots

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"
)

// Pool holds the slots one daemon has taken.
type Pool struct {
	held map[int]*os.File
}

// NewPool returns a pool that holds no slot.
func NewPool() *Pool {
	return &Pool{held: map[int]*os.File{}}
}

// Acquire takes the lowest free slot up to the limit of the machine; taken is false while the daemons of the
// machine hold them all. A slot file this user may not open counts as taken: the daemon of another user has just
// created it, locks it and opens it to every user a moment later.
func (p *Pool) Acquire(config Config) (slot int, taken bool, err error) {
	for number := 1; number <= config.MaxParallel; number++ {
		if p.held[number] != nil {
			continue
		}
		file, err := open(config, number, true)
		if errors.Is(err, fs.ErrPermission) {
			continue
		}
		if err != nil {
			return 0, false, err
		}
		locked, err := lock(file)
		if err != nil || !locked {
			file.Close()
			if err != nil {
				return 0, false, err
			}
			continue
		}
		p.held[number] = file
		return number, true, nil
	}
	return 0, false, nil
}

// Release gives a slot back. Releasing a slot the pool does not hold is a bug of the caller.
func (p *Pool) Release(slot int) {
	file := p.held[slot]
	if file == nil {
		panic(fmt.Sprintf("slots: releasing slot %d, which this daemon does not hold", slot))
	}
	delete(p.held, slot)
	file.Close()
}

// Others counts the slots up to the limit that other daemons hold: the runs of the other users of the machine.
// It locks each free slot for a moment to see that it is free; a daemon that asks for a slot in that moment finds
// it taken and asks again at its next sample.
func (p *Pool) Others(config Config) (int, error) {
	count := 0
	for number := 1; number <= config.MaxParallel; number++ {
		if p.held[number] != nil {
			continue
		}
		file, err := open(config, number, false)
		if errors.Is(err, fs.ErrPermission) {
			count++
			continue
		}
		if err != nil {
			return 0, err
		}
		if file == nil {
			continue
		}
		locked, err := lock(file)
		file.Close()
		if err != nil {
			return 0, err
		}
		if !locked {
			count++
		}
	}
	return count, nil
}

// open opens the file of a slot, and with create makes a missing one, open to every user whatever the umask. It
// opens first and creates only a missing file: Linux refuses O_CREAT on the file of another user in a sticky
// folder (fs.protected_regular). Without create a missing file gives nil.
func open(config Config, number int, create bool) (*os.File, error) {
	path := filepath.Join(config.Folder, "slot-"+strconv.Itoa(number))
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if errors.Is(err, fs.ErrNotExist) {
		if _, folderError := os.Stat(config.Folder); errors.Is(folderError, fs.ErrNotExist) {
			return nil, fmt.Errorf("heavy: the slot folder %s of %s is missing; create it with mode 1777", config.Folder, config.Path)
		}
		if !create {
			return nil, nil
		}
		file, err = os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666)
		if errors.Is(err, fs.ErrExist) {
			file, err = os.OpenFile(path, os.O_RDWR, 0)
		} else if err == nil {
			if err = file.Chmod(0o666); err != nil {
				file.Close()
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("heavy: opening the slot %s: %w", path, err)
	}
	return file, nil
}

// lock takes the lock of a slot file; locked is false while another holder has it.
func lock(file *os.File) (locked bool, err error) {
	err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("heavy: locking the slot %s: %w", file.Name(), err)
	}
	return true, nil
}
