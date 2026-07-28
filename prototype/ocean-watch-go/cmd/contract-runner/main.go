package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/contractrunner"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(os.Args) < 2 {
		fail("capture-python or compare is required", 2)
	}
	var code int
	switch os.Args[1] {
	case "capture-python":
		code = capturePython(ctx, os.Args[2:])
	case "compare":
		code = compare(ctx, os.Args[2:])
	default:
		fail("unknown contract-runner action: "+os.Args[1], 2)
	}
	os.Exit(code)
}

func capturePython(ctx context.Context, arguments []string) int {
	flags := flag.NewFlagSet("capture-python", flag.ContinueOnError)
	manifest := flags.String("manifest", "", "command manifest")
	fixtures := flags.String("fixtures", "", "fixture directory")
	output := flags.String("out", "", "evidence output directory")
	gitSHA := flags.String("git-sha", "", "full candidate git SHA")
	python := flags.String("python", "", "Python executable")
	entrypoint := flags.String("python-entrypoint", "", "Python Ocean Watch entrypoint")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *manifest == "" || *output == "" || *gitSHA == "" || len(flags.Args()) != 0 {
		fail("--manifest, --out, and --git-sha are required", 2)
	}
	repositoryRoot, resolvedEntrypoint, resolvedPython, err := resolvePython(*manifest, *entrypoint, *python)
	if err != nil {
		fail(err.Error(), 2)
	}
	program := contractrunner.Program{
		Executable: resolvedPython, Prefix: []string{resolvedEntrypoint},
		Env: map[string]string{"PYTHONPATH": filepath.Join(repositoryRoot, "skills", "ads-plan-monitor", "src")},
	}
	capture, err := contractrunner.CapturePython(ctx, contractrunner.CaptureOptions{
		ManifestPath: *manifest, FixturesPath: *fixtures, OutputPath: *output,
		Program: program, RepositoryRoot: repositoryRoot, GitSHA: *gitSHA,
	})
	if err != nil {
		fail(err.Error(), 1)
	}
	printJSON(map[string]any{"ok": true, "case_count": len(capture.Cases), "out": *output})
	return 0
}

func compare(ctx context.Context, arguments []string) int {
	flags := flag.NewFlagSet("compare", flag.ContinueOnError)
	manifest := flags.String("manifest", "", "command manifest")
	baseline := flags.String("baseline", "", "Python baseline directory or capture")
	candidate := flags.String("candidate", "", "candidate Ocean Watch executable")
	output := flags.String("out", "", "comparison output directory")
	gitSHA := flags.String("git-sha", "", "full candidate git SHA")
	candidateIdentityPath := flags.String("candidate-identity", "", "immutable candidate identity JSON")
	python := flags.String("python", "", "Python executable for fallback")
	entrypoint := flags.String("python-entrypoint", "", "Python Ocean Watch entrypoint for fallback")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if *manifest == "" || *baseline == "" || *candidate == "" || *output == "" || *gitSHA == "" || len(flags.Args()) != 0 {
		fail("--manifest, --baseline, --candidate, --out, and --git-sha are required", 2)
	}
	repositoryRoot, resolvedEntrypoint, resolvedPython, err := resolvePython(*manifest, *entrypoint, *python)
	if err != nil {
		fail(err.Error(), 2)
	}
	resolvedCandidate, err := resolveExecutable(*candidate)
	if err != nil {
		fail(err.Error(), 2)
	}
	var candidateIdentity *contractrunner.CandidateIdentity
	if *candidateIdentityPath != "" {
		candidateIdentity, err = contractrunner.LoadCandidateIdentity(*candidateIdentityPath, *gitSHA)
		if err != nil {
			fail(err.Error(), 2)
		}
	}
	program := contractrunner.Program{
		Executable: resolvedCandidate,
		Env: map[string]string{
			"OCEAN_WATCH_PYTHON":            resolvedPython,
			"OCEAN_WATCH_PYTHON_ENTRYPOINT": resolvedEntrypoint,
			"PYTHONPATH":                    filepath.Join(repositoryRoot, "skills", "ads-plan-monitor", "src"),
		},
	}
	report, err := contractrunner.Compare(ctx, contractrunner.CompareOptions{
		ManifestPath: *manifest, BaselinePath: *baseline, Candidate: program,
		OutputPath: *output, RepositoryRoot: repositoryRoot, GitSHA: *gitSHA,
		CandidateIdentity: candidateIdentity,
	})
	if err != nil {
		fail(err.Error(), 1)
	}
	printJSON(map[string]any{"ok": report.Failed == 0, "total": report.Total, "passed": report.Passed, "failed": report.Failed, "out": *output})
	if report.Failed != 0 {
		return 1
	}
	return 0
}

func resolvePython(manifest, entrypoint, python string) (string, string, string, error) {
	absoluteManifest, err := filepath.Abs(manifest)
	if err != nil {
		return "", "", "", err
	}
	repositoryRoot := filepath.Dir(filepath.Dir(absoluteManifest))
	if entrypoint == "" {
		entrypoint = filepath.Join(repositoryRoot, "skills", "ads-plan-monitor", "run.py")
	}
	if python == "" {
		virtual := filepath.Join(repositoryRoot, ".venv", "bin", "python")
		if _, err := os.Stat(virtual); err == nil {
			python = virtual
		} else {
			python = "python3"
		}
	}
	resolvedPython, err := resolveExecutable(python)
	if err != nil {
		return "", "", "", err
	}
	resolvedEntrypoint, err := filepath.Abs(entrypoint)
	if err != nil {
		return "", "", "", err
	}
	if _, err := os.Stat(resolvedEntrypoint); err != nil {
		return "", "", "", fmt.Errorf("Python entrypoint is unavailable: %w", err)
	}
	return repositoryRoot, resolvedEntrypoint, resolvedPython, nil
}

func resolveExecutable(name string) (string, error) {
	resolved, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("resolve executable %s: %w", name, err)
	}
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	return absolute, nil
}

func printJSON(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func fail(message string, code int) {
	_, _ = fmt.Fprintln(os.Stderr, message)
	os.Exit(code)
}
