package sqlite

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"
)

func acquireMaintenance(ctx context.Context, path string, exclusive bool, timeout time.Duration) (func() error, func() error, error) {
	return acquireFileLock(ctx, path+".maintenance.lock", exclusive, timeout)
}

func acquireInitialization(ctx context.Context, path string, timeout time.Duration) (func() error, error) {
	release, _, err := acquireFileLock(ctx, path+".initialize.lock", true, timeout)
	return release, err
}

func acquireFileLock(ctx context.Context, path string, exclusive bool, timeout time.Duration) (func() error, func() error, error) {
	if err := createStateFile(path); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, nil, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || !private(path, info) {
		return nil, nil, ErrUnsafePath
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		_ = file.Close()
		return nil, nil, ErrUnsafePath
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		locked, err := tryMaintenanceLock(file, exclusive)
		if err != nil {
			_ = file.Close()
			return nil, nil, err
		}
		if locked {
			var once sync.Once
			var releaseErr error
			release := func() error {
				once.Do(func() { releaseErr = errors.Join(unlockMaintenance(file), file.Close()) })
				return releaseErr
			}
			downgrade := func() error {
				return downgradeMaintenance(file)
			}
			return release, downgrade, nil
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = file.Close()
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
	}
}
