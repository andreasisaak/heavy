package sampler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/andreasisaak/heavy/internal/helper"
)

// maxAnswer is the longest answer line of the helper.
const maxAnswer = 4 << 20

// helperProcess is the docker run of the helper with its pipes.
type helperProcess struct {
	command *exec.Cmd
	input   io.WriteCloser
	answers chan []byte
	stderr  *sharedBuffer
	done    chan struct{}
}

func startHelper(command *exec.Cmd) (*helperProcess, error) {
	input, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("heavy: connecting to the helper: %w", err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("heavy: connecting to the helper: %w", err)
	}
	running := &helperProcess{command: command, input: input, answers: make(chan []byte, 1), stderr: &sharedBuffer{}, done: make(chan struct{})}
	command.Stderr = running.stderr
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("heavy: starting the helper: %w", err)
	}
	go func() {
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 64<<10), maxAnswer)
		for scanner.Scan() {
			running.answers <- append([]byte{}, scanner.Bytes()...)
		}
		close(running.answers)
		_ = command.Wait()
		close(running.done)
	}()
	return running, nil
}

// ask sends one request and waits for its answer within the timeout.
func (p *helperProcess) ask(request helper.Request, timeout time.Duration) (helper.Response, error) {
	encoded, err := json.Marshal(request)
	if err != nil {
		return helper.Response{}, fmt.Errorf("heavy: encoding a request to the helper: %w", err)
	}
	if _, err := p.input.Write(append(encoded, '\n')); err != nil {
		return helper.Response{}, p.ended(timeout)
	}
	select {
	case line, open := <-p.answers:
		if !open {
			return helper.Response{}, p.ended(timeout)
		}
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		var response helper.Response
		if err := decoder.Decode(&response); err != nil {
			return helper.Response{}, fmt.Errorf("heavy: the helper answered %q: %w", line, err)
		}
		return response, nil
	case <-time.After(timeout):
		return helper.Response{}, fmt.Errorf("heavy: the helper in the VM did not answer within %s", timeout)
	}
}

// ended describes a helper that went away, with what it wrote to stderr.
func (p *helperProcess) ended(timeout time.Duration) error {
	select {
	case <-p.done:
	case <-time.After(timeout):
	}
	if text := strings.TrimSpace(p.stderr.String()); text != "" {
		return fmt.Errorf("heavy: the helper in the VM ended: %s", text)
	}
	return errors.New("heavy: the helper in the VM ended")
}

// close ends the input and reports whether the helper ended within the timeout.
func (p *helperProcess) close(timeout time.Duration) bool {
	_ = p.input.Close()
	select {
	case <-p.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// kill ends the docker run of the helper and waits for it within the timeout.
func (p *helperProcess) kill(timeout time.Duration) {
	_ = p.command.Process.Kill()
	select {
	case <-p.done:
	case <-time.After(timeout):
	}
}

// sharedBuffer collects stderr of the helper for its error messages.
type sharedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (b *sharedBuffer) Write(data []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.Write(data)
}

func (b *sharedBuffer) String() string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.String()
}
