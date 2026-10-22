package sqlite

import (
	"os"
	"path/filepath"
)

type pathIdentity struct {
	file      os.FileInfo
	directory os.FileInfo
}

func captureIdentity(path string) (*pathIdentity, error) {
	file, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	directory, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	return &pathIdentity{file: file, directory: directory}, nil
}

func (store *Store) ensureIdentity() error {
	if store.identity == nil {
		return nil
	}
	if _, err := preparePath(store.path, false); err != nil {
		return err
	}
	current, err := captureIdentity(store.path)
	if err != nil {
		return err
	}
	if !os.SameFile(store.identity.file, current.file) || !os.SameFile(store.identity.directory, current.directory) {
		return ErrUnsafePath
	}
	return nil
}
