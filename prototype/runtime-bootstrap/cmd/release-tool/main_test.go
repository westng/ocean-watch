package main

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseToolSignsAndVerifiesWithoutArgumentSecret(t *testing.T) {
	t.Setenv(signingKeyEnvironment, strings.Repeat("42", 32))
	directory := t.TempDir()
	input := filepath.Join(directory, "input.json")
	signature := filepath.Join(directory, "input.sig")
	if err := os.WriteFile(input, []byte(`{"value":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"sign", "--input", input, "--output", signature}); err != nil {
		t.Fatal(err)
	}
	privateKey, err := signingKey()
	if err != nil {
		t.Fatal(err)
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	if err := run([]string{
		"verify", "--input", input, "--signature", signature,
		"--public-key-hex", fmtHex(publicKey),
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input, []byte(`{"value":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{
		"verify", "--input", input, "--signature", signature,
		"--public-key-hex", fmtHex(publicKey),
	}); err == nil {
		t.Fatal("expected altered payload rejection")
	}
}

func TestReleaseToolRequiresEnvironmentKey(t *testing.T) {
	t.Setenv(signingKeyEnvironment, "")
	if err := run([]string{"public-key"}); err == nil {
		t.Fatal("expected missing signing key failure")
	}
}

func fmtHex(value []byte) string {
	const digits = "0123456789abcdef"
	result := make([]byte, len(value)*2)
	for index, item := range value {
		result[index*2] = digits[item>>4]
		result[index*2+1] = digits[item&15]
	}
	return string(result)
}
