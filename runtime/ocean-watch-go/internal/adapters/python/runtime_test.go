package python

import (
	"context"
	"errors"
	"reflect"
	"strings"
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

func TestResolverReadsF2PackageVersionFromSelectedRuntime(t *testing.T) {
	var gotExecutable string
	var gotArguments []string
	resolver := Resolver{Run: func(_ context.Context, executable string, arguments []string) (CommandResult, error) {
		gotExecutable = executable
		gotArguments = append([]string(nil), arguments...)
		return CommandResult{Stdout: RequiredF2Version + "\n"}, nil
	}}
	version, err := resolver.F2Version(context.Background(), Runtime{
		Executable: "/synthetic/python", Prefix: []string{"-3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if version != RequiredF2Version || gotExecutable != "/synthetic/python" ||
		!reflect.DeepEqual(gotArguments[:2], []string{"-3", "-c"}) {
		t.Fatalf("unexpected F2 probe: version=%q executable=%q arguments=%#v", version, gotExecutable, gotArguments)
	}
}

func TestResolveF2FallsThroughPythonWithoutPinnedPackage(t *testing.T) {
	resolver := Resolver{
		Getenv: func(string) string { return "" },
		LookPath: func(name string) (string, error) {
			return "/synthetic/" + strings.TrimPrefix(name, "/"), nil
		},
		Run: func(_ context.Context, executable string, arguments []string) (CommandResult, error) {
			if strings.Contains(arguments[len(arguments)-1], "sys.version_info") {
				return CommandResult{Stdout: "3.12.7"}, nil
			}
			if strings.HasSuffix(executable, "python3") {
				return CommandResult{ExitCode: 1}, nil
			}
			return CommandResult{Stdout: RequiredF2Version}, nil
		},
		GOOS: "darwin",
	}
	runtime, err := resolver.ResolveF2(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Executable != "/synthetic/python" {
		t.Fatalf("unexpected F2 runtime: %#v", runtime)
	}
}

func TestResolveF2DiscoversMacOSPackageManagerOutsidePath(t *testing.T) {
	resolver := Resolver{
		Getenv: func(string) string { return "" },
		LookPath: func(name string) (string, error) {
			if name == "/usr/local/opt/python@3.12/libexec/bin/python3" {
				return name, nil
			}
			return "", errors.New("not found")
		},
		Run: func(_ context.Context, _ string, arguments []string) (CommandResult, error) {
			if strings.Contains(arguments[len(arguments)-1], "sys.version_info") {
				return CommandResult{Stdout: "3.12.11"}, nil
			}
			return CommandResult{Stdout: RequiredF2Version}, nil
		},
		GOOS: "darwin",
	}
	runtime, err := resolver.ResolveF2(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Executable != "/usr/local/opt/python@3.12/libexec/bin/python3" || runtime.Source != "platform-discovery" {
		t.Fatalf("unexpected package-manager F2 runtime: %#v", runtime)
	}
}

func TestResolveF2DoesNotIgnoreInvalidExplicitOverride(t *testing.T) {
	resolver := Resolver{
		Getenv: func(name string) string {
			if name == PythonOverrideEnv {
				return "/explicit/python"
			}
			return ""
		},
		LookPath: func(name string) (string, error) { return name, nil },
		Run: func(_ context.Context, _ string, arguments []string) (CommandResult, error) {
			if strings.Contains(arguments[len(arguments)-1], "sys.version_info") {
				return CommandResult{Stdout: "3.12.11"}, nil
			}
			return CommandResult{ExitCode: 1}, nil
		},
		GOOS: "darwin",
	}
	if _, err := resolver.ResolveF2(context.Background()); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("invalid override did not fail closed: %v", err)
	}
}
