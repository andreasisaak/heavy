package client

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"sync"
	"syscall"
	"time"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// maxCheckedLine bounds the part of one line the red patterns see; output beyond it still passes through.
const maxCheckedLine = 1 << 20

// copyWait bounds the wait for the output of a command that ended, should a child it left behind keep the pipe.
const copyWait = 2 * time.Second

// watched passes the output of a command through as it comes and checks each finished line against the red
// patterns of its list entry (design G8); the first match goes to hits.
type watched struct {
	patterns []*regexp.Regexp
	hits     chan string
	copies   sync.WaitGroup
}

// pipe returns the write end the command gets and copies the read end into out.
func (w *watched) pipe(out io.Writer) (*os.File, error) {
	read, write, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("heavy: making a pipe for the output of the command: %w", err)
	}
	w.copies.Add(1)
	go func() {
		defer w.copies.Done()
		defer read.Close()
		w.copy(read, out)
	}()
	return write, nil
}

func (w *watched) copy(in io.Reader, out io.Writer) {
	buffer := make([]byte, 32<<10)
	var line []byte
	for {
		count, err := in.Read(buffer)
		if count > 0 {
			chunk := buffer[:count]
			_, _ = out.Write(chunk)
			for len(chunk) > 0 {
				end := bytes.IndexByte(chunk, '\n')
				if end < 0 {
					line = appendBounded(line, chunk)
					break
				}
				w.check(string(appendBounded(line, chunk[:end])))
				line, chunk = line[:0], chunk[end+1:]
			}
		}
		if err != nil {
			return
		}
	}
}

func appendBounded(line, more []byte) []byte {
	room := maxCheckedLine - len(line)
	if room <= 0 {
		return line
	}
	if len(more) > room {
		more = more[:room]
	}
	return append(line, more...)
}

func (w *watched) check(line string) {
	for _, pattern := range w.patterns {
		if pattern.MatchString(line) {
			select {
			case w.hits <- line:
			default:
			}
			return
		}
	}
}

// wait waits for the output of the command to pass through, at most copyWait.
func (w *watched) wait() {
	done := make(chan struct{})
	go func() {
		w.copies.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(copyWait):
	}
}

// flush waits for the output of an ended command and takes a red line the loop had no turn for yet.
func (s *session) flush() {
	if s.watch == nil {
		return
	}
	s.watch.wait()
	select {
	case line := <-s.watch.hits:
		if s.redLine == "" {
			s.redLine, s.redAt = line, time.Now()
		}
	default:
	}
}

// redStop asks the daemon to end the run after a red line; without the daemon the client ends its group itself.
func (s *session) redStop(line string) {
	if s.redLine != "" || s.command == nil {
		return
	}
	s.redLine, s.redAt = line, time.Now()
	if err := s.link.Send(protocol.Message{Type: protocol.TypeRed, Red: &protocol.Red{Line: line}}); err != nil {
		fmt.Fprintf(s.options.Stderr, "heavy: telling the daemon the red line failed, ending the command here: %v\n", err)
		if err := syscall.Kill(-s.command.Process.Pid, syscall.SIGTERM); err != nil {
			fmt.Fprintf(s.options.Stderr, "heavy: ending the command failed: %v\n", err)
		}
	}
}

// redEnd reports the end of a run stopped red and names the line last; heavy then exits 1.
func (s *session) redEnd(exitStatus int) int {
	s.report(exitStatus)
	fmt.Fprintf(s.options.Stdout, "heavy: red at %s, stopped the run: %s\n", s.redAt.Format("15:04:05"), s.redLine)
	return 1
}
