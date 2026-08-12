package environment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/python"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/onboarding"
)

var codexVersionPattern = regexp.MustCompile(`(?:^|[^0-9])(\d+)\.(\d+)\.(\d+)(?:[^0-9]|$)`)

var minimumPython = python.Version{Major: 3, Minor: 10}
var minimumCodex = python.Version{Major: 0, Minor: 144, Patch: 1}

const minimumPythonText = "3.10"

type CredentialBackend interface {
	BackendName() string
}

type Probe struct {
	PythonResolver python.Resolver
	Credentials    CredentialBackend
	LookPath       func(string) (string, error)
	Run            func(context.Context, string, []string) (python.CommandResult, error)
	Listen         func(string, string) (net.Listener, error)
	GOOS           string
	GOARCH         string
	SystemRelease  func(context.Context, string) string
	CommandTimeout time.Duration
}

func (probe Probe) Python(ctx context.Context) onboarding.Check {
	runtimeInfo, err := probe.PythonResolver.Resolve(ctx)
	ready := err == nil && runtimeInfo.Version.AtLeast(minimumPython)
	result := onboarding.Check{
		"id": "python", "required": true,
		"status": "blocked", "minimum_version": minimumPythonText,
		"message":     "Python 3.10 or newer is required.",
		"remediation": "Install Python 3.10 or newer, then start a new Codex task.",
	}
	if runtimeInfo.Executable != "" {
		result["version"] = runtimeInfo.Version.String()
		result["executable"] = runtimeInfo.Executable
	}
	if ready {
		result["status"] = "ready"
		result["message"] = "Python runtime is supported."
		result["remediation"] = nil
	}
	return result
}

func (probe Probe) F2(ctx context.Context) onboarding.Check {
	result := onboarding.Check{
		"id": "f2", "required": true,
		"status": "blocked", "required_version": python.RequiredF2Version,
		"message":     "F2 " + python.RequiredF2Version + " is required in the selected Python runtime.",
		"remediation": "Install F2 " + python.RequiredF2Version + " into the selected Python runtime, then rerun setup doctor.",
	}
	runtimeInfo, err := probe.PythonResolver.Resolve(ctx)
	if err != nil || !runtimeInfo.Version.AtLeast(minimumPython) {
		return result
	}
	version, err := probe.PythonResolver.F2Version(ctx, runtimeInfo)
	if version != "" {
		result["version"] = version
	}
	if err == nil && version == python.RequiredF2Version {
		result["status"] = "ready"
		result["message"] = "Pinned F2 package is available."
		result["remediation"] = nil
	}
	return result
}

func (probe Probe) Platform(ctx context.Context) onboarding.Check {
	goos := probe.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := probe.GOARCH
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	system := map[string]string{"darwin": "Darwin", "linux": "Linux", "windows": "Windows"}[goos]
	if system == "" {
		system = goos
		if system == "" {
			system = "Unknown"
		}
	}
	ready := goos == "darwin" || goos == "linux" || goos == "windows"
	release := ""
	if probe.SystemRelease != nil {
		release = probe.SystemRelease(ctx, goos)
	} else {
		release = probe.systemRelease(ctx, goos)
	}
	result := onboarding.Check{
		"id": "platform", "required": true,
		"status": "blocked", "system": system, "release": release, "machine": platformMachine(goos, goarch),
		"message":     "Only Windows, macOS, and Linux are supported.",
		"remediation": "Use Ocean Watch on Windows, macOS, or Linux.",
	}
	if ready {
		result["status"] = "ready"
		result["message"] = "Operating system is supported."
		result["remediation"] = nil
	}
	return result
}

func (probe Probe) CodexCLI(ctx context.Context) onboarding.Check {
	lookPath := probe.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	executable, err := lookPath("codex")
	if err != nil {
		return onboarding.Check{
			"id": "codex_cli", "required": false, "status": "warning", "available": false,
			"minimum_version": minimumCodex.String(),
			"message":         "Codex CLI was not found on PATH.",
			"remediation":     "The Plugin can still run inside Codex; install Codex CLI 0.144.1 or newer only for terminal use.",
		}
	}
	result, runErr := probe.run(ctx, executable, []string{"--version"})
	if runErr != nil {
		return onboarding.Check{
			"id": "codex_cli", "required": false, "status": "warning", "available": true,
			"executable": executable, "minimum_version": minimumCodex.String(),
			"message": "Codex CLI version could not be read.", "remediation": truncate(runErr.Error(), 256),
		}
	}
	output := strings.TrimSpace(result.Stdout)
	if output == "" {
		output = strings.TrimSpace(result.Stderr)
	}
	version, parsed := parseCodexVersion(output)
	ready := result.ExitCode == 0 && parsed && version.AtLeast(minimumCodex)
	check := onboarding.Check{
		"id": "codex_cli", "required": false, "available": true,
		"executable": executable, "minimum_version": minimumCodex.String(), "raw_version": truncate(output, 128),
		"status": "warning", "version": nil,
		"message":     "Codex CLI is unavailable, too old, or returned an unknown version.",
		"remediation": "Install or upgrade Codex CLI to 0.144.1 or newer.",
	}
	if parsed {
		check["version"] = version.String()
	}
	if ready {
		check["status"] = "ready"
		check["message"] = "Codex CLI is available."
		check["remediation"] = nil
	}
	return check
}

