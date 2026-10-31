package credentials

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const macOSSystemFormat = "ocean-watch-keychain-chunks-v1"
const macOSSystemChunkBytes = 1024
const macOSSystemMaxParts = 128

type macOSSystemIndex struct {
	Format   string `json:"format"`
	Revision string `json:"revision"`
	Parts    int    `json:"parts"`
}

func parseMacOSSystemIndex(entry map[string]any) (macOSSystemIndex, error) {
	var index macOSSystemIndex
	encoded, err := json.Marshal(entry)
	if err != nil || json.Unmarshal(encoded, &index) != nil || index.Format != macOSSystemFormat ||
		len(index.Revision) != 32 || index.Parts < 1 || index.Parts > macOSSystemMaxParts {
		return macOSSystemIndex{}, errors.New("invalid system credential index")
	}
	if _, err := hex.DecodeString(index.Revision); err != nil {
		return macOSSystemIndex{}, errors.New("invalid system credential index")
	}
	return index, nil
}

func macOSSystemPart(account string, index macOSSystemIndex, part int) string {
	return fmt.Sprintf("%s-%s-%d", account, index.Revision, part)
}

func (store SystemStore) readSystemMacOS(ctx context.Context, account string) (map[string]any, error) {
	entry, err := store.readCommand(ctx, "security", macOSReadArguments(account), 44)
	if err != nil {
		return nil, err
	}
	if _, chunked := entry["format"]; !chunked {
		return entry, nil
	} // Legacy short entries.
	index, err := parseMacOSSystemIndex(entry)
	if err != nil {
		return nil, err
	}
	var payload []byte
	for part := 0; part < index.Parts; part++ {
		entry, err := store.readCommand(ctx, "security", macOSReadArguments(macOSSystemPart(account, index, part)), 44)
		if err != nil {
			return nil, errors.New("system credential is incomplete")
		}
		encoded, _ := entry["data"].(string)
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(data) == 0 || len(data) > macOSSystemChunkBytes {
			return nil, errors.New("system credential is incomplete")
		}
		payload = append(payload, data...)
	}
	return decode(payload)
}

func (store SystemStore) writeMacOSSystemEntry(ctx context.Context, account string, payload []byte) error {
	input := macOSWriteInput(account, payload)
	// security -i silently truncates command lines at roughly 4 KiB. Keep
	// every command small, including space for an isolated test keychain path.
	if len(input) > 3500 {
		return errors.New("system credential command is too large")
	}
	if err := store.Store.runCommand(ctx, "security", []string{"-i"}, input, true); err != nil {
		return err
	}
	entry, err := store.readCommand(ctx, "security", macOSReadArguments(account), 44)
	if err != nil {
		return errors.New("system credential read-back failed")
	}
	actual, err := encode(entry, false)
	if err != nil || !bytes.Equal(payload, actual) {
		return errors.New("system credential read-back failed")
	}
	return nil
}

func (store SystemStore) deleteMacOSSystemParts(account string, index macOSSystemIndex) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for part := 0; part < index.Parts; part++ {
		if ctx.Err() != nil {
			return
		}
		_ = store.Store.runCommand(ctx, "security", []string{"delete-generic-password", "-s", Service, "-a", macOSSystemPart(account, index, part)}, nil, true)
	}
}

func (store SystemStore) writeSystemMacOS(ctx context.Context, account string, payload []byte) error {
	if len(payload) == 0 || len(payload) > macOSSystemChunkBytes*macOSSystemMaxParts {
		return errors.New("invalid system credential size")
	}
	// A previous truncated entry must not prevent the user replacing it. The
	// old index is used only for cleanup after the new revision is committed.
	previousEntry, _ := store.readCommand(ctx, "security", macOSReadArguments(account), 44)
	previous, _ := parseMacOSSystemIndex(previousEntry)
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	index := macOSSystemIndex{Format: macOSSystemFormat, Revision: hex.EncodeToString(nonce[:]), Parts: (len(payload) + macOSSystemChunkBytes - 1) / macOSSystemChunkBytes}
	rootAttempted := false
	created := index
	created.Parts = 0
	defer func() {
		// Once the root write has started its outcome may be uncertain. Retain
		// the new parts rather than risk deleting the committed credential.
		if !rootAttempted {
			store.deleteMacOSSystemParts(account, created)
		}
	}()
	for part := 0; part < index.Parts; part++ {
		created.Parts = part + 1
		end := min((part+1)*macOSSystemChunkBytes, len(payload))
		entry := map[string]any{"data": base64.StdEncoding.EncodeToString(payload[part*macOSSystemChunkBytes : end])}
		encoded, _ := encode(entry, false)
		if err := store.writeMacOSSystemEntry(ctx, macOSSystemPart(account, index, part), encoded); err != nil {
			return err
		}
	}
	encoded, _ := encode(map[string]any{"format": index.Format, "revision": index.Revision, "parts": index.Parts}, false)
	rootAttempted = true
	if err := store.writeMacOSSystemEntry(ctx, account, encoded); err != nil {
		return err
	}
	if previous.Format == macOSSystemFormat && previous.Parts > 0 {
		store.deleteMacOSSystemParts(account, previous)
	}
	return nil
}
