package python

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const PythonOverrideEnv = "OCEAN_WATCH_PYTHON"
const RequiredF2Version = "0.0.1.7"

var ErrRuntimeUnavailable = errors.New("Python runtime required by F2 is unavailable")

type Version struct {
	Major int
	Minor int
	Patch int
}

func (version Version) String() string {
	return fmt.Sprintf("%d.%d.%d", version.Major, version.Minor, version.Patch)
}

func (version Version) AtLeast(minimum Version) bool {
	if version.Major != minimum.Major {
		return version.Major > minimum.Major
	}
	if version.Minor != minimum.Minor {
		return version.Minor > minimum.Minor
	}
	return version.Patch >= minimum.Patch
}

type Runtime struct {
	Executable string
	Prefix     []string
	Version    Version
	Source     string
}

type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type Resolver struct {
	Getenv       func(string) string
	LookPath     func(string) (string, error)
	Run          func(context.Context, string, []string) (CommandResult, error)
	GOOS         string
	ProbeTimeout time.Duration
}

func (resolver Resolver) Resolve(ctx context.Context) (Runtime, error) {
	return resolver.resolve(ctx, false)
}

func (resolver Resolver) ResolveF2(ctx context.Context) (Runtime, error) {
	return resolver.resolve(ctx, true)
}

func (resolver Resolver) resolve(ctx context.Context, requireF2 bool) (Runtime, error) {
	getenv := resolver.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	lookPath := resolver.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	goos := resolver.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	type candidate struct {
		name   string
		prefix []string
		source string
	}
	candidates := []candidate{}
	overridden := false
	if override := strings.TrimSpace(getenv(PythonOverrideEnv)); override != "" {
		overridden = true
		candidates = append(candidates, candidate{name: override, source: "environment"})
	} else if goos == "windows" {
		candidates = append(candidates,
			candidate{name: "py", prefix: []string{"-3"}, source: "platform-discovery"},
			candidate{name: "python", source: "platform-discovery"},
			candidate{name: "python3", source: "platform-discovery"},
		)
	} else {
		candidates = append(candidates,
			candidate{name: "python3", source: "platform-discovery"},
			candidate{name: "python", source: "platform-discovery"},
		)
		if goos == "darwin" {
			candidates = append(candidates,
				candidate{name: "/opt/homebrew/bin/python3", source: "platform-discovery"},
				candidate{name: "/usr/local/bin/python3", source: "platform-discovery"},
			)
			for _, prefix := range []string{"/opt/homebrew", "/usr/local"} {
				for _, version := range []string{"3.14", "3.13", "3.12", "3.11", "3.10"} {
					candidates = append(candidates, candidate{
						name:   prefix + "/opt/python@" + version + "/libexec/bin/python3",
						source: "platform-discovery",
					})
				}
			}
		}
	}

	var failures []string
	seen := map[string]bool{}
	for _, candidate := range candidates {
		path, err := lookPath(candidate.name)
		if err != nil {
			failures = append(failures, candidate.name+": not found")
			continue
		}
		key := path + "\x00" + strings.Join(candidate.prefix, "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		version, err := resolver.probe(ctx, path, candidate.prefix)
		if err != nil {
			failures = append(failures, candidate.name+": "+err.Error())
			continue
		}
		runtimeInfo := Runtime{
			Executable: path,
			Prefix:     append([]string(nil), candidate.prefix...),
			Version:    version,
			Source:     candidate.source,
		}
		if requireF2 {
			if !version.AtLeast(Version{Major: 3, Minor: 10}) {
				failures = append(failures, candidate.name+": Python 3.10 or newer is required")
				continue
			}
			f2Version, f2Err := resolver.F2Version(ctx, runtimeInfo)
			if f2Err != nil || f2Version != RequiredF2Version {
				failures = append(failures, candidate.name+": F2 "+RequiredF2Version+" is unavailable")
				if overridden {
					break
				}
				continue
			}
		}
		return runtimeInfo, nil
	}
	return Runtime{}, fmt.Errorf("%w: %s", ErrRuntimeUnavailable, strings.Join(failures, "; "))
}

func (resolver Resolver) F2Version(ctx context.Context, runtime Runtime) (string, error) {
	timeout := resolver.ProbeTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	arguments := append(append([]string(nil), runtime.Prefix...), "-c", "import importlib.metadata; print(importlib.metadata.version('f2'))")
	result, err := resolver.run(probeCtx, runtime.Executable, arguments)
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("F2 version probe exited with %d", result.ExitCode)
	}
	version := strings.TrimSpace(result.Stdout)
	if version == "" {
		return "", errors.New("F2 version probe returned an empty version")
	}
	return version, nil
}

func (resolver Resolver) probe(ctx context.Context, executable string, prefix []string) (Version, error) {
	timeout := resolver.ProbeTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	arguments := append(append([]string(nil), prefix...), "-c", "import sys; print('.'.join(str(part) for part in sys.version_info[:3]))")
	result, err := resolver.run(probeCtx, executable, arguments)
	if err != nil {
		return Version{}, err
	}
	if result.ExitCode != 0 {
		return Version{}, fmt.Errorf("version probe exited with %d", result.ExitCode)
	}
	return ParseVersion(strings.TrimSpace(result.Stdout))
}

func (resolver Resolver) run(ctx context.Context, executable string, arguments []string) (CommandResult, error) {
	if resolver.Run != nil {
		return resolver.Run(ctx, executable, arguments)
	}
	command := exec.CommandContext(ctx, executable, arguments...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	result := CommandResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err == nil {
		return result, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		result.ExitCode = exitError.ExitCode()
		return result, nil
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return result, err
}

func ParseVersion(value string) (Version, error) {
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) != 3 {
		return Version{}, errors.New("F2 Python version probe returned an unknown version")
	}
	parsed := make([]int, 3)
	for index, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return Version{}, errors.New("F2 Python version probe returned an unknown version")
		}
		parsed[index] = value
	}
	return Version{Major: parsed[0], Minor: parsed[1], Patch: parsed[2]}, nil
}
