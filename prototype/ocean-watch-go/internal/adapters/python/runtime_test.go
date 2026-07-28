package python

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestResolverUsesOverrideAndProbesActualRuntime(t *testing.T) {
	var gotExecutable string
	var gotArguments []string
	resolver := Resolver{
		Getenv: func(name string) string {
			if name == PythonOverrideEnv {
				return "/synthetic/python"
			}
			return ""
		},
		LookPath: func(name string) (string, error) { return name, nil },
		Run: func(_ context.Context, executable string, arguments []string) (CommandResult, error) {
			gotExecutable = executable
			gotArguments = append([]string(nil), arguments...)
			return CommandResult{Stdout: "3.12.7\n"}, nil
		},
		GOOS: "darwin",
	}
	runtime, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Executable != "/synthetic/python" || runtime.Source != "environment" || runtime.Version.String() != "3.12.7" {
		t.Fatalf("unexpected runtime: %#v", runtime)
	}
	if gotExecutable != "/synthetic/python" || !reflect.DeepEqual(gotArguments[:2], []string{"-c", "import sys; print('.'.join(str(part) for part in sys.version_info[:3]))"}) {
		t.Fatalf("unexpected probe: %q %#v", gotExecutable, gotArguments)
	}
}

func TestResolverUsesWindowsLauncherPrefix(t *testing.T) {
	resolver := Resolver{
		Getenv: func(string) string { return "" },
		LookPath: func(name string) (string, error) {
			if name == "py" {
				return `C:\\Windows\\py.exe`, nil
			}
			return "", errors.New("not found")
		},
		Run: func(_ context.Context, _ string, arguments []string) (CommandResult, error) {
			if !reflect.DeepEqual(arguments[:2], []string{"-3", "-c"}) {
				t.Fatalf("unexpected Windows probe arguments: %#v", arguments)
			}
			return CommandResult{Stdout: "3.9.18"}, nil
		},
		GOOS: "windows",
	}
	runtime, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(runtime.Prefix, []string{"-3"}) {
		t.Fatalf("unexpected prefix: %#v", runtime.Prefix)
	}
}

func TestResolverFallsThroughUnusableCandidate(t *testing.T) {
	resolver := Resolver{
		Getenv:   func(string) string { return "" },
		LookPath: func(name string) (string, error) { return "/synthetic/" + name, nil },
		Run: func(_ context.Context, executable string, _ []string) (CommandResult, error) {
			if executable == "/synthetic/python3" {
				return CommandResult{ExitCode: 1}, nil
			}
			return CommandResult{Stdout: "3.11.9"}, nil
		},
		GOOS: "linux",
	}
	runtime, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Executable != "/synthetic/python" {
		t.Fatalf("unexpected runtime: %#v", runtime)
	}
}
