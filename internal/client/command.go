package client

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/andreasisaak/heavy/internal/protocol"
)

// start runs the command in a process group of its own with HEAVY_RUN and HEAVY_CPUS, its output straight into
// heavy's own, and tells the daemon the group.
func (s *session) start(start protocol.Start) error {
	command := exec.Command(s.options.Argv[0], s.options.Argv[1:]...)
	command.Env = append(environmentWithout(os.Environ(), "HEAVY_RUN", "HEAVY_CPUS"),
		"HEAVY_RUN="+start.ID, "HEAVY_CPUS="+strconv.Itoa(start.Cores))
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var ends []*os.File
	if s.watch != nil {
		stdout, err := s.watch.pipe(s.options.Stdout)
		if err != nil {
			return err
		}
		stderr, err := s.watch.pipe(s.options.Stderr)
		if err != nil {
			stdout.Close()
			return err
		}
		command.Stdout, command.Stderr, ends = stdout, stderr, []*os.File{stdout, stderr}
	}
	fmt.Fprintf(s.options.Stdout, "heavy: started at %s with %d cores (HEAVY_CPUS), running: %s\n",
		clock(start.Since), start.Cores, strings.Join(s.options.Argv, " "))
	err := command.Start()
	for _, end := range ends {
		end.Close()
	}
	if err != nil {
		return fmt.Errorf("heavy: starting %s failed: %w", s.options.Argv[0], err)
	}
	s.command, s.exited = command, make(chan int, 1)
	s.adoption = &protocol.Adoption{ID: start.ID, State: "running", Since: start.Since, Order: s.order(), ProcessGroup: command.Process.Pid, Cores: start.Cores}
	go func() { s.exited <- exitStatus(command.Wait()) }()
	answer := start
	answer.ProcessGroup = command.Process.Pid
	if err := s.link.Send(protocol.Message{Type: protocol.TypeStart, Start: &answer}); err != nil {
		fmt.Fprintf(s.options.Stderr, "heavy: telling the daemon the process group failed: %v\n", err)
	}
	return nil
}

func (s *session) order() float64 {
	if s.adoption == nil {
		return float64(time.Now().UnixNano()) / float64(time.Second)
	}
	return s.adoption.Order
}

// exitStatus is the status the shell would show: the exit code, or 128 plus the signal that ended the command;
// 1 when waiting for the command failed.
func exitStatus(err error) int {
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		return 1
	}
	if status, ok := exitError.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return exitError.ExitCode()
}

// report tells the daemon how the command ended, through a new daemon if the old one is gone, and prints the lines
// the daemon sent before it closes the connection, such as the line of a stop. It reports true when the daemon had
// ended the command for a restart, which the client may see only after the end: the run then waits again.
func (s *session) report(exitStatus int) bool {
	ended := protocol.Message{Type: protocol.TypeEnded, Ended: &protocol.Ended{ExitStatus: exitStatus, Red: s.redLine}}
	if s.link.Send(ended) == nil {
		if s.drain() {
			return true
		}
		s.link.Close()
		return false
	}
	if err := s.connect(); err != nil {
		fmt.Fprintf(s.options.Stderr, "heavy: the daemon did not get the end of the run: %v\n", err)
		return false
	}
	if err := s.link.Send(ended); err != nil {
		fmt.Fprintf(s.options.Stderr, "heavy: the daemon did not get the end of the run: %v\n", err)
	}
	s.link.Close()
	return false
}

// interrupted passes SIGTERM or SIGINT on to the command's group, waits for the command, and ends with 128 plus
// the signal; a run that still waits just leaves the queue.
func (s *session) interrupted(caught syscall.Signal) int {
	if s.command == nil {
		s.link.Close()
		return 128 + int(caught)
	}
	group := s.command.Process.Pid
	if err := syscall.Kill(-group, caught); err != nil {
		fmt.Fprintf(s.options.Stderr, "heavy: passing %s to the command failed: %v\n", caught, err)
	}
	select {
	case <-s.exited:
	case <-time.After(killAfter):
		if err := syscall.Kill(-group, syscall.SIGKILL); err != nil {
			fmt.Fprintf(s.options.Stderr, "heavy: killing the command failed: %v\n", err)
		}
		<-s.exited
	}
	s.report(128 + int(caught))
	return 128 + int(caught)
}

// reconnect announces the run to a new daemon after the old one went away, while the command keeps running.
func (s *session) reconnect() (int, bool) {
	s.link.Close()
	fmt.Fprintf(s.options.Stderr, "heavy: lost the daemon at %s, announcing the run to a new one\n", time.Now().Format("15:04:05"))
	for {
		err := s.connect()
		if err == nil {
			return 0, false
		}
		fmt.Fprintln(s.options.Stderr, err)
		select {
		case exitStatus := <-s.exited:
			s.exited <- exitStatus
			return 0, false
		case caught := <-s.signals:
			return s.interrupted(caught.(syscall.Signal)), true
		case <-time.After(time.Second):
		}
	}
}

func environmentWithout(environment []string, names ...string) []string {
	var kept []string
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		drop := false
		for _, unwanted := range names {
			drop = drop || name == unwanted
		}
		if !drop {
			kept = append(kept, entry)
		}
	}
	return kept
}

// drain prints the lines that reached the client before the daemon closed the connection after the end. A place in
// the queue instead means the daemon restarted the run: drain then reports true and the run waits again.
func (s *session) drain() bool {
	deadline := time.After(drainFor)
	for {
		select {
		case incoming := <-s.messages:
			if incoming.err != nil {
				return false
			}
			switch incoming.message.Type {
			case protocol.TypePrint:
				fmt.Fprintln(s.options.Stdout, incoming.message.Print.Line)
			case protocol.TypeWaiting:
				s.command, s.exited, s.restarting, s.printed = nil, nil, false, false
				s.waiting(*incoming.message.Waiting)
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// drainFor bounds the wait for the daemon to close the connection after the end of a run.
const drainFor = 2 * time.Second
