package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/andreasisaak/heavy/internal/client"
	"github.com/andreasisaak/heavy/internal/daemon"
	"github.com/andreasisaak/heavy/internal/protocol"
	"github.com/andreasisaak/heavy/internal/statusview"
)

// runCommand is `heavy [--directory <dir>] [--agent <name>] <command> [arguments...]`.
func runCommand(arguments []string, stdout, stderr io.Writer) int {
	directory, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "heavy: finding the working directory: %v\n", err)
		return 1
	}
	agent := ""
	for len(arguments) > 0 {
		switch option := arguments[0]; {
		case option == "--directory" || option == "--agent":
			if len(arguments) < 2 {
				fmt.Fprintf(stderr, "heavy: %s needs a %s. %s\n", option, map[string]string{"--directory": "directory", "--agent": "name"}[option], usage)
				return exitUsage
			}
			if option == "--directory" {
				directory, err = filepath.Abs(arguments[1])
				if err != nil {
					fmt.Fprintf(stderr, "heavy: resolving %s: %v\n", arguments[1], err)
					return 1
				}
			} else {
				agent = arguments[1]
			}
			arguments = arguments[2:]
		case len(option) > 2 && option[:2] == "--":
			fmt.Fprintf(stderr, "heavy: unknown option %s. %s\n", option, usage)
			return exitUsage
		default:
			connect, err := connectOptions()
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			return client.Run(client.Options{Argv: arguments, Directory: directory, Agent: agent, Connect: connect, Stdout: stdout, Stderr: stderr})
		}
	}
	fmt.Fprintf(stderr, "heavy: no command given. %s\n", usage)
	return exitUsage
}

// runStatus is `heavy --status [--json]`.
func runStatus(arguments []string, stdout, stderr io.Writer) int {
	asJSON := len(arguments) == 1 && arguments[0] == "--json"
	if len(arguments) > 0 && !asJSON {
		fmt.Fprintf(stderr, "heavy: unknown option %s. %s\n", arguments[0], usage)
		return exitUsage
	}
	connect, err := connectOptions()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	status, err := client.Status(connect)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !asJSON {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(stderr, "heavy: finding the home directory: %v\n", err)
			return 1
		}
		fmt.Fprint(stdout, statusview.Text(status.Runs, home))
		if status.VM != "" {
			fmt.Fprintln(stdout, status.VM)
		}
		return 0
	}
	text, err := statusview.JSON(status.Runs)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprint(stdout, text)
	return 0
}

// runControl is `heavy --pause|--resume|--stop <id>`.
func runControl(option string, arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) != 1 || arguments[0] == "" {
		fmt.Fprintf(stderr, "heavy: %s needs a run id. %s\n", option, usage)
		return exitUsage
	}
	connect, err := connectOptions()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	kind := map[string]string{"--pause": protocol.TypePause, "--resume": protocol.TypeResume, "--stop": protocol.TypeTerminate}[option]
	line, err := client.Control(connect, kind, arguments[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, line)
	return 0
}

// runForward is `heavy --forward <position>`.
func runForward(arguments []string, stdout, stderr io.Writer) int {
	position, err := strconv.Atoi(firstOf(arguments))
	if len(arguments) != 1 || err != nil || !digits(arguments[0]) {
		fmt.Fprintf(stderr, "heavy: --forward needs a position. %s\n", usage)
		return exitUsage
	}
	connect, err := connectOptions()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	line, err := client.Forward(connect, position)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, line)
	return 0
}

// digits reports a position written as digits alone, as 0.6 took it.
func digits(word string) bool {
	for _, character := range word {
		if character < '0' || character > '9' {
			return false
		}
	}
	return word != ""
}

func firstOf(arguments []string) string {
	if len(arguments) == 0 {
		return ""
	}
	return arguments[0]
}

// connectOptions reach the daemon of the state directory with this binary and its identity.
func connectOptions() (daemon.ConnectOptions, error) {
	binary, err := ownBinary()
	if err != nil {
		return daemon.ConnectOptions{}, err
	}
	identity, err := ownIdentity()
	if err != nil {
		return daemon.ConnectOptions{}, err
	}
	state, err := daemon.StateDirectory()
	if err != nil {
		return daemon.ConnectOptions{}, err
	}
	return daemon.ConnectOptions{StateDirectory: state, Binary: binary, Identity: identity}, nil
}
