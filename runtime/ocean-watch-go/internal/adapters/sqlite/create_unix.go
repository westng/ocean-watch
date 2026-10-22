//go:build !windows

package sqlite

import "os"

func createStateDirectory(path string) error {
	return os.MkdirAll(path, 0o700)
}

func createStateFile(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	return file.Close()
}
