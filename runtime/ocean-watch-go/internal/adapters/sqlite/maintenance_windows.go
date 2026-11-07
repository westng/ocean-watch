//go:build windows

package sqlite

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func tryMaintenanceLock(file *os.File, exclusive bool) (bool, error) {
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	err := windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, new(windows.Overlapped))
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
		return false, nil
	}
	return err == nil, err
}

func unlockMaintenance(file *os.File) error {
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, new(windows.Overlapped))
}

func downgradeMaintenance(file *os.File) error {
	// LockFileEx permits a shared lock over this handle's exclusive lock. The
	// first UnlockFileEx then removes the exclusive lock, retaining the shared
	// lock without a window in which a migration could acquire exclusivity.
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, new(windows.Overlapped)); err != nil {
		return err
	}
	return unlockMaintenance(file)
}
