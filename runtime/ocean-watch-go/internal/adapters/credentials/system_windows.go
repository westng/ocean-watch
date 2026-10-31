//go:build windows

package credentials

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	credentialDLL = windows.NewLazySystemDLL("advapi32.dll")
	credRead      = credentialDLL.NewProc("CredReadW")
	credWrite     = credentialDLL.NewProc("CredWriteW")
	credDelete    = credentialDLL.NewProc("CredDeleteW")
	credFree      = credentialDLL.NewProc("CredFree")
)

type windowsCredential struct {
	Flags, Type             uint32
	TargetName, Comment     *uint16
	LastWritten             windows.Filetime
	BlobSize                uint32
	Blob                    *byte
	Persist, AttributeCount uint32
	Attributes              uintptr
	TargetAlias, UserName   *uint16
}

type vaultIndex struct {
	Revision string `json:"revision"`
	Parts    int    `json:"parts"`
}

func readVaultBlob(account string) ([]byte, error) {
	name, _ := windows.UTF16PtrFromString(Service + ":" + account)
	var credential *windowsCredential
	ok, _, err := credRead.Call(uintptr(unsafe.Pointer(name)), 1, 0, uintptr(unsafe.Pointer(&credential)))
	if ok == 0 {
		if errors.Is(err, windows.ERROR_NOT_FOUND) {
			return nil, nil
		}
		return nil, errors.New("system credential could not be read")
	}
	defer credFree.Call(uintptr(unsafe.Pointer(credential)))
	if credential.BlobSize > 2560 {
		return nil, errors.New("invalid system credential size")
	}
	return append([]byte(nil), unsafe.Slice(credential.Blob, credential.BlobSize)...), nil
}

func writeVaultBlob(account string, payload []byte) error {
	if len(payload) == 0 || len(payload) > 2560 {
		return errors.New("invalid system credential size")
	}
	name, _ := windows.UTF16PtrFromString(Service + ":" + account)
	entry := windowsCredential{Type: 1, TargetName: name, BlobSize: uint32(len(payload)), Blob: &payload[0], Persist: 2}
	ok, _, _ := credWrite.Call(uintptr(unsafe.Pointer(&entry)), 0)
	runtime.KeepAlive(payload)
	if ok == 0 {
		return errors.New("system credential could not be saved")
	}
	return nil
}

func readVaultIndex(account string) (vaultIndex, error) {
	payload, err := readVaultBlob(account)
	var index vaultIndex
	if err != nil || len(payload) == 0 {
		return index, err
	}
	if json.Unmarshal(payload, &index) != nil || len(index.Revision) != 32 || index.Parts < 1 || index.Parts > 64 {
		return vaultIndex{}, errors.New("invalid system credential index")
	}
	if _, err := hex.DecodeString(index.Revision); err != nil {
		return vaultIndex{}, errors.New("invalid system credential index")
	}
	return index, nil
}

func vaultPart(account string, index vaultIndex, part int) string {
	return fmt.Sprintf("%s-%s-%d", account, index.Revision, part)
}

func readSystemWindows(ctx context.Context, account string) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	index, err := readVaultIndex(account)
	if err != nil {
		return nil, err
	}
	if index.Parts == 0 {
		return map[string]any{}, nil
	}
	var payload []byte
	for i := 0; i < index.Parts; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		part, err := readVaultBlob(vaultPart(account, index, i))
		if err != nil || len(part) == 0 {
			return nil, errors.New("system credential is incomplete")
		}
		payload = append(payload, part...)
	}
	return decode(payload)
}

func deleteVaultParts(account string, index vaultIndex) {
	for i := 0; i < index.Parts; i++ {
		name, _ := windows.UTF16PtrFromString(Service + ":" + vaultPart(account, index, i))
		credDelete.Call(uintptr(unsafe.Pointer(name)), 1, 0)
	}
}

func writeSystemWindows(ctx context.Context, account string, payload []byte) error {
	if len(payload) == 0 || len(payload) > 128<<10 {
		return errors.New("invalid system credential size")
	}
	previous, err := readVaultIndex(account)
	if err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	index := vaultIndex{Revision: hex.EncodeToString(nonce[:]), Parts: (len(payload) + 2047) / 2048}
	committed := false
	defer func() {
		if !committed {
			deleteVaultParts(account, index)
		}
	}()
	// Generic credentials have a 2560-byte blob limit. Commit the index only
	// after all new chunks are saved, preserving the previous Cookie on failure.
	for i := 0; i < index.Parts; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min((i+1)*2048, len(payload))
		if err := writeVaultBlob(vaultPart(account, index, i), payload[i*2048:end]); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	encoded, _ := json.Marshal(index)
	if err := writeVaultBlob(account, encoded); err != nil {
		return err
	}
	committed = true
	deleteVaultParts(account, previous)
	return nil
}
