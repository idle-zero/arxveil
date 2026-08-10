package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const usage = `Arxveil development command-line interface.

Usage:
  arxveil backend start [--with-agents] [--detach]
  arxveil backend stop [--with-agents]
  arxveil backend status [--with-agents]
  arxveil backend logs [--with-agents] [service...]
  arxveil backend reset [--with-agents] --yes
  arxveil generate go
  arxveil help

Commands:
  backend start   Build and start the local backend stack in the foreground.
  backend stop    Stop the local backend stack while preserving its data.
  backend status  Show the stack's Compose services.
  backend logs    Follow Compose logs, optionally for named services.
  backend reset   Stop the stack and permanently remove its Docker volumes.
  generate go     Generate committed Go bindings from the protobuf contract.
`

type commandRunner func(name string, args []string, dir string) error

func main() {
	workingDirectory, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "arxveil: determine working directory: %v\n", err)
		os.Exit(1)
	}

	exitCode := run(os.Args[1:], workingDirectory, executeCommand, os.Stdout, os.Stderr)
	os.Exit(exitCode)
}

func run(arguments []string, workingDirectory string, execute commandRunner, stdout, stderr io.Writer) int {
	if len(arguments) == 0 || arguments[0] == "help" || arguments[0] == "--help" || arguments[0] == "-h" {
		fmt.Fprint(stdout, usage)
		return 0
	}

	root, err := findRepositoryRoot(workingDirectory)
	if err != nil {
		fmt.Fprintf(stderr, "arxveil: %v\n", err)
		return 1
	}

	var command []string
	switch arguments[0] {
	case "backend":
		command, err = backendCommand(arguments[1:], root)
	case "generate":
		command, err = generateCommand(arguments[1:], root)
	default:
		err = fmt.Errorf("unknown command %q", arguments[0])
	}

	if err != nil {
		fmt.Fprintf(stderr, "arxveil: %v\n\n%s", err, usage)
		return 1
	}

	if err := execute("docker", command, root); err != nil {
		fmt.Fprintf(stderr, "arxveil: docker command failed: %v\n", err)
		return 1
	}

	return 0
}

func backendCommand(arguments []string, root string) ([]string, error) {
	if len(arguments) == 0 {
		return nil, errors.New("missing backend command")
	}

	switch arguments[0] {
	case "start":
		flags := flag.NewFlagSet("backend start", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		withAgents := flags.Bool("with-agents", false, "start the local agent fleet")
		detach := flags.Bool("detach", false, "run the stack in the background")
		if err := flags.Parse(arguments[1:]); err != nil {
			return nil, err
		}
		if flags.NArg() != 0 {
			return nil, fmt.Errorf("unexpected backend start arguments: %s", strings.Join(flags.Args(), " "))
		}

		command := composeCommand(root, *withAgents)
		command = append(command, "up", "--build")
		if *detach {
			command = append(command, "--detach")
		}
		return command, nil

	case "stop":
		withAgents, err := parseWithAgents(arguments[1:], "backend stop")
		if err != nil {
			return nil, err
		}
		return append(composeCommand(root, withAgents), "down"), nil

	case "status":
		withAgents, err := parseWithAgents(arguments[1:], "backend status")
		if err != nil {
			return nil, err
		}
		return append(composeCommand(root, withAgents), "ps"), nil

	case "logs":
		flags := flag.NewFlagSet("backend logs", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		withAgents := flags.Bool("with-agents", false, "include the local agent fleet")
		if err := flags.Parse(arguments[1:]); err != nil {
			return nil, err
		}
		command := append(composeCommand(root, *withAgents), "logs", "--follow")
		return append(command, flags.Args()...), nil

	case "reset":
		flags := flag.NewFlagSet("backend reset", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		withAgents := flags.Bool("with-agents", false, "include the local agent fleet")
		yes := flags.Bool("yes", false, "confirm permanent Docker-volume deletion")
		if err := flags.Parse(arguments[1:]); err != nil {
			return nil, err
		}
		if flags.NArg() != 0 {
			return nil, fmt.Errorf("unexpected backend reset arguments: %s", strings.Join(flags.Args(), " "))
		}
		if !*yes {
			return nil, errors.New("backend reset permanently removes Docker volumes; rerun with --yes")
		}
		return append(composeCommand(root, *withAgents), "down", "--volumes", "--remove-orphans"), nil

	default:
		return nil, fmt.Errorf("unknown backend command %q", arguments[0])
	}
}

func generateCommand(arguments []string, root string) ([]string, error) {
	if len(arguments) != 1 || arguments[0] != "go" {
		return nil, errors.New("usage: arxveil generate go")
	}

	return []string{
		"build",
		"--target", "output",
		"--file", filepath.Join(root, "proto", "Dockerfile.go"),
		"--output", "type=local,dest=" + filepath.Join(root, "server", "internal", "gen"),
		root,
	}, nil
}

func parseWithAgents(arguments []string, commandName string) (bool, error) {
	flags := flag.NewFlagSet(commandName, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	withAgents := flags.Bool("with-agents", false, "include the local agent fleet")
	if err := flags.Parse(arguments); err != nil {
		return false, err
	}
	if flags.NArg() != 0 {
		return false, fmt.Errorf("unexpected %s arguments: %s", commandName, strings.Join(flags.Args(), " "))
	}
	return *withAgents, nil
}

func composeCommand(root string, withAgents bool) []string {
	command := []string{"compose", "-f", filepath.Join(root, "infra", "compose.yaml")}
	if withAgents {
		command = append(command, "-f", filepath.Join(root, "infra", "compose.agents.yaml"))
	}
	return command
}

func findRepositoryRoot(start string) (string, error) {
	directory, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}

	for {
		if isFile(filepath.Join(directory, "infra", "compose.yaml")) &&
			isFile(filepath.Join(directory, "proto", "Dockerfile.go")) {
			return directory, nil
		}

		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errors.New("could not find an Arxveil repository root")
		}
		directory = parent
	}
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func executeCommand(name string, arguments []string, directory string) error {
	command := exec.Command(name, arguments...)
	command.Dir = directory
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}
