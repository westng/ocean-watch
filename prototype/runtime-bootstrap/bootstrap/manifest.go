package bootstrap

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const (
	ManifestVersion   = 1
	DefaultSDKVersion = "v1.1.92"
)

var (
	hexDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	commitPattern    = regexp.MustCompile(`^[a-f0-9]{40}$`)
	assetNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

type Asset struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type Manifest struct {
	ManifestVersion int               `json:"manifest_version"`
	ProductVersion  string            `json:"product_version"`
	PluginVersion   string            `json:"plugin_version"`
	GitCommit       string            `json:"git_commit"`
	SDKVersion      string            `json:"sdk_version"`
	Tag             string            `json:"tag"`
	Routes          map[string]string `json:"routes"`
	Assets          map[string]Asset  `json:"assets"`
}

type Identity struct {
	ProductVersion string
	PluginVersion  string
	GitCommit      string
	SDKVersion     string
}

func VerifyManifest(raw, signature []byte, publicKey ed25519.PublicKey) (Manifest, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return Manifest{}, errors.New("invalid trusted Ed25519 public key")
	}
	decodedSignature, err := decodeSignature(signature)
	if err != nil {
		return Manifest{}, err
	}
	if !ed25519.Verify(publicKey, raw, decodedSignature) {
		return Manifest{}, errors.New("runtime manifest signature verification failed")
	}
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return Manifest{}, fmt.Errorf("invalid runtime manifest JSON: %w", err)
	}
	if !bytes.Equal(raw, canonical) {
		return Manifest{}, errors.New("runtime manifest is not canonical JSON")
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode runtime manifest: %w", err)
	}
	return manifest, nil
}

func decodeSignature(value []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == ed25519.SignatureSize {
		return append([]byte(nil), trimmed...), nil
	}
	decoded, err := base64.StdEncoding.DecodeString(string(trimmed))
	if err != nil || len(decoded) != ed25519.SignatureSize {
		return nil, errors.New("invalid detached Ed25519 signature")
	}
	return decoded, nil
}

func canonicalJSON(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, err
	}
	return json.Marshal(value)
}

func ValidateManifest(manifest Manifest, identity Identity, route, goos, goarch string, maxAssetBytes int64) (string, Asset, error) {
	if manifest.ManifestVersion != ManifestVersion {
		return "", Asset{}, fmt.Errorf("unsupported manifest_version %d", manifest.ManifestVersion)
	}
	if identity.SDKVersion == "" {
		identity.SDKVersion = DefaultSDKVersion
	}
	checks := []struct {
		name     string
		actual   string
		expected string
	}{
		{"product_version", manifest.ProductVersion, identity.ProductVersion},
		{"plugin_version", manifest.PluginVersion, identity.PluginVersion},
		{"git_commit", manifest.GitCommit, identity.GitCommit},
		{"sdk_version", manifest.SDKVersion, identity.SDKVersion},
		{"tag", manifest.Tag, "v" + identity.ProductVersion},
	}
	for _, check := range checks {
		if check.actual == "" || check.actual != check.expected {
			return "", Asset{}, fmt.Errorf("runtime manifest %s mismatch", check.name)
		}
	}
	if !commitPattern.MatchString(manifest.GitCommit) {
		return "", Asset{}, errors.New("runtime manifest git_commit is malformed")
	}
	selectedRoute, ok := manifest.Routes[route]
	if !ok {
		return "", Asset{}, fmt.Errorf("runtime route is missing: %s", route)
	}
	if selectedRoute != "go" && selectedRoute != "python" {
		return "", Asset{}, fmt.Errorf("runtime route is invalid: %s", selectedRoute)
	}
	if selectedRoute == "python" {
		return selectedRoute, Asset{}, nil
	}
	platform := goos + "-" + goarch
	asset, ok := manifest.Assets[platform]
	if !ok {
		return "", Asset{}, fmt.Errorf("runtime platform is unsupported: %s", platform)
	}
	expectedName := fmt.Sprintf("ocean-watch_%s_%s", goos, goarch)
	if goos == "windows" {
		expectedName += ".exe"
	}
	if !assetNamePattern.MatchString(asset.Name) || asset.Name != filepath.Base(asset.Name) || strings.Contains(asset.Name, "..") {
		return "", Asset{}, errors.New("runtime asset name is unsafe")
	}
	if asset.Name != expectedName {
		return "", Asset{}, errors.New("runtime asset name does not match platform")
	}
	if !hexDigestPattern.MatchString(asset.SHA256) {
		return "", Asset{}, errors.New("runtime asset digest is malformed")
	}
	if asset.Size <= 0 || asset.Size > maxAssetBytes {
		return "", Asset{}, errors.New("runtime asset size is outside the allowed range")
	}
	return selectedRoute, asset, nil
}

func PlatformKey(goos, goarch string) (string, error) {
	supported := map[string]bool{
		"darwin-amd64":  true,
		"darwin-arm64":  true,
		"linux-amd64":   true,
		"linux-arm64":   true,
		"windows-amd64": true,
	}
	key := goos + "-" + goarch
	if !supported[key] {
		return "", fmt.Errorf("unsupported runtime platform: %s", key)
	}
	return key, nil
}

func CurrentPlatform() (string, string, error) {
	if _, err := PlatformKey(runtime.GOOS, runtime.GOARCH); err != nil {
		return "", "", err
	}
	return runtime.GOOS, runtime.GOARCH, nil
}

func VerifyFileDigest(data []byte, asset Asset) error {
	if int64(len(data)) != asset.Size {
		return errors.New("runtime asset size mismatch")
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != asset.SHA256 {
		return errors.New("runtime asset SHA-256 mismatch")
	}
	return nil
}
