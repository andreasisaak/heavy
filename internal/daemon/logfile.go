package daemon

import (
	"fmt"
	"os"
	"sync"
)

// logLimit is the size at which daemon.log moves to daemon.log.1.
const logLimit = 10 << 20

// rotatingLog appends to daemon.log and moves it to daemon.log.1 before a write would take it over the limit.
type rotatingLog struct {
	mutex sync.Mutex
	path  string
	file  *os.File
	size  int64
}

func openRotatingLog(path string) (*rotatingLog, error) {
	file, err := openAppend(path)
	if err != nil {
		return nil, err
	}
	information, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("heavy: reading the size of %s: %w", path, err)
	}
	return &rotatingLog{path: path, file: file, size: information.Size()}, nil
}

func openAppend(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("heavy: opening the daemon log: %w", err)
	}
	return file, nil
}

func (l *rotatingLog) Write(data []byte) (int, error) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	var rotation error
	if l.size > 0 && l.size+int64(len(data)) > logLimit {
		rotation = l.rotate()
	}
	written, err := l.file.Write(data)
	l.size += int64(written)
	if err != nil {
		return written, err
	}
	return written, rotation
}

// rotate moves the log aside; if that fails, the record goes into the current file, so none is lost.
func (l *rotatingLog) rotate() error {
	if err := os.Rename(l.path, l.path+".1"); err != nil {
		return fmt.Errorf("heavy: moving %s to %s.1: %w", l.path, l.path, err)
	}
	file, err := openAppend(l.path)
	if err != nil {
		return err
	}
	l.file.Close()
	l.file, l.size = file, 0
	return nil
}

func (l *rotatingLog) Close() error {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return l.file.Close()
}
