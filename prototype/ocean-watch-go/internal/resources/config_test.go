package resources

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

const defaultConfigSHA256 = "827e5e2d757d91beb4bf4f650158680f1483973d2091e28f5e7f04ec6c286401"

func TestEmbeddedDefaultConfigMatchesFrozenPythonTemplate(t *testing.T) {
	payload := DefaultConfigBytes()
	digest := sha256.Sum256(payload)
	if got := hex.EncodeToString(digest[:]); got != defaultConfigSHA256 {
		t.Fatalf("embedded config hash %s, want %s", got, defaultConfigSHA256)
	}
	pythonTemplate := filepath.Join("..", "..", "..", "..", "skills", "ads-plan-monitor", "assets", "config.example.json")
	if source, err := os.ReadFile(pythonTemplate); err == nil {
		sourceDigest := sha256.Sum256(source)
		if sourceDigest != digest {
			t.Fatal("embedded config drifted from Python template")
		}
	}
	if _, err := DefaultConfig(); err != nil {
		t.Fatal(err)
	}
}