func (probe Probe) CredentialBackend(context.Context) onboarding.Check {
	backend := "unavailable"
	if probe.Credentials != nil {
		backend = probe.Credentials.BackendName()
	}
	result := onboarding.Check{"id": "credential_backend", "required": true, "backend": backend}
	switch backend {
	case "file-fallback":
		result["status"] = "warning"
		result["message"] = "Development-only plaintext credential fallback is enabled."
		result["remediation"] = "Disable plaintext fallback before using a real advertising account."
	case "unavailable", "":
		result["status"] = "blocked"
		result["message"] = "No secure credential backend is available for OAuth secrets."
		result["remediation"] = "Use macOS Keychain, Windows DPAPI, or Linux Secret Service. Plaintext fallback is development-only."
	default:
		result["status"] = "ready"
		result["message"] = "Secure credential backend is available."
		result["remediation"] = nil
	}
	return result
}

func (probe Probe) Callback(_ context.Context, redirectURI string) onboarding.Check {
	target, err := callbackTarget(redirectURI)
	if err != nil {
		return onboarding.Check{
			"id": "oauth_callback", "required": true, "status": "blocked", "redirect_uri": redirectURI,
			"message":     err.Error(),
			"remediation": "Use http://127.0.0.1:8787/oauth/callback in local config and the official console.",
		}
	}
	listen := probe.Listen
	if listen == nil {
		listen = net.Listen
	}
	listener, listenErr := listen(target.network, target.address)
	if listenErr != nil {
		return onboarding.Check{
			"id": "oauth_callback", "required": true, "status": "blocked", "redirect_uri": redirectURI,
			"host": target.host, "port": target.port, "path": target.path,
			"message":     fmt.Sprintf("OAuth callback port %d is unavailable.", target.port),
			"remediation": fmt.Sprintf("Stop the process using %s:%d, then rerun the environment check.", target.host, target.port),
			"error":       truncate(listenErr.Error(), 256),
		}
	}
	_ = listener.Close()
	return onboarding.Check{
		"id": "oauth_callback", "required": true, "status": "ready", "redirect_uri": redirectURI,
		"host": target.host, "port": target.port, "path": target.path,
		"message": "OAuth callback host and port are available.", "remediation": nil,
	}
}

type bindTarget struct {
	network string
	address string
	host    string
	port    int
	path    string
}

func callbackTarget(value string) (bindTarget, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() == "" || parsed.Port() == "" {
		return bindTarget{}, errors.New("OAuth callback must be an HTTP loopback URI with an explicit port")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port <= 0 || port > 65535 {
		return bindTarget{}, errors.New("OAuth callback must be an HTTP loopback URI with an explicit port")
	}
	host := parsed.Hostname()
	addressHost := host
	network := "tcp"
	if strings.EqualFold(host, "localhost") {
		addressHost = "127.0.0.1"
		network = "tcp4"
	} else {
		address := net.ParseIP(host)
		if address == nil || !address.IsLoopback() {
			return bindTarget{}, errors.New("OAuth callback must use a loopback host")
		}
		if address.To4() != nil {
			network = "tcp4"
		} else {
			network = "tcp6"
		}
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	return bindTarget{
		network: network, address: net.JoinHostPort(addressHost, strconv.Itoa(port)),
		host: host, port: port, path: path,
	}, nil
}

func (probe Probe) run(ctx context.Context, executable string, arguments []string) (python.CommandResult, error) {
	timeout := probe.CommandTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if probe.Run != nil {
		return probe.Run(commandCtx, executable, arguments)
	}
	command := exec.CommandContext(commandCtx, executable, arguments...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	result := python.CommandResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err == nil {
		return result, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		result.ExitCode = exitError.ExitCode()
		return result, nil
	}
	if commandCtx.Err() != nil {
		return result, commandCtx.Err()
	}
	return result, err
}

func (probe Probe) systemRelease(ctx context.Context, goos string) string {
	if goos == "darwin" || goos == "linux" {
		for _, executable := range []string{"/usr/bin/uname", "/bin/uname"} {
			if result, runErr := probe.run(ctx, executable, []string{"-r"}); runErr == nil && result.ExitCode == 0 {
				if release := strings.TrimSpace(result.Stdout); release != "" {
					return release
				}
			}
		}
	}
	return ""
}

func platformMachine(goos, goarch string) string {
	switch goarch {
	case "amd64":
		if goos == "windows" {
			return "AMD64"
		}
		return "x86_64"
	case "386":
		if goos == "windows" {
			return "x86"
		}
		return "i386"
	case "arm64":
		if goos == "linux" {
			return "aarch64"
		}
		if goos == "windows" {
			return "ARM64"
		}
		return "arm64"
	default:
		return goarch
	}
}

func parseCodexVersion(value string) (python.Version, bool) {
	match := codexVersionPattern.FindStringSubmatch(value)
	if len(match) != 4 {
		return python.Version{}, false
	}
	major, firstErr := strconv.Atoi(match[1])
	minor, secondErr := strconv.Atoi(match[2])
	patch, thirdErr := strconv.Atoi(match[3])
	if firstErr != nil || secondErr != nil || thirdErr != nil {
		return python.Version{}, false
	}
	return python.Version{Major: major, Minor: minor, Patch: patch}, true
}

func truncate(value string, maximum int) string {
	characters := []rune(value)
	if len(characters) <= maximum {
		return value
	}
	return string(characters[:maximum])
}
