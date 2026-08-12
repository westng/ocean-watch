package workmetadata

import (
	"bytes"
	"testing"
)

func TestLimitedBufferReportsOverflow(t *testing.T) {
	var buffer bytes.Buffer
	writer := &limitedBuffer{buffer: &buffer, limit: 4}
	if written, err := writer.Write([]byte("abcdef")); err != nil || written != 6 {
		t.Fatalf("written=%d err=%v", written, err)
	}
	if !writer.overflow || buffer.String() != "abcd" {
		t.Fatalf("overflow=%v buffer=%q", writer.overflow, buffer.String())
	}
}
