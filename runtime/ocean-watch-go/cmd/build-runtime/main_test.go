package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuildInjectsProductVersion(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), binaryName(target{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}))
	if err := build(
		filepath.Join(root, "runtime", "ocean-watch-go"),
		destination,
		"9.8.7",
		target{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH},
	); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(destination, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("built runtime failed: %v: %s", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != "ocean-watch 9.8.7" {
		t.Fatalf("built runtime version = %q, want %q", got, "ocean-watch 9.8.7")
	}
}
