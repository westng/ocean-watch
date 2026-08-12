package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const sdkVersion = "v1.1.92"
const cliVersionSymbol = "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/cli.Version"

type pluginManifest struct {
	Version string `json:"version"`
}

type target struct {
	GOOS   string
	GOARCH string
}

var releaseTargets = []target{
	{GOOS: "darwin", GOARCH: "amd64"},
	{GOOS: "darwin", GOARCH: "arm64"},
	{GOOS: "linux", GOARCH: "amd64"},
	{GOOS: "linux", GOARCH: "arm64"},
	{GOOS: "windows", GOARCH: "amd64"},
}

func main() {
	all := flag.Bool("all", false, "build every supported release target")
	verify := flag.Bool("verify", false, "verify tracked binaries reproduce exactly")
	output := flag.String("output", "", "override the binary output directory")
	flag.Parse()
	if *verify && !*all {
		fatal(errors.New("--verify requires --all"))
	}
	root, err := repositoryRoot()
	if err != nil {
		fatal(err)
	}
	version, err := productVersion(root)
	if err != nil {
		fatal(err)
	}
	targets := []target{hostTarget()}
	if *all {
		targets = append([]target(nil), releaseTargets...)
	}
	for _, item := range targets {
		if !supported(item) {
			fatal(fmt.Errorf("unsupported runtime target %s/%s", item.GOOS, item.GOARCH))
		}
	}
	outputRoot := strings.TrimSpace(*output)
	if outputRoot == "" {
		outputRoot = filepath.Join(root, ".codex-plugin", "bin")
	} else if !filepath.IsAbs(outputRoot) {
		outputRoot = filepath.Join(root, outputRoot)
	}
	if err := os.MkdirAll(outputRoot, 0o755); err != nil {
		fatal(err)
	}
	moduleRoot := filepath.Join(root, "runtime", "ocean-watch-go")
	for _, item := range targets {
		name := binaryName(item)
		destination := filepath.Join(outputRoot, name)
		if *verify {
			temporary, err := os.CreateTemp("", "ocean-watch-runtime-*")
			if err != nil {
				fatal(err)
			}
			temporaryPath := temporary.Name()
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
			defer os.Remove(temporaryPath)
			if err := build(moduleRoot, temporaryPath, version, item); err != nil {
				fatal(err)
			}
			if err := equalFiles(destination, temporaryPath); err != nil {
				fatal(fmt.Errorf("%s: %w", name, err))
			}
			fmt.Printf("verified %s\n", name)
			continue
		}
		if err := build(moduleRoot, destination, version, item); err != nil {
			fatal(err)
		}
		fmt.Printf("built %s\n", destination)
	}
}

func hostTarget() target {
	result := target{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	if result.GOOS == "darwin" {
		if output, err := exec.Command("sysctl", "-n", "hw.optional.arm64").Output(); err == nil && strings.TrimSpace(string(output)) == "1" {
			result.GOARCH = "arm64"
			return result
		}
	}
	if result.GOOS != "darwin" && result.GOOS != "linux" {
		return result
	}
	output, err := exec.Command("uname", "-m").Output()
	if err != nil {
		return result
	}
	switch strings.TrimSpace(string(output)) {
	case "x86_64", "amd64":
		result.GOARCH = "amd64"
	case "arm64", "aarch64":
		result.GOARCH = "arm64"
	}
	return result
}

func repositoryRoot() (string, error) {
	current, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if info, statErr := os.Stat(filepath.Join(current, ".codex-plugin", "plugin.json")); statErr == nil && info.Mode().IsRegular() {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("repository root was not found")
		}
		current = parent
	}
}

func productVersion(root string) (string, error) {
	payload, err := os.ReadFile(filepath.Join(root, ".codex-plugin", "plugin.json"))
	if err != nil {
		return "", err
	}
	var manifest pluginManifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return "", err
	}
	version := strings.TrimSpace(strings.SplitN(manifest.Version, "+", 2)[0])
	if version == "" {
		return "", errors.New("plugin product version is missing")
	}
	return version, nil
}

func supported(value target) bool {
	for _, item := range releaseTargets {
		if item == value {
			return true
		}
	}
	return false
}

func binaryName(value target) string {
	name := "ocean-watch_" + value.GOOS + "_" + value.GOARCH
	if value.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func build(moduleRoot, destination, version string, value target) error {
	arguments := []string{
		"build", "-buildvcs=false", "-trimpath",
		"-ldflags", strings.Join([]string{
			"-s", "-w", "-buildid=",
			"-X", cliVersionSymbol + "=" + version,
			"-X", "main.sdkVersion=" + sdkVersion,
		}, " "),
		"-o", destination, "./cmd/ocean-watch",
	}
	command := exec.Command("go", arguments...)
	command.Dir = moduleRoot
	environment := append([]string(nil), os.Environ()...)
	environment = setEnvironment(environment, "CGO_ENABLED", "0")
	environment = setEnvironment(environment, "GOTOOLCHAIN", "go1.26.5")
	environment = setEnvironment(environment, "GOOS", value.GOOS)
	environment = setEnvironment(environment, "GOARCH", value.GOARCH)
	environment = setEnvironment(environment, "TZ", "UTC")
	environment = setEnvironment(environment, "LC_ALL", "C")
	command.Env = environment
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return fmt.Errorf("go build %s/%s failed: %s", value.GOOS, value.GOARCH, strings.TrimSpace(output.String()))
	}
	if value.GOOS != "windows" {
		if err := os.Chmod(destination, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func setEnvironment(values []string, name, value string) []string {
	prefix := name + "="
	result := make([]string, 0, len(values)+1)
	for _, item := range values {
		if !strings.HasPrefix(item, prefix) {
			result = append(result, item)
		}
	}
	return append(result, prefix+value)
}

func equalFiles(expectedPath, actualPath string) error {
	expected, err := os.ReadFile(expectedPath)
	if err != nil {
		return fmt.Errorf("prepared runtime is missing: %w", err)
	}
	actual, err := os.ReadFile(actualPath)
	if err != nil {
		return err
	}
	if !bytes.Equal(expected, actual) {
		return errors.New("prepared runtime does not match the current source")
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func init() {
	sort.Slice(releaseTargets, func(left, right int) bool {
		if releaseTargets[left].GOOS == releaseTargets[right].GOOS {
			return releaseTargets[left].GOARCH < releaseTargets[right].GOARCH
		}
		return releaseTargets[left].GOOS < releaseTargets[right].GOOS
	})
}
