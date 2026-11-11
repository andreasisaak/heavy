package client

import (
	"errors"
	"fmt"

	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// Status asks the daemon for the queue.
func Status(options daemon.ConnectOptions) (protocol.Status, error) {
	answer, err := ask(options, protocol.Message{Type: protocol.TypeStatus, Status: &protocol.Status{}})
	if err != nil {
		return protocol.Status{}, err
	}
	if answer.Type != protocol.TypeStatus {
		return protocol.Status{}, fmt.Errorf("heavy: the daemon answered the status with a %s message", answer.Type)
	}
	return *answer.Status, nil
}

// Forward moves the run waiting at position one place ahead and returns the daemon's line about it.
func Forward(options daemon.ConnectOptions, position int) (string, error) {
	answer, err := ask(options, protocol.Message{Type: protocol.TypeForward, Forward: &protocol.Forward{Position: position}})
	if err != nil {
		return "", err
	}
	if answer.Type != protocol.TypePrint {
		return "", fmt.Errorf("heavy: the daemon answered the forward with a %s message", answer.Type)
	}
	return answer.Print.Line, nil
}

// Control pauses, resumes or stops a run: kind is protocol.TypePause, TypeResume or TypeTerminate.
func Control(options daemon.ConnectOptions, kind, id string) (string, error) {
	request := protocol.Message{Type: kind}
	target := &protocol.Target{ID: id}
	switch kind {
	case protocol.TypePause:
		request.Pause = target
	case protocol.TypeResume:
		request.Resume = target
	default:
		request.Terminate = target
	}
	answer, err := ask(options, request)
	if err != nil {
		return "", err
	}
	if answer.Type != protocol.TypePrint {
		return "", fmt.Errorf("heavy: the daemon answered the %s with a %s message", kind, answer.Type)
	}
	return answer.Print.Line, nil
}

// StatusIfRunning asks a running daemon for the queue and starts none; found is false without a daemon.
func StatusIfRunning(options daemon.ConnectOptions) (status protocol.Status, found bool, err error) {
	link, found, err := daemon.Reach(options)
	if err != nil || !found {
		return protocol.Status{}, false, err
	}
	defer link.Close()
	if err := link.Send(protocol.Message{Type: protocol.TypeStatus, Status: &protocol.Status{}}); err != nil {
		return protocol.Status{}, false, fmt.Errorf("heavy: asking the daemon for the queue: %w", err)
	}
	answer, err := link.Receive()
	if err != nil {
		return protocol.Status{}, false, fmt.Errorf("heavy: reading the queue from the daemon: %w", err)
	}
	if answer.Type != protocol.TypeStatus {
		return protocol.Status{}, false, fmt.Errorf("heavy: the daemon answered the status with a %s message", answer.Type)
	}
	return *answer.Status, true, nil
}

// ask sends one request and returns the answer; an error message of the daemon becomes the error.
func ask(options daemon.ConnectOptions, request protocol.Message) (protocol.Message, error) {
	link, err := daemon.Connect(options)
	if err != nil {
		return protocol.Message{}, err
	}
	defer link.Close()
	if err := link.Send(request); err != nil {
		return protocol.Message{}, fmt.Errorf("heavy: sending the %s request: %w", request.Type, err)
	}
	answer, err := link.Receive()
	if err != nil {
		return protocol.Message{}, fmt.Errorf("heavy: reading the answer to the %s request: %w", request.Type, err)
	}
	if answer.Type == protocol.TypeError {
		return protocol.Message{}, errors.New(answer.Error.Text)
	}
	return answer, nil
}
