//go:build windows

package credentials

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"unsafe"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/filesystem"
	"golang.org/x/sys/windows"
)

func (store Store) readWindows(account string) (map[string]any, error) {
	encoded, err := os.ReadFile(store.filePath(account, ".dpapi"))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read DPAPI credential: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(string(bytesTrimSpace(encoded)))
	if err != nil {
		return nil, fmt.Errorf("decode DPAPI credential: %w", err)
	}
	plaintext, err := cryptUnprotect(ciphertext)
	if err != nil {
		return nil, err
	}
	return decode(plaintext)
}

func (store Store) writeWindows(account string, payload []byte) error {
	ciphertext, err := cryptProtect(payload)
	if err != nil {
		return err
	}
	encoded := append([]byte(base64.StdEncoding.EncodeToString(ciphertext)), '\n')
	return filesystem.AtomicWritePrivateFile(store.filePath(account, ".dpapi"), encoded)
}

func cryptProtect(payload []byte) ([]byte, error) {
	input := dataBlob(payload)
	var output windows.DataBlob
	if err := windows.CryptProtectData(&input, nil, nil, 0, nil, 0, &output); err != nil {
		return nil, fmt.Errorf("CryptProtectData failed: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(output.Data)))
	return append([]byte(nil), unsafe.Slice(output.Data, output.Size)...), nil
}

func cryptUnprotect(payload []byte) ([]byte, error) {
	input := dataBlob(payload)
	var output windows.DataBlob
	if err := windows.CryptUnprotectData(&input, nil, nil, 0, nil, 0, &output); err != nil {
		return nil, fmt.Errorf("CryptUnprotectData failed: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(output.Data)))
	return append([]byte(nil), unsafe.Slice(output.Data, output.Size)...), nil
}

func dataBlob(payload []byte) windows.DataBlob {
	if len(payload) == 0 {
		return windows.DataBlob{}
	}
	return windows.DataBlob{Size: uint32(len(payload)), Data: &payload[0]}
}

func bytesTrimSpace(value []byte) []byte {
	start, end := 0, len(value)
	for start < end && (value[start] == ' ' || value[start] == '\r' || value[start] == '\n' || value[start] == '\t') {
		start++
	}
	for end > start && (value[end-1] == ' ' || value[end-1] == '\r' || value[end-1] == '\n' || value[end-1] == '\t') {
		end--
	}
	return value[start:end]
}
