package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const sdkVersion = "v1.1.92"
const cliVersionSymbol = "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/cli.Version"
const runtimeVersionSymbol = "main.runtimeVersion"

type pluginManifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type runtimeManifest struct {
	SchemaVersion int               `json:"schema_version"`
	Version       string            `json:"version"`
	Plugin        runtimeFile       `json:"plugin"`
	Resources     map[string]string `json:"resources"`
	SHA256        map[string]string `json:"sha256"`
}

type runtimeFile struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
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
	distribution, err := distributionVersion(root)
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
			if err := build(moduleRoot, temporaryPath, version, distribution, item); err != nil {
				fatal(err)
			}
			if err := equalFiles(destination, temporaryPath); err != nil {
				fatal(fmt.Errorf("%s: %w", name, err))
			}
			fmt.Printf("verified %s\n", name)
			continue
		}
		if err := build(moduleRoot, destination, version, distribution, item); err != nil {
			fatal(err)
		}
		fmt.Printf("built %s\n", destination)
	}
	if strings.TrimSpace(*output) != "" && !*all {
		return
	}
	payload, err := buildRuntimeManifest(root, outputRoot, distribution)
	if err != nil {
		fatal(err)
	}
	manifestPath := filepath.Join(root, ".codex-plugin", "runtime-manifest.json")
	if *verify {
		actual, readErr := os.ReadFile(manifestPath)
		if readErr != nil || !bytes.Equal(actual, payload) {
			fatal(errors.New("prepared runtime manifest does not match the current binaries"))
		}
		fmt.Println("verified runtime-manifest.json")
		return
	}
	if err := os.WriteFile(manifestPath, payload, 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("built %s\n", manifestPath)
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
	version, err := distributionVersion(root)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.SplitN(version, "+", 2)[0]), nil
}

func distributionVersion(root string) (string, error) {
	payload, err := os.ReadFile(filepath.Join(root, ".codex-plugin", "plugin.json"))
	if err != nil {
		return "", err
	}
	var manifest pluginManifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return "", err
	}
	version := strings.TrimSpace(manifest.Version)
	if version == "" {
		return "", errors.New("plugin product version is missing")
	}
	return version, nil
}

func buildRuntimeManifest(root, binaryRoot, version string) ([]byte, error) {
	pluginPath := filepath.Join(root, ".codex-plugin", "plugin.json")
	pluginPayload, err := os.ReadFile(pluginPath)
	if err != nil {
		return nil, fmt.Errorf("hash plugin manifest: %w", err)
	}
	var plugin pluginManifest
	if err := json.Unmarshal(pluginPayload, &plugin); err != nil || plugin.Name != "ocean-watch" || plugin.Version != version {
		return nil, errors.New("plugin manifest identity does not match the runtime version")
	}
	resourcePaths := []string{
		".mcp.json",
		filepath.Join(".claude-plugin", "plugin.json"),
		filepath.Join("f2", "resolve.py"),
		filepath.Join("bin", "ocean-watch-launcher"),
	}
	err = filepath.WalkDir(filepath.Join(root, "skills"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("plugin host resource must not be a symlink: %s", path)
		}
		if entry.Type().IsRegular() {
			relative, relativeErr := filepath.Rel(root, path)
			if relativeErr != nil {
				return relativeErr
			}
			resourcePaths = append(resourcePaths, relative)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("enumerate plugin host resources: %w", err)
	}
	sort.Strings(resourcePaths)
	manifest := runtimeManifest{
		SchemaVersion: 1,
		Version:       version,
		Plugin: runtimeFile{
			Name: "ocean-watch", Version: version, SHA256: digest(pluginPayload),
		},
		Resources: make(map[string]string, len(resourcePaths)),
		SHA256:    make(map[string]string, len(releaseTargets)),
	}
	for _, resourcePath := range resourcePaths {
		resourcePayload, err := os.ReadFile(filepath.Join(root, resourcePath))
		if err != nil {
			return nil, fmt.Errorf("hash runtime resource %s: %w", resourcePath, err)
		}
		manifest.Resources[filepath.ToSlash(resourcePath)] = digest(resourcePayload)
	}
	for _, item := range releaseTargets {
		name := binaryName(item)
		payload, err := os.ReadFile(filepath.Join(binaryRoot, name))
		if err != nil {
			return nil, fmt.Errorf("hash prepared runtime %s: %w", name, err)
		}
		manifest.SHA256[name] = digest(payload)
	}
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(payload, '\n'), nil
}

func digest(payload []byte) string {
	value := sha256.Sum256(payload)
	return hex.EncodeToString(value[:])
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

func build(moduleRoot, destination, version, distribution string, value target) error {
	arguments := []string{
		"build", "-buildvcs=false", "-trimpath",
		"-ldflags", strings.Join([]string{
			"-s", "-w", "-buildid=",
			"-X", cliVersionSymbol + "=" + version,
			"-X", runtimeVersionSymbol + "=" + distribution,
			"-X", "main.sdkVersion=" + sdkVersion,
		}, " "),
		"-o", destination, "./cmd/ocean-watch",
	}
	command := exec.Command("go", arguments...)
	command.Dir = moduleRoot
	environment := append([]string(nil), os.Environ()...)
	environment = setEnvironment(environment, "CGO_ENABLED", "0")
	environment = setEnvironment(environment, "GOTOOLCHAIN", "go1.27.0")
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
