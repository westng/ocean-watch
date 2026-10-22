package operations

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

func DispatchKey(request DispatchIntentRequest) (string, error) {
	if err := request.Validate(); err != nil {
		return "", err
	}
	encoded := make([]byte, 0, 256)
	encoded = appendDispatchField(encoded, "dispatch-key-v1")
	encoded = appendDispatchUint64(encoded, uint64(request.Version))
	for _, value := range []string{
		request.OperationID,
		request.ItemID,
		request.TargetID,
		request.Action,
		request.PayloadDigest,
	} {
		encoded = appendDispatchField(encoded, value)
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func appendDispatchField(dst []byte, value string) []byte {
	dst = appendDispatchUint64(dst, uint64(len(value)))
	return append(dst, value...)
}

func appendDispatchUint64(dst []byte, value uint64) []byte {
	var buffer [8]byte
	binary.BigEndian.PutUint64(buffer[:], value)
	return append(dst, buffer[:]...)
}
