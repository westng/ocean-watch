package main

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestResolveExecutableUsesPathForBareNames(t *testing.T) {
	name := "go"
	want, err := exec.LookPath(name)
	if err != nil {
		t.Skip("Go executable is not on PATH")
	}
	got, err := resolveExecutable(name)
	if err != nil {
		t.Fatal(err)
	}
	want, err = filepath.Abs(want)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
