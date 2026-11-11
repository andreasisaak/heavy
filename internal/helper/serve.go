package helper

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"syscall"
)

// maxRequest is the longest request line, the same bound as the protocol between client and daemon.
const maxRequest = 4 << 20

// Options are the /proc root the helper reads, the page size of the kernel and how it sends a signal.
type Options struct {
	Root     string
	PageSize int64
	Kill     func(pid int, signal syscall.Signal) error
	// Unprivileged is the helper inside the daemon of a Linux user, without a container: it reads and signals
	// only what that user may, while the helper in the Docker VM runs as root.
	Unprivileged bool
}

// Serve answers each request line from in with one line on out until in ends.
func Serve(options Options, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64<<10), maxRequest)
	for scanner.Scan() {
		encoded, err := json.Marshal(answer(options, scanner.Bytes()))
		if err != nil {
			return fmt.Errorf("heavy: encoding an answer: %w", err)
		}
		if _, err := out.Write(append(encoded, '\n')); err != nil {
			return fmt.Errorf("heavy: writing an answer: %w", err)
		}
	}
	if errors.Is(scanner.Err(), bufio.ErrTooLong) {
		return fmt.Errorf("heavy: a request is longer than %d bytes", maxRequest)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("heavy: reading the requests: %w", err)
	}
	return nil
}

func answer(options Options, line []byte) Response {
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	var request Request
	if err := decoder.Decode(&request); err != nil {
		return Response{Error: fmt.Sprintf("heavy: expected a JSON request, got %q: %v", line, err)}
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Response{Error: fmt.Sprintf("heavy: expected one JSON request per line, got %q", line)}
	}
	switch request.Type {
	case "sample":
		sample, err := ReadSample(options)
		if err != nil {
			return Response{Error: err.Error()}
		}
		return Response{Sample: &sample}
	case "find":
		if request.Tag == "" {
			return Response{Error: "heavy: find needs a tag"}
		}
		processes, err := Find(options, request.Tag)
		if err != nil {
			return Response{Error: err.Error()}
		}
		return Response{Found: &Found{Processes: processes}}
	case "containers":
		containers, err := ContainerUsage(options)
		if err != nil {
			return Response{Error: err.Error()}
		}
		return Response{Usage: &Usage{Containers: containers}}
	case "signal":
		signaled, err := SignalTagged(options, request)
		if err != nil {
			return Response{Error: err.Error()}
		}
		return Response{Signaled: &signaled}
	}
	return Response{Error: "unknown request " + request.Type}
}
