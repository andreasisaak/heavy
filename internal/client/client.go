// Package client is `heavy <command>`: it announces the run to the daemon, waits for its turn, runs the command in
// a process group of its own, passes its output through, and ends with its exit status (FR-12 … FR-17).
package client

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/protocol"
)

// Options describe one run.
type Options struct {
	Argv      []string
	Directory string
	Agent     string
	Connect   daemon.ConnectOptions
	Stdout    io.Writer
	Stderr    io.Writer
}

// killAfter is how long the command has after a forwarded signal before SIGKILL.
const killAfter = 10 * time.Second

// Run runs the command and returns the exit status for heavy to end with.
func Run(options Options) int {
	if _, err := exec.LookPath(options.Argv[0]); err != nil {
		fmt.Fprintf(options.Stderr, "heavy: found no command %s, so nothing waits in the queue. To see the queue: heavy --status.\n", options.Argv[0])
		return 127
	}
	register, patterns, err := describe(options)
	if err != nil {
		fmt.Fprintln(options.Stderr, err)
		return 1
	}
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	current := &session{options: options, register: register, signals: signals}
	if len(patterns) > 0 {
		current.watch = &watched{patterns: patterns, hits: make(chan string, 1)}
	}
	return current.run()
}

// session is a run between its registration and its end, across reconnects to new daemons.
type session struct {
	options  Options
	register protocol.Register
	signals  chan os.Signal
	link     *daemon.Connection
	messages chan received
	// adoption is what a new daemon needs to take over the run; nil until the daemon named the run.
	adoption *protocol.Adoption
	printed  bool
	command  *exec.Cmd
	exited   chan int
	// restarting is set when the daemon ends the command to start it again: its end then waits for a new start.
	restarting bool
	// watch checks the output for red lines; nil when the entry has no red patterns. redLine is the first red line.
	watch   *watched
	redLine string
	redAt   time.Time
}

type received struct {
	message protocol.Message
	err     error
}

func (s *session) run() int {
	if err := s.connect(0); err != nil {
		fmt.Fprintln(s.options.Stderr, err)
		return 1
	}
	for {
		select {
		case incoming := <-s.messages:
			if status, done := s.handle(incoming); done {
				return status
			}
		case line := <-s.hits():
			s.redStop(line)
		case exitStatus := <-s.exited:
			s.flush()
			if s.redLine != "" {
				return s.redEnd(exitStatus)
			}
			if s.restarting {
				s.waitAgain(exitStatus)
				continue
			}
			if s.report(exitStatus) {
				continue
			}
			return exitStatus
		case caught := <-s.signals:
			return s.interrupted(caught.(syscall.Signal))
		}
	}
}

// connect reaches the daemon and announces the run, again with what the new daemon needs after a lost one.
func (s *session) connect(startAfter time.Duration) error {
	options := s.options.Connect
	options.StartAfter = startAfter
	link, err := daemon.Connect(options)
	if err != nil {
		return err
	}
	register := s.register
	register.Adopt = s.adoption
	if err := link.Send(protocol.Message{Type: protocol.TypeRegister, Register: &register}); err != nil {
		link.Close()
		return fmt.Errorf("heavy: announcing the run to the daemon: %w", err)
	}
	s.link, s.messages = link, make(chan received, 8)
	go read(link, s.messages)
	return nil
}

func read(link *daemon.Connection, messages chan<- received) {
	for {
		message, err := link.Receive()
		messages <- received{message: message, err: err}
		if err != nil {
			return
		}
	}
}

// handle acts on one message of the daemon; done is true when the run ends with status.
func (s *session) handle(incoming received) (status int, done bool) {
	if incoming.err != nil {
		return s.reconnect()
	}
	message := incoming.message
	switch message.Type {
	case protocol.TypeWaiting:
		s.waiting(*message.Waiting)
	case protocol.TypeStart:
		if err := s.start(*message.Start); err != nil {
			fmt.Fprintln(s.options.Stderr, err)
			s.report(127)
			return 127, true
		}
	case protocol.TypePrint:
		fmt.Fprintln(s.options.Stdout, message.Print.Line)
	case protocol.TypeRestart:
		s.restarting = true
	case protocol.TypeError:
		fmt.Fprintln(s.options.Stderr, message.Error.Text)
		if s.command != nil {
			status := <-s.exited
			s.flush()
			return status, true
		}
		return 1, true
	default:
		fmt.Fprintf(s.options.Stderr, "heavy: the daemon sent a %s message a run does not take\n", message.Type)
		return 1, true
	}
	return 0, false
}

func (s *session) waiting(waiting protocol.Waiting) {
	s.adoption = &protocol.Adoption{ID: waiting.ID, State: "waiting", Since: waiting.Since, Order: waiting.Order}
	if !s.printed {
		s.printed = true
		fmt.Fprintf(s.options.Stdout, "heavy: waiting since %s, #%d in the queue\n", clock(waiting.Since), waiting.Position)
	}
}

// hits are the red lines of the output; none without red patterns.
func (s *session) hits() <-chan string {
	if s.watch == nil {
		return nil
	}
	return s.watch.hits
}

// waitAgain tells the daemon the end of a command it ended for a restart; the run waits again for its start.
func (s *session) waitAgain(exitStatus int) {
	if err := s.link.Send(protocol.Message{Type: protocol.TypeEnded, Ended: &protocol.Ended{ExitStatus: exitStatus}}); err != nil {
		fmt.Fprintf(s.options.Stderr, "heavy: telling the daemon the end of the restarted command failed: %v\n", err)
	}
	s.command, s.exited, s.restarting, s.printed = nil, nil, false, false
}

func clock(unix int64) string {
	return time.Unix(unix, 0).Format("15:04:05")
}
