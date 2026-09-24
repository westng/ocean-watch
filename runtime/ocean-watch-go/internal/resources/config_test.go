package resources

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

const defaultConfigSHA256 = "f56cd5635e9432b33032eae566e72f40c3f2edbcdf4055834c1db347bf0a9647"

func TestEmbeddedDefaultConfigMatchesBundledTemplate(t *testing.T) {
	payload := DefaultConfigBytes()
	digest := sha256.Sum256(payload)
	if got := hex.EncodeToString(digest[:]); got != defaultConfigSHA256 {
		t.Fatalf("embedded config hash %s, want %s", got, defaultConfigSHA256)
	}
	assetTemplate := filepath.Join("..", "..", "..", "..", "skills", "ads-plan-monitor", "assets", "config.example.json")
	if source, err := os.ReadFile(assetTemplate); err == nil {
		sourceDigest := sha256.Sum256(source)
		if sourceDigest != digest {
			t.Fatal("embedded config drifted from bundled template")
		}
	}
	if _, err := DefaultConfig(); err != nil {
		t.Fatal(err)
	}
}
