package monitor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// lost is why a subscription ended that the monitor makes again after its pause.
type lost string

func (l lost) Error() string { return string(l) }

// follow subscribes for the target, calls subscribed once the daemon took the subscription, and prints the events
// meant for its session until the subscription breaks. A lost error makes Run subscribe again; any other error ends
// Run, such as a daemon that refused this heavy in its hello and would refuse every new try alike. The end of ctx
// closes the connection.
func follow(ctx context.Context, deps Dependencies, whom target, subscribed func() error) error {
	connection, err := deps.Connect()
	var refused *daemon.RefusedError
	if errors.As(err, &refused) {
		return refused
	}
	if err != nil {
		return lost(fmt.Sprintf("reaching the daemon failed (%v)", err))
	}
	defer connection.Close()
	defer context.AfterFunc(ctx, func() { connection.Close() })()
	request := whom.subscription()
	if err := connection.Send(protocol.Message{Type: protocol.TypeSubscribe, Subscribe: &request}); err != nil {
		return lost(fmt.Sprintf("lost the daemon (%v)", err))
	}
	if _, err := expect(connection, protocol.TypeSubscribe); err != nil {
		return err
	}
	if err := subscribed(); err != nil {
		return err
	}
	for {
		if err := deliver(connection, deps, whom); err != nil {
			return err
		}
	}
}

// deliver receives one event and prints its line when the event is meant for the session.
func deliver(connection *daemon.Connection, deps Dependencies, whom target) error {
	message, err := expect(connection, protocol.TypeEvent)
	if err != nil {
		return err
	}
	here, err := whom.place(deps.StateDirectory)
	if err != nil {
		return err
	}
	if !here.wants(*message.Event) {
		return nil
	}
	return notify(deps.Stdout, message.Event.Line)
}

// expect receives the next message, which must be of the type want; a lost connection, an error message or a
// message of another type make the monitor subscribe again.
func expect(connection *daemon.Connection, want string) (protocol.Message, error) {
	message, err := connection.Receive()
	switch {
	case err != nil:
		return protocol.Message{}, lost(fmt.Sprintf("lost the daemon (%v)", err))
	case message.Type == protocol.TypeError:
		return protocol.Message{}, lost(fmt.Sprintf("the daemon refused the subscription (%s)", message.Error.Text))
	case message.Type != want:
		return protocol.Message{}, lost(fmt.Sprintf("expected %s message from the daemon, got %s message", article(want), article(message.Type)))
	}
	return message, nil
}

// article puts "a" or "an" in front of a message type.
func article(word string) string {
	if strings.ContainsAny(word[:1], "aeiou") {
		return "an " + word
	}
	return "a " + word
}
