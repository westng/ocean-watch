//go:build !windows

package sqlite

import (
	"errors"
	"os"
	"syscall"
)

func tryMaintenanceLock(file *os.File, exclusive bool) (bool, error) {
	mode := syscall.LOCK_SH
	if exclusive {
		mode = syscall.LOCK_EX
	}
	err := syscall.Flock(int(file.Fd()), mode|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}

func unlockMaintenance(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}

func downgradeMaintenance(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_SH)
}
