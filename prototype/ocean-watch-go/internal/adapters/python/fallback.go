package python

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

type Fallback struct {
	Executable string
	Prefix     []string
	Entrypoint string
	Directory  string
	Env        []string
	Resolver   Resolver
}

func (fallback Fallback) Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	executable := fallback.Executable
	prefix := append([]string(nil), fallback.Prefix...)
	if executable == "" {
		resolved, err := fallback.Resolver.Resolve(ctx)
		if err != nil {
			return 1
		}
		executable = resolved.Executable
		prefix = append(prefix, resolved.Prefix...)
	}
	entrypoint := fallback.Entrypoint
	if entrypoint == "" {
		entrypoint = filepath.Join("skills", "ads-plan-monitor", "run.py")
	}
	arguments := append(prefix, entrypoint)
	arguments = append(arguments, args...)
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Stdout = stdout
	command.Stderr = stderr
	command.Dir = fallback.Directory
	if len(fallback.Env) == 0 {
		command.Env = os.Environ()
	} else {
		command.Env = append([]string(nil), fallback.Env...)
	}
	err := command.Run()
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		if code := exitError.ExitCode(); code >= 0 {
			return code
		}
	}
	if ctx.Err() != nil {
		return 130
	}
	return 1
}
