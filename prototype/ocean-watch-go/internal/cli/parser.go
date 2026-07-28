package cli

import (
	"errors"
	"strings"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/contracts"
)

type Invocation struct {
	Command              contracts.Command
	Arguments            []string
	PassthroughArguments []string
	Version              bool
	Help                 bool
}

func Parse(args []string) (Invocation, error) {
	if len(args) == 1 && args[0] == "--version" {
		return Invocation{Version: true}, nil
	}
	if len(args) == 1 && isHelpArgument(args[0]) {
		return Invocation{Help: true, PassthroughArguments: append([]string(nil), args...)}, nil
	}
	if len(args) == 2 && isHelpArgument(args[1]) {
		if !contracts.HasDomain(args[0]) {
			return Invocation{}, errors.New("unknown command domain: " + args[0])
		}
		return Invocation{Help: true, PassthroughArguments: append([]string(nil), args...)}, nil
	}
	if len(args) < 2 {
		return Invocation{}, errors.New("a command domain and action are required")
	}
	command, ok := contracts.Lookup(args[0], args[1])
	if !ok {
		return Invocation{}, errors.New("unknown command: " + strings.Join(args[:2], " "))
	}
	help := false
	for _, argument := range args[2:] {
		if isHelpArgument(argument) {
			help = true
			break
		}
	}
	return Invocation{Command: command, Arguments: append([]string(nil), args[2:]...), Help: help}, nil
}

func isHelpArgument(argument string) bool {
	return argument == "-h" || argument == "--help"
}
