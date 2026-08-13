package filesystem

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type FileLock struct {
	file *os.File
	path string
}

func AcquireLock(ctx context.Context, path string, timeout time.Duration) (*FileLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open process lock: %w", err)
	}
	return acquireOpenedLock(ctx, file, path, timeout)
}

func acquireLockAt(
	ctx context.Context,
	root *os.Root,
	name string,
	displayPath string,
	timeout time.Duration,
) (*FileLock, error) {
	file, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open process lock: %w", err)
	}
	return acquireOpenedLock(ctx, file, displayPath, timeout)
}

func acquireOpenedLock(
	ctx context.Context,
	file *os.File,
	path string,
	timeout time.Duration,
) (*FileLock, error) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		locked, lockErr := tryPlatformLock(file)
		if lockErr != nil {
			_ = file.Close()
			return nil, fmt.Errorf("acquire process lock: %w", lockErr)
		}
		if locked {
			if err := writeLockMetadata(file); err != nil {
				_ = unlockPlatformFile(file)
				_ = file.Close()
				return nil, err
			}
			return &FileLock{file: file, path: path}, nil
		}
		if time.Now().After(deadline) {
			_ = file.Close()
			return nil, fmt.Errorf("timed out waiting for process lock: %s", path)
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func writeLockMetadata(file *os.File) error {
	nonceBytes := make([]byte, 8)
	if _, err := rand.Read(nonceBytes); err != nil {
		return fmt.Errorf("generate process lock nonce: %w", err)
	}
	payload, err := json.Marshal(map[string]any{
		"pid": os.Getpid(), "nonce": hex.EncodeToString(nonceBytes),
	})
	if err != nil {
		return err
	}
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("truncate process lock metadata: %w", err)
	}
	if _, err := file.WriteAt(payload, 0); err != nil {
		return fmt.Errorf("write process lock metadata: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync process lock metadata: %w", err)
	}
	return nil
}

func (lock *FileLock) Release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	unlockErr := unlockPlatformFile(lock.file)
	closeErr := lock.file.Close()
	lock.file = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

func lockPath(path string) string { return path + ".lock" }
