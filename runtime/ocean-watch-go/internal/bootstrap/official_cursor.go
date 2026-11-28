package bootstrap

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
)

// Create the private key only when an official query actually needs a cursor.
func officialCursorKey(stateRoot string) func(context.Context) ([32]byte, error) {
	dir := filepath.Join(stateRoot, "official-cursors")
	path := filepath.Join(dir, "signing.key")
	return func(ctx context.Context) ([32]byte, error) {
		var key [32]byte
		if err := os.MkdirAll(dir, 0700); err != nil {
			return key, err
		}
		info, err := os.Lstat(dir)
		if err != nil {
			return key, err
		}
		if !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
			return key, errors.New("official cursor directory must be private")
		}
		key, err = readOfficialCursorKey(path)
		if !errors.Is(err, os.ErrNotExist) {
			return key, err
		}
		lock, err := filesystem.AcquireLock(ctx, filepath.Join(dir, "initialize.lock"), 0)
		if err != nil {
			return key, err
		}
		defer lock.Release()
		// A competing process may have published the key while we waited.
		key, err = readOfficialCursorKey(path)
		if !errors.Is(err, os.ErrNotExist) {
			return key, err
		}
		if _, err = rand.Read(key[:]); err != nil {
			return [32]byte{}, err
		}
		if err = filesystem.AtomicWritePrivateFile(path, key[:]); err != nil {
			return [32]byte{}, err
		}
		return key, nil
	}
}

func readOfficialCursorKey(path string) ([32]byte, error) {
	var key [32]byte
	info, err := os.Lstat(path)
	if err != nil {
		return key, err
	}
	if !info.Mode().IsRegular() || info.Size() != int64(len(key)) || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return key, errors.New("invalid official cursor key file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return key, err
	}
	if len(data) != len(key) {
		return key, errors.New("invalid official cursor key length")
	}
	copy(key[:], data)
	return key, nil
}
