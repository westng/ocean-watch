package resources

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

const defaultConfigSHA256 = "5f0b1bb349cb90fe29a63736532126dfd5494ef2adf1df991eda985f9bf8b81e"

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
