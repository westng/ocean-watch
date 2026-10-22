package sqlite

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func preparePath(path string, create bool) (string, error) {
	if !filepath.IsAbs(path) || strings.TrimSpace(path) == "" || strings.HasPrefix(filepath.ToSlash(path), "//") {
		return "", ErrUnsafePath
	}
	path = filepath.Clean(path)
	directory := filepath.Dir(path)
	if err := checkDirectoryChain(directory); err != nil {
		return "", err
	}
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) && create {
		if err = createStateDirectory(directory); err != nil {
			return "", fmt.Errorf("create state directory: %w", err)
		}
		info, err = os.Lstat(directory)
	}
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNotInitialized
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !privateDirectory(directory, info) {
		return "", ErrUnsafePath
	}
	canonical, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", err
	}
	path = filepath.Join(canonical, filepath.Base(path))
	info, err = os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && create {
		if createErr := createStateFile(path); createErr != nil && !errors.Is(createErr, os.ErrExist) {
			return "", createErr
		}
		info, err = os.Lstat(path)
	}
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNotInitialized
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || !private(path, info) {
		return "", ErrUnsafePath
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		info, err := os.Lstat(path + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() || !private(path+suffix, info) {
			return "", ErrUnsafePath
		}
	}
	return path, nil
}

func checkDirectoryChain(directory string) error {
	for path := directory; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				canonical, resolveErr := filepath.EvalSymlinks(path)
				if runtime.GOOS != "darwin" || (path != "/var" && path != "/tmp") || resolveErr != nil || canonical != "/private"+path {
					return ErrUnsafePath
				}
			} else if !info.IsDir() {
				return ErrUnsafePath
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if parent := filepath.Dir(path); parent == path {
			return nil
		}
	}
}

func private(path string, info os.FileInfo) bool {
	return owned(path, info) && (runtime.GOOS == "windows" || info.Mode().Perm()&0o077 == 0)
}
