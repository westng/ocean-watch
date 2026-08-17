package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
)

func decodeSingleJSONObject(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(payload))
	var result map[string]any
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("stdout contains more than one JSON value: %v", err)
	}
	return result
}
