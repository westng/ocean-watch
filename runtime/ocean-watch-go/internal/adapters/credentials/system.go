package credentials

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"runtime"
)

const BackendWindowsCredentialManager = "windows-credential-manager"

// SystemStore never uses the OAuth store's opt-in file or DPAPI-file backends.
// Its zero value addresses only the current OS user's system credential vault.
type SystemStore struct{ Store Store }

func (store SystemStore) BackendName() string {
	if runtime.GOOS == "windows" {
		return BackendWindowsCredentialManager
	}
	switch backend := store.Store.BackendName(); backend {
	case BackendMacOSKeychain, BackendLinuxSecretService:
		return backend
	default:
		return BackendUnavailable
	}
}

func (store SystemStore) Read(ctx context.Context, account string) (map[string]any, error) {
	if err := validateAccount(account); err != nil {
		return nil, err
	}
	switch store.BackendName() {
	case BackendWindowsCredentialManager:
		return readSystemWindows(ctx, account)
	case BackendMacOSKeychain:
		return store.readSystemMacOS(ctx, account)
	case BackendLinuxSecretService:
		return store.readCommand(ctx, "secret-tool", linuxReadArguments(account), 1)
	default:
		return nil, errors.New("system credential storage is unavailable")
	}
}

func (store SystemStore) readCommand(ctx context.Context, name string, args []string, missingCode int) (map[string]any, error) {
	path, err := store.Store.resolveCommand(name)
	if err != nil {
		return nil, errors.New("system credential storage is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, credentialCommandTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, path, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		var exit *exec.ExitError
		if ctx.Err() == nil && errors.As(err, &exit) && exit.ExitCode() == missingCode && (name == "security" || stderr.Len() == 0) {
			return map[string]any{}, nil
		}
		return nil, errors.New("system credential could not be read")
	}
	return decode(stdout.Bytes())
}

func (store SystemStore) Write(ctx context.Context, account string, value map[string]any) (string, error) {
	if err := validateAccount(account); err != nil {
		return "", err
	}
	payload, err := encode(value, false)
	if err != nil {
		return "", errors.New("invalid system credential")
	}
	backend := store.BackendName()
	switch backend {
	case BackendWindowsCredentialManager:
		err = writeSystemWindows(ctx, account, payload)
	case BackendMacOSKeychain:
		err = store.writeSystemMacOS(ctx, account, payload)
	case BackendLinuxSecretService:
		args := linuxWriteArguments(account)
		args[2] = "Ocean Watch F2 Douyin Cookie"
		err = store.Store.runCommand(ctx, "secret-tool", args, payload, true)
	default:
		err = errors.New("system credential storage is unavailable")
	}
	if err != nil {
		return "", errors.New("system credential could not be saved")
	}
	written, err := store.Read(ctx, account)
	if err != nil {
		return "", errors.New("system credential read-back failed")
	}
	actual, err := encode(written, false)
	if err != nil || !bytes.Equal(payload, actual) {
		return "", errors.New("system credential read-back failed")
	}
	return backend, nil
}
