// Package protocol is the language of heavy's clients and its daemon: JSON lines over the
// socket $STATE/daemon.sock. Every connection opens with a Hello in each direction, which
// names the protocol, so a daemon and a client of different protocols notice it at once;
// Messages follow, each with a type and the one payload of that type.
package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Version is the protocol this heavy speaks.
const Version = 1

// MaxLine is the longest line either side takes.
const MaxLine = 4 << 20

// Hello opens every connection, in both directions.
type Hello struct {
	Protocol int    `json:"protocol"`
	Version  string `json:"version"`
	Stamp    string `json:"stamp"`
	// Error is the daemon's answer to a connection it refuses.
	Error string `json:"error,omitempty"`
	// Handover is the daemon's answer to a newer heavy: it ends, so the newer one starts its own.
	Handover bool `json:"handover,omitempty"`
	// Reader marks a heavy that only reads (guard, shim, herdr refresh, dashboard): it starts no daemon, so it
	// replaces none either.
	Reader bool `json:"reader,omitempty"`
}

// Write sends a hello or a message as one JSON line.
func Write[T Hello | Message](writer io.Writer, value T) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("heavy: encoding %#v: %w", value, err)
	}
	_, err = writer.Write(append(data, '\n'))
	return err
}

// ReadHello reads the hello of the other side. A hello of another protocol comes back with its protocol number
// alone, whatever else it holds, so the caller can name both protocols. A connection closed before its hello
// gives io.EOF.
func ReadHello(reader *bufio.Reader) (Hello, error) {
	line, err := readLine(reader)
	if err != nil {
		return Hello{}, err
	}
	var number struct {
		Protocol int `json:"protocol"`
	}
	if err := json.Unmarshal(line, &number); err != nil {
		return Hello{}, fmt.Errorf("heavy: expected a hello of protocol %d, got %s: %w", Version, line, err)
	}
	if number.Protocol != Version {
		return Hello{Protocol: number.Protocol}, nil
	}
	var hello Hello
	if err := strict(line, &hello); err != nil {
		return Hello{}, fmt.Errorf("heavy: expected a hello of protocol %d, got %s: %w", Version, line, err)
	}
	return hello, nil
}

// readLine reads one line without its line break, at most MaxLine bytes; io.EOF only when the other side closed
// between lines.
func readLine(reader *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := reader.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > MaxLine+1 {
			return nil, fmt.Errorf("heavy: a line from the other side is longer than %d bytes", MaxLine)
		}
		switch {
		case err == nil:
			return bytes.TrimSuffix(line, []byte("\n")), nil
		case errors.Is(err, bufio.ErrBufferFull):
		case errors.Is(err, io.EOF) && len(line) == 0:
			return nil, io.EOF
		default:
			return nil, fmt.Errorf("heavy: reading from the other side after %.100q: %w", line, err)
		}
	}
}

func strict(line []byte, target *Hello) error {
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
