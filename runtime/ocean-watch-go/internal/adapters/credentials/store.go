package credentials

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
)

const (
	Service                   = "ads-plan-monitor"
	InsecureFileFallbackEnv   = "ADS_PLAN_MONITOR_ALLOW_INSECURE_FILE_FALLBACK"
	BackendMacOSKeychain      = "macos-keychain"
	BackendWindowsDPAPI       = "windows-dpapi"
	BackendLinuxSecretService = "linux-secret-service"
	BackendFileFallback       = "file-fallback"
	BackendUnavailable        = "unavailable"
	credentialCommandTimeout  = 30 * time.Second
)

type Store struct {
	Root     string
	Getenv   func(string) string
	LookPath func(string) (string, error)
}

func (store Store) BackendName() string {
	getenv := store.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	lookPath := store.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	return backendName(runtime.GOOS, getenv, lookPath)
}

func backendName(goos string, getenv func(string) string, lookPath func(string) (string, error)) string {
	switch goos {
	case "darwin":
		if _, err := lookPath("security"); err == nil {
			return BackendMacOSKeychain
		}
	case "windows":
		return BackendWindowsDPAPI
	default:
		if _, err := lookPath("secret-tool"); err == nil {
			return BackendLinuxSecretService
		}
	}
	if getenv(InsecureFileFallbackEnv) == "1" {
		return BackendFileFallback
	}
	return BackendUnavailable
}

func (store Store) Read(ctx context.Context, account string) (map[string]any, error) {
	if err := validateAccount(account); err != nil {
		return nil, err
	}
	switch store.BackendName() {
	case BackendMacOSKeychain:
		return store.readCommand(ctx, "security", macOSReadArguments(account), false)
	case BackendLinuxSecretService:
		return store.readCommand(ctx, "secret-tool", linuxReadArguments(account), false)
	case BackendWindowsDPAPI:
		return store.readWindows(account)
	case BackendFileFallback:
		return store.readFile(account)
	default:
		return map[string]any{}, nil
	}
}

func (store Store) Write(ctx context.Context, account string, value map[string]any) (string, error) {
	if err := validateAccount(account); err != nil {
		return "", err
	}
	if value == nil {
		value = map[string]any{}
	}
	payload, err := encode(value, false)
	if err != nil {
		return "", err
	}
	backend := store.BackendName()
	switch backend {
	case BackendMacOSKeychain:
		// Prompted passwords are read from the terminal, not reliably from stdin.
		// Interactive command mode accepts the complete command on stdin; -X keeps
		// JSON quoting out of its parser and the credential out of process arguments.
		// -U updates in place, so a failed write does not first delete the old entry.
		err = store.runCommand(ctx, "security", []string{"-i"}, macOSWriteInput(account, payload), true)
	case BackendLinuxSecretService:
		err = store.runCommand(ctx, "secret-tool", linuxWriteArguments(account), payload, true)
	case BackendWindowsDPAPI:
		err = store.writeWindows(account, payload)
	case BackendFileFallback:
		err = store.writeFile(account, value)
	default:
		err = errors.New("no secure credential backend is available; install a supported backend or explicitly enable the development-only file fallback")
	}
	if err != nil {
		return "", &writeError{cause: err, reason: "CREDENTIAL_WRITE_FAILED"}
	}
	written, err := store.Read(ctx, account)
	if err != nil {
		return "", &writeError{cause: err, reason: "CREDENTIAL_VERIFY_FAILED"}
	}
	want, err := encode(value, false)
	if err != nil {
		return "", err
	}
	got, err := encode(written, false)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(want, got) {
		return "", &writeError{cause: errors.New("credential read-back verification failed"), reason: "CREDENTIAL_VERIFY_FAILED"}
	}
	return backend, nil
}

func macOSReadArguments(account string) []string {
	return []string{"find-generic-password", "-s", Service, "-a", account, "-w"}
}

func macOSWriteInput(account string, payload []byte) []byte {
	return []byte("add-generic-password -U -s " + Service + " -a " + account + " -X " + hex.EncodeToString(payload) + "\n")
}

func linuxReadArguments(account string) []string {
	return []string{"lookup", "service", Service, "account", account}
}

func linuxWriteArguments(account string) []string {
	return []string{
		"store", "--label", "Ads Plan Monitor OceanEngine OAuth",
		"service", Service, "account", account,
	}
}

func (store Store) readCommand(ctx context.Context, name string, args []string, fail bool) (map[string]any, error) {
	path, err := store.resolveCommand(name)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, credentialCommandTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, path, args...)
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = ioDiscard{}
	err = command.Run()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !fail {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("read credential entry: %w", err)
	}
	return decode(stdout.Bytes())
}

func (store Store) runCommand(ctx context.Context, name string, args []string, stdin []byte, fail bool) error {
	path, err := store.resolveCommand(name)
	if err != nil {
		if fail {
			return err
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, credentialCommandTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, path, args...)
	if stdin != nil {
		command.Stdin = bytes.NewReader(stdin)
	}
	command.Stdout = ioDiscard{}
	command.Stderr = ioDiscard{}
	err = command.Run()
	if err != nil && fail {
		return fmt.Errorf("write credential entry: %w", err)
	}
	return nil
}

func (store Store) resolveCommand(name string) (string, error) {
	lookPath := store.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	path, err := lookPath(name)
	if err != nil {
		return "", fmt.Errorf("required credential command is unavailable: %s", name)
	}
	return path, nil
}

func (store Store) readFile(account string) (map[string]any, error) {
	payload, err := os.ReadFile(store.filePath(account, ".json"))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read credential file: %w", err)
	}
	return decode(payload)
}

func (store Store) writeFile(account string, value map[string]any) error {
	payload, err := encode(value, true)
	if err != nil {
		return err
	}
	return filesystem.AtomicWritePrivateFile(store.filePath(account, ".json"), payload)
}

func (store Store) filePath(account, extension string) string {
	suffix := strings.ReplaceAll(account, "/", "-")
	return filepath.Join(store.Root, suffix+extension)
}

func decode(payload []byte) (map[string]any, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return map[string]any{}, nil
	}
	result, err := decodeJSON(trimmed)
	if err == nil {
		return result, nil
	}
	if len(trimmed)%2 == 0 {
		decoded := make([]byte, hex.DecodedLen(len(trimmed)))
		if _, decodeErr := hex.Decode(decoded, trimmed); decodeErr == nil {
			if result, jsonErr := decodeJSON(decoded); jsonErr == nil {
				return result, nil
			}
		}
	}
	return nil, errors.New("stored credentials are not valid JSON or hex-encoded JSON")
}

func decodeJSON(payload []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var result map[string]any
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("credential entry must be a JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return nil, errors.New("credential entry contains multiple JSON values")
	}
	return result, nil
}

func encode(value map[string]any, indented bool) ([]byte, error) {
	buffer := new(bytes.Buffer)
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	if indented {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("encode credential entry: %w", err)
	}
	if !indented {
		return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
	}
	return buffer.Bytes(), nil
}

func validateAccount(account string) error {
	if account == "" {
		return errors.New("credential account name is invalid")
	}
	for _, value := range account {
		if value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' ||
			value >= '0' && value <= '9' || value == '.' || value == '_' || value == '-' {
			continue
		}
		return errors.New("credential account name is invalid")
	}
	return nil
}

type ioDiscard struct{}

func (ioDiscard) Write(payload []byte) (int, error) { return len(payload), nil }
