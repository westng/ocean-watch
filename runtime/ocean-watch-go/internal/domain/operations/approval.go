package operations

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
)

func NonceDigest(nonce string) (string, error) {
	if nonce == "" || len(nonce) > 256 {
		return "", invalidApproval("nonce is required and must be at most 256 bytes")
	}
	if strings.TrimSpace(nonce) == "" {
		return "", invalidApproval("nonce must not be blank")
	}
	sum := sha256.Sum256([]byte(nonce))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func EvidenceDigest(record ApprovalRecord) (string, error) {
	if record.EvidenceHash == "" {
		record.EvidenceHash = "sha256:" + strings.Repeat("0", 64)
	}
	if err := record.Validate(); err != nil {
		return "", err
	}
	var encoded []byte
	encoded = appendField(encoded, record.ID)
	encoded = appendField(encoded, record.OperationID)
	encoded = appendUint64(encoded, uint64(record.Version))
	encoded = appendField(encoded, record.SnapshotDigest)
	encoded = appendField(encoded, record.ScopeDigest)
	encoded = appendField(encoded, record.ProviderID)
	encoded = appendField(encoded, record.AdapterID)
	encoded = appendField(encoded, record.HostVersion)
	encoded = appendField(encoded, record.NonceHash)
	encoded = appendField(encoded, record.SourceReference)
	encoded = appendField(encoded, record.IssuedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"))
	encoded = appendField(encoded, record.ExpiresAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"))
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func VerifyEvidenceDigest(record ApprovalRecord) error {
	if record.EvidenceHash == "" {
		return errors.New("evidence hash is required")
	}
	computed, err := EvidenceDigest(record)
	if err != nil {
		return err
	}
	if computed != record.EvidenceHash {
		return invalidApproval("evidence_hash does not match bound evidence")
	}
	return nil
}

func appendField(dst []byte, value string) []byte {
	dst = appendUint64(dst, uint64(len(value)))
	return append(dst, value...)
}

func appendUint64(dst []byte, value uint64) []byte {
	var buffer [8]byte
	binary.BigEndian.PutUint64(buffer[:], value)
	return append(dst, buffer[:]...)
}
