package bootstrap

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func testIdentity() Identity {
	return Identity{
		ProductVersion: "0.0.0",
		PluginVersion:  "0.0.0+codex.test",
		GitCommit:      testCommit,
		SDKVersion:     DefaultSDKVersion,
	}
}

func testKey() (ed25519.PublicKey, ed25519.PrivateKey) {
	seed := []byte{
		0x9d, 0x61, 0xb1, 0x9d, 0xef, 0xfd, 0x5a, 0x60,
		0xba, 0x84, 0x4a, 0xf4, 0x92, 0xec, 0x2c, 0xc4,
		0x44, 0x49, 0xc5, 0x69, 0x7b, 0x32, 0x69, 0x19,
		0x70, 0x3b, 0xac, 0x03, 0x1c, 0xae, 0x7f, 0x60,
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	return privateKey.Public().(ed25519.PublicKey), privateKey
}

func testManifest(t *testing.T, identity Identity, assetData []byte) ([]byte, []byte, Manifest) {
	t.Helper()
	digest := sha256.Sum256(assetData)
	manifest := Manifest{
		ManifestVersion: ManifestVersion,
		ProductVersion:  identity.ProductVersion,
		PluginVersion:   identity.PluginVersion,
		GitCommit:       identity.GitCommit,
		SDKVersion:      identity.SDKVersion,
		Tag:             "v" + identity.ProductVersion,
		Routes:          map[string]string{"accounts list": "go", "python-fallback": "python"},
		Assets: map[string]Asset{
			"darwin-amd64": {Name: "ocean-watch_darwin_amd64", SHA256: hex.EncodeToString(digest[:]), Size: int64(len(assetData))},
		},
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := canonicalJSON(encoded)
	if err != nil {
		t.Fatal(err)
	}
	_, privateKey := testKey()
	signature := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, raw)))
	return raw, signature, manifest
}

func testConfig(t *testing.T, serverURL string, cacheRoot string, raw, signature []byte, publicKey ed25519.PublicKey) Config {
	t.Helper()
	return Config{
		Identity:              testIdentity(),
		Route:                 "accounts list",
		GOOS:                  "darwin",
		GOARCH:                "amd64",
		CacheRoot:             cacheRoot,
		ManifestURL:           serverURL + "/runtime-manifest.json",
		SignatureURL:          serverURL + "/runtime-manifest.sig",
		ReleaseBaseURL:        serverURL,
		TrustedPublicKey:      publicKey,
		AllowInsecureForTests: true,
	}
}

func TestRFC8032Ed25519Vector(t *testing.T) {
	publicKey, privateKey := testKey()
	if hex.EncodeToString(publicKey) != "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a" {
		t.Fatalf("unexpected public key: %x", publicKey)
	}
	signature := ed25519.Sign(privateKey, nil)
	want := "e5564300c360ac729086e2cc806e828a84877f1eb8e5d974d873e06522490155" +
		"5fb8821590a33bacc61e39701cf9b46bd25bf5f0595bbe24655141438e7a100b"
	if hex.EncodeToString(signature) != want {
		t.Fatalf("unexpected RFC8032 signature: %x", signature)
	}
}

func TestEnsureValidThenOfflineCacheReuse(t *testing.T) {
	identity := testIdentity()
	assetData := []byte("verified runtime")
	raw, signature, _ := testManifest(t, identity, assetData)
	publicKey, _ := testKey()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/runtime-manifest.json":
			response.Write(raw)
		case "/runtime-manifest.sig":
			response.Write(signature)
		case "/v0.0.0/ocean-watch_darwin_amd64":
			response.Write(assetData)
		default:
			http.NotFound(response, request)
		}
	}))
	cacheRoot := t.TempDir()
	config := testConfig(t, server.URL, cacheRoot, raw, signature, publicKey)
	first, err := Ensure(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if first.CacheHit || first.AssetPath == "" {
		t.Fatalf("expected fresh download, got %+v", first)
	}
	server.Close()
	second, err := Ensure(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if !second.CacheHit || second.AssetPath != first.AssetPath {
		t.Fatalf("expected offline cache hit, got %+v", second)
	}
}

func TestEnsureRejectsAlteredManifestAndDamagedCache(t *testing.T) {
	identity := testIdentity()
	assetData := []byte("verified runtime")
	raw, signature, manifest := testManifest(t, identity, assetData)
	publicKey, _ := testKey()
	var currentManifest = raw
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/runtime-manifest.json":
			response.Write(currentManifest)
		case "/runtime-manifest.sig":
			response.Write(signature)
		case "/v0.0.0/ocean-watch_darwin_amd64":
			response.Write(assetData)
		default:
			http.NotFound(response, request)
		}
	}))
	cacheRoot := t.TempDir()
	config := testConfig(t, server.URL, cacheRoot, raw, signature, publicKey)
	if _, err := Ensure(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	altered := manifest
	altered.PluginVersion = "0.0.0+codex.other"
	currentManifest, _ = json.Marshal(altered)
	alteredConfig := testConfig(t, server.URL, t.TempDir(), raw, signature, publicKey)
	if _, err := Ensure(context.Background(), alteredConfig); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("expected signature failure, got %v", err)
	}
	currentManifest = raw
	assetPath := filepath.Join(cacheRoot, "0.0.0", "darwin-amd64", "ocean-watch_darwin_amd64")
	if err := os.WriteFile(assetPath, []byte("damaged"), 0o700); err != nil {
		t.Fatal(err)
	}
	server.Close()
	if _, err := Ensure(context.Background(), config); err == nil {
		t.Fatal("expected offline damaged-cache failure")
	}
	if _, err := os.Stat(assetPath); !os.IsNotExist(err) {
		t.Fatalf("damaged cache should be removed, stat=%v", err)
	}
}

