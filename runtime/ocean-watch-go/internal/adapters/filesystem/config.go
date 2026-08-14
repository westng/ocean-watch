package filesystem

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application"
)

type ConfigStore struct {
	Path        string
	LockTimeout time.Duration
}

type ConfigUpdater = application.ConfigUpdater

func (store ConfigStore) Initialize(ctx context.Context, value map[string]any, overwrite bool) (bool, error) {
	lock, err := AcquireLock(ctx, lockPath(store.Path), store.LockTimeout)
	if err != nil {
		return false, err
	}
	defer func() { _ = lock.Release() }()
	existed := false
	if info, statErr := os.Stat(store.Path); statErr == nil {
		if !info.Mode().IsRegular() {
			return false, errors.New("config path is not a regular file")
		}
		existed = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return false, fmt.Errorf("stat config: %w", statErr)
	}
	if existed && !overwrite {
		return false, nil
	}
	if err := atomicWriteJSON(store.Path, value); err != nil {
		return false, err
	}
	return true, nil
}

func (store ConfigStore) Read(ctx context.Context) (map[string]any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return readJSON(store.Path)
}

func (store ConfigStore) ReadWithRevision(ctx context.Context) (map[string]any, string, error) {
	config, err := store.Read(ctx)
	if err != nil {
		return nil, "", err
	}
	revision, err := JSONRevision(config)
	if err != nil {
		return nil, "", err
	}
	return config, revision, nil
}

func (store ConfigStore) Update(ctx context.Context, update ConfigUpdater) (any, error) {
	lock, err := AcquireLock(ctx, lockPath(store.Path), store.LockTimeout)
	if err != nil {
		return nil, err
	}
	defer func() { _ = lock.Release() }()
	raw, err := readJSON(store.Path)
	if err != nil {
		return nil, err
	}
	result, changed, err := update(raw)
	if err != nil {
		return nil, err
	}
	if !changed {
		return result, nil
	}
	if err := atomicWriteJSON(store.Path, raw); err != nil {
		return nil, err
	}
	return result, nil
}

func (store ConfigStore) CompareAndSwap(
	ctx context.Context,
	expectedRevision string,
	updated map[string]any,
) error {
	lock, err := AcquireLock(ctx, lockPath(store.Path), store.LockTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	current, err := readJSON(store.Path)
	if err != nil {
		return err
	}
	revision, err := JSONRevision(current)
	if err != nil {
		return err
	}
	if revision != expectedRevision {
		return errors.New("configuration changed while this operation was running; reload and retry")
	}
	return atomicWriteJSON(store.Path, updated)
}

func (store ConfigStore) CommitMigration(
	ctx context.Context,
	expectedRevision string,
	updated map[string]any,
) error {
	lock, err := AcquireLock(ctx, lockPath(store.Path), store.LockTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	current, err := readJSON(store.Path)
	if err != nil {
		return err
	}
	revision, err := JSONRevision(current)
	if err != nil {
		return err
	}
	if revision != expectedRevision {
		return errors.New("configuration changed while this operation was running; reload and retry")
	}
	return atomicWriteJSONWithoutBackup(store.Path, updated)
}

func JSONRevision(value map[string]any) (string, error) {
	buffer := new(bytes.Buffer)
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", fmt.Errorf("encode configuration revision: %w", err)
	}
	payload := bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func readJSON(path string) (map[string]any, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	defer file.Close()
	return decodeJSON(file)
}

func decodeJSON(reader io.Reader) (map[string]any, error) {
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if value == nil {
		return nil, errors.New("config must be a JSON object")
	}
	return value, nil
}

func atomicWriteJSON(path string, value map[string]any) error {
	payload, err := encodeJSON(value)
	if err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	_ = os.Chmod(parent, 0o700)
	if existing, readErr := os.ReadFile(path); readErr == nil {
		if err := atomicWriteBytes(path+".bak", existing); err != nil {
			return fmt.Errorf("write config backup: %w", err)
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("read config for backup: %w", readErr)
	}
	return atomicWriteBytes(path, payload)
}

func atomicWriteJSONWithoutBackup(path string, value map[string]any) error {
	payload, err := encodeJSON(value)
	if err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	_ = os.Chmod(parent, 0o700)
	return atomicWriteBytes(path, payload)
}

func encodeJSON(value map[string]any) ([]byte, error) {
	buffer := new(bytes.Buffer)
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	return buffer.Bytes(), nil
}

func atomicWriteBytes(path string, payload []byte) error {
	return atomicWriteBytesWithReplace(path, payload, replaceFile)
}

func AtomicWritePrivateFile(path string, payload []byte) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create private file directory: %w", err)
	}
	_ = os.Chmod(parent, 0o700)
	return atomicWriteBytes(path, payload)
}

func AtomicWritePrivateJSON(path string, value map[string]any) error {
	buffer := new(bytes.Buffer)
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("encode private JSON: %w", err)
	}
	return AtomicWritePrivateFile(path, buffer.Bytes())
}

func atomicWriteBytesWithReplace(
	path string,
	payload []byte,
	replace func(string, string) error,
) error {
	parent := filepath.Dir(path)
	temporary, err := os.CreateTemp(parent, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create config temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	keep := false
	defer func() {
		_ = temporary.Close()
		if !keep {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := replace(temporaryName, path); err != nil {
		return fmt.Errorf("replace config atomically: %w", err)
	}
	keep = true
	_ = os.Chmod(path, 0o600)
	written, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("verify atomic file write: %w", err)
	}
	if !bytes.Equal(written, payload) {
		return fmt.Errorf("atomic file write verification failed: %s", path)
	}
	if directory, err := os.Open(parent); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}
