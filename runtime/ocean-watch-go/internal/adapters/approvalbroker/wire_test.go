package approvalbroker

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestWireRejectsOversizedTruncatedAndUnexpectedInput(t *testing.T) {
	for _, encoded := range [][]byte{
		{0, 0, 0, 0},
		{0, 1, 0, 1},
		{0, 0, 0, 2, '{'},
		frame(`{"protocol":"broker-v1","challenge":"x","auto_approve":true}`),
		frame(`{"protocol":"broker-v1"} {"challenge":"x"}`),
	} {
		var message helloMessage
		if err := readMessage(bytes.NewReader(encoded), &message); err == nil {
			t.Fatalf("invalid frame accepted: %q", encoded)
		}
	}
}

func frame(payload string) []byte {
	encoded := make([]byte, 4)
	binary.BigEndian.PutUint32(encoded, uint32(len(payload)))
	return append(encoded, payload...)
}