func TestEnsureConcurrentLaunchesPromoteOnlyVerifiedAssets(t *testing.T) {
	identity := testIdentity()
	assetData := []byte("verified runtime")
	raw, signature, _ := testManifest(t, identity, assetData)
	publicKey, _ := testKey()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/runtime-manifest.json":
			response.Write(raw)
		case "/runtime-manifest.sig":
			response.Write(signature)
		case "/v0.0.0/ocean-watch_darwin_amd64":
			response.Write(assetData)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	config := testConfig(t, server.URL, t.TempDir(), raw, signature, publicKey)
	var wait sync.WaitGroup
	errors := make(chan error, 8)
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := Ensure(context.Background(), config)
			errors <- err
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestValidateManifestRejectsIdentityPlatformAndUnsafeAsset(t *testing.T) {
	identity := testIdentity()
	assetData := []byte("verified runtime")
	_, _, manifest := testManifest(t, identity, assetData)
	if _, _, err := ValidateManifest(manifest, Identity{ProductVersion: "other", PluginVersion: identity.PluginVersion, GitCommit: identity.GitCommit, SDKVersion: identity.SDKVersion}, "accounts list", "darwin", "amd64", 1024); err == nil {
		t.Fatal("expected product mismatch")
	}
	manifest.Assets["darwin-amd64"] = Asset{Name: "../escape", SHA256: strings.Repeat("a", 64), Size: 1}
	if _, _, err := ValidateManifest(manifest, identity, "accounts list", "darwin", "amd64", 1024); err == nil {
		t.Fatal("expected unsafe asset rejection")
	}
	if _, _, err := ValidateManifest(manifest, identity, "accounts list", "plan9", "amd64", 1024); err == nil {
		t.Fatal("expected unsupported platform rejection")
	}
}

func TestEnsureUnsupportedPlatformDoesNotNeedNetwork(t *testing.T) {
	config := Config{Route: "accounts list", GOOS: "plan9", GOARCH: "amd64", CacheRoot: t.TempDir()}
	if _, err := Ensure(context.Background(), config); err == nil || !strings.Contains(err.Error(), "unsupported runtime platform") {
		t.Fatalf("expected platform failure before network, got %v", err)
	}
}

func TestVerifyManifestRejectsWrongKeyMalformedSignatureAndNonCanonicalJSON(t *testing.T) {
	identity := testIdentity()
	raw, signature, _ := testManifest(t, identity, []byte("runtime"))
	publicKey, privateKey := testKey()
	if _, err := VerifyManifest(raw, signature, publicKey); err != nil {
		t.Fatal(err)
	}
	wrongSeed := bytes.Repeat([]byte{0x42}, ed25519.SeedSize)
	wrongKey := ed25519.NewKeyFromSeed(wrongSeed).Public().(ed25519.PublicKey)
	if _, err := VerifyManifest(raw, signature, wrongKey); err == nil {
		t.Fatal("expected wrong key rejection")
	}
	if _, err := VerifyManifest(raw, []byte("not-a-signature"), publicKey); err == nil {
		t.Fatal("expected malformed signature rejection")
	}
	pretty := append([]byte("\n"), raw...)
	pretty = append(pretty, '\n')
	prettySignature := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, pretty)))
	if _, err := VerifyManifest(pretty, prettySignature, publicKey); err == nil || !strings.Contains(err.Error(), "canonical") {
		t.Fatalf("expected canonical JSON rejection, got %v", err)
	}
}

