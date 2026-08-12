package resources

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

const defaultConfigSHA256 = "381a24024e06acc99f691a3be93bbc1cfc2eb3e65d80d06d431841933c73096d"

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