func TestValidateManifestRejectsEachIdentityAndBoundsViolation(t *testing.T) {
	identity := testIdentity()
	_, _, base := testManifest(t, identity, []byte("runtime"))
	checks := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{"plugin", func(value *Manifest) { value.PluginVersion = "other" }},
		{"commit", func(value *Manifest) { value.GitCommit = strings.Repeat("b", 40) }},
		{"sdk", func(value *Manifest) { value.SDKVersion = "v0.0.0" }},
		{"tag", func(value *Manifest) { value.Tag = "vother" }},
		{"route", func(value *Manifest) { value.Routes["accounts list"] = "shell" }},
		{"digest", func(value *Manifest) {
			value.Assets["darwin-amd64"] = Asset{Name: "ocean-watch_darwin_amd64", SHA256: "bad", Size: 1}
		}},
		{"size", func(value *Manifest) {
			value.Assets["darwin-amd64"] = Asset{Name: "ocean-watch_darwin_amd64", SHA256: strings.Repeat("a", 64), Size: 0}
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			manifest := base
			manifest.Routes = map[string]string{"accounts list": "go"}
			manifest.Assets = map[string]Asset{"darwin-amd64": base.Assets["darwin-amd64"]}
			check.mutate(&manifest)
			if _, _, err := ValidateManifest(manifest, identity, "accounts list", "darwin", "amd64", 1024); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
	manifest := base
	manifest.Assets["darwin-amd64"] = Asset{Name: "ocean-watch_darwin_amd64", SHA256: strings.Repeat("a", 64), Size: 2048}
	if _, _, err := ValidateManifest(manifest, identity, "accounts list", "darwin", "amd64", 1024); err == nil {
		t.Fatal("expected max-size rejection")
	}
}

func TestDownloadAssetRejectsTruncationDigestAndCleansTemporaryFiles(t *testing.T) {
	asset := Asset{Name: "ocean-watch_darwin_amd64", SHA256: strings.Repeat("a", 64), Size: 10}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Write([]byte("short"))
	}))
	defer server.Close()
	directory := t.TempDir()
	err := downloadAsset(context.Background(), secureClient(nil, server.URL+"/asset"), server.URL+"/asset", filepath.Join(directory, asset.Name), asset)
	if err == nil {
		t.Fatalf("expected truncation failure, got %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(directory, ".runtime-*.tmp")); len(matches) != 0 {
		t.Fatalf("temporary download was not removed: %v", matches)
	}

	asset.Size = 5
	asset.SHA256 = strings.Repeat("b", 64)
	err = downloadAsset(context.Background(), secureClient(nil, server.URL+"/asset"), server.URL+"/asset", filepath.Join(directory, asset.Name), asset)
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("expected digest failure, got %v", err)
	}
}

func TestSecureClientRejectsCrossOriginRedirect(t *testing.T) {
	foreign := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Write([]byte("foreign"))
	}))
	defer foreign.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, foreign.URL, http.StatusFound)
	}))
	defer redirect.Close()
	client := secureClient(nil, redirect.URL)
	request, err := http.NewRequest(http.MethodGet, redirect.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Do(request)
	if err == nil || !strings.Contains(err.Error(), "cross-origin") {
		t.Fatalf("expected redirect rejection, got %v", err)
	}
}

func TestValidCacheDoesNotProbeNetwork(t *testing.T) {
	identity := testIdentity()
	assetData := []byte("verified runtime")
	raw, signature, _ := testManifest(t, identity, assetData)
	publicKey, _ := testKey()
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		switch request.URL.Path {
		case "/runtime-manifest.json":
			response.Write(raw)
		case "/runtime-manifest.sig":
			response.Write(signature)
		default:
			response.Write(assetData)
		}
	}))
	cacheRoot := t.TempDir()
	config := testConfig(t, server.URL, cacheRoot, raw, signature, publicKey)
	if _, err := Ensure(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	firstRequests := requests
	server.Close()
	started := time.Now()
	if _, err := Ensure(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	if requests != firstRequests {
		t.Fatalf("valid cache should not request network: before=%d after=%d", firstRequests, requests)
	}
	if time.Since(started) > time.Second {
		t.Fatal("cache reuse took too long")
	}
}
