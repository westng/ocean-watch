package bootstrap

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultMaxManifestBytes  = 1 << 20
	defaultMaxSignatureBytes = 4 << 10
	defaultMaxAssetBytes     = 256 << 20
)

type Config struct {
	Identity         Identity
	Route            string
	GOOS             string
	GOARCH           string
	CacheRoot        string
	ManifestURL      string
	SignatureURL     string
	ReleaseBaseURL   string
	TrustedPublicKey ed25519.PublicKey
	HTTPClient       *http.Client
	// AllowInsecureForTests is intentionally unavailable from the production CLI.
	AllowInsecureForTests bool
	MaxManifestBytes      int64
	MaxSignatureBytes     int64
	MaxAssetBytes         int64
}

type Result struct {
	Route     string `json:"route"`
	AssetPath string `json:"asset_path,omitempty"`
	CacheHit  bool   `json:"cache_hit"`
	Platform  string `json:"platform"`
}

func Ensure(ctx context.Context, config Config) (Result, error) {
	if config.Route == "" {
		return Result{}, errors.New("runtime route command is required")
	}
	if _, err := PlatformKey(config.GOOS, config.GOARCH); err != nil {
		return Result{}, err
	}
	if config.MaxManifestBytes == 0 {
		config.MaxManifestBytes = defaultMaxManifestBytes
	}
	if config.MaxSignatureBytes == 0 {
		config.MaxSignatureBytes = defaultMaxSignatureBytes
	}
	if config.MaxAssetBytes == 0 {
		config.MaxAssetBytes = defaultMaxAssetBytes
	}
	if config.CacheRoot == "" {
		return Result{}, errors.New("runtime cache root is required")
	}
	if err := validateFixedURLs(config); err != nil {
		return Result{}, err
	}
	platform := config.GOOS + "-" + config.GOARCH
	cacheDir := filepath.Join(config.CacheRoot, config.Identity.ProductVersion, platform)
	if err := ensurePrivateDirectory(config.CacheRoot, cacheDir); err != nil {
		return Result{}, err
	}
	if cached, ok, err := verifiedCachedResult(cacheDir, config, platform); err != nil {
		return Result{}, err
	} else if ok {
		return cached, nil
	}
	client := secureClient(config.HTTPClient, config.ManifestURL)
	manifestBytes, err := downloadBounded(ctx, client, config.ManifestURL, config.MaxManifestBytes)
	onlineManifest := err == nil
	var signatureBytes []byte
	if onlineManifest {
		signatureBytes, err = downloadBounded(ctx, client, config.SignatureURL, config.MaxSignatureBytes)
		onlineManifest = err == nil
	}
	if !onlineManifest {
		manifestBytes, signatureBytes, err = loadCachedManifest(cacheDir, config.MaxManifestBytes, config.MaxSignatureBytes)
		if err != nil {
			return Result{}, fmt.Errorf("runtime manifest unavailable online and no valid cache candidate exists: %w", err)
		}
	}
	manifest, err := VerifyManifest(manifestBytes, signatureBytes, config.TrustedPublicKey)
	if err != nil {
		return Result{}, err
	}
	selectedRoute, asset, err := ValidateManifest(
		manifest,
		config.Identity,
		config.Route,
		config.GOOS,
		config.GOARCH,
		config.MaxAssetBytes,
	)
	if err != nil {
		return Result{}, err
	}
	if onlineManifest {
		if err := cacheVerifiedManifest(cacheDir, manifestBytes, signatureBytes); err != nil {
			return Result{}, err
		}
	}
	result := Result{Route: selectedRoute, Platform: platform}
	if selectedRoute == "python" {
		return result, nil
	}
	target := filepath.Join(cacheDir, asset.Name)
	cacheHit, err := validateCachedAsset(target, asset)
	if err != nil {
		return Result{}, err
	}
	if cacheHit {
		result.AssetPath = target
		result.CacheHit = true
		return result, nil
	}
	assetURL := strings.TrimRight(config.ReleaseBaseURL, "/") + "/" + manifest.Tag + "/" + asset.Name
	if err := downloadAsset(ctx, secureClient(config.HTTPClient, assetURL), assetURL, target, asset); err != nil {
		return Result{}, err
	}
	result.AssetPath = target
	return result, nil
}

func verifiedCachedResult(cacheDir string, config Config, platform string) (Result, bool, error) {
	manifestBytes, signatureBytes, err := loadCachedManifest(cacheDir, config.MaxManifestBytes, config.MaxSignatureBytes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Result{}, false, nil
		}
		return Result{}, false, fmt.Errorf("read cached runtime manifest: %w", err)
	}
	manifest, err := VerifyManifest(manifestBytes, signatureBytes, config.TrustedPublicKey)
	if err != nil {
		return Result{}, false, fmt.Errorf("verify cached runtime manifest: %w", err)
	}
	selectedRoute, asset, err := ValidateManifest(
		manifest,
		config.Identity,
		config.Route,
		config.GOOS,
		config.GOARCH,
		config.MaxAssetBytes,
	)
	if err != nil {
		return Result{}, false, fmt.Errorf("validate cached runtime manifest: %w", err)
	}
	result := Result{Route: selectedRoute, Platform: platform, CacheHit: true}
	if selectedRoute == "python" {
		return result, true, nil
	}
	target := filepath.Join(cacheDir, asset.Name)
	cacheHit, err := validateCachedAsset(target, asset)
	if err != nil {
		return Result{}, false, err
	}
	if !cacheHit {
		return Result{}, false, nil
	}
	result.AssetPath = target
	return result, true, nil
}

func loadCachedManifest(cacheDir string, manifestLimit, signatureLimit int64) ([]byte, []byte, error) {
	manifest, err := readPrivateRegularFile(filepath.Join(cacheDir, "runtime-manifest.json"), manifestLimit)
	if err != nil {
		return nil, nil, err
	}
	signature, err := readPrivateRegularFile(filepath.Join(cacheDir, "runtime-manifest.sig"), signatureLimit)
	if err != nil {
		return nil, nil, err
	}
	return manifest, signature, nil
}

func readPrivateRegularFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > limit || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("cached runtime metadata is unsafe")
	}
	return os.ReadFile(path)
}

func cacheVerifiedManifest(cacheDir string, manifest, signature []byte) error {
	if err := writeAtomicFile(filepath.Join(cacheDir, "runtime-manifest.json"), manifest, 0o600); err != nil {
		return fmt.Errorf("cache verified runtime manifest: %w", err)
	}
	if err := writeAtomicFile(filepath.Join(cacheDir, "runtime-manifest.sig"), signature, 0o600); err != nil {
		return fmt.Errorf("cache verified runtime signature: %w", err)
	}
	return nil
}

func writeAtomicFile(path string, data []byte, mode os.FileMode) error {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("runtime metadata cache is not a regular file")
		}
		existing, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(existing) == string(data) {
			return os.Chmod(path, mode)
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".metadata-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	keep := false
	defer func() {
		if !keep {
			_ = temporary.Close()
			_ = os.Remove(temporaryName)
		}
	}()
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		if existing, readErr := os.ReadFile(path); readErr == nil && string(existing) == string(data) {
			return nil
		}
		return err
	}
	keep = true
	return nil
}

func validateFixedURLs(config Config) error {
	if config.ManifestURL == "" || config.SignatureURL == "" || config.ReleaseBaseURL == "" {
		return errors.New("runtime release URLs are required")
	}
	manifest, err := url.Parse(config.ManifestURL)
	if err != nil || (manifest.Scheme != "https" && !(config.AllowInsecureForTests && manifest.Scheme == "http")) || manifest.Host == "" {
		return errors.New("runtime manifest URL is invalid")
	}
	signature, err := url.Parse(config.SignatureURL)
	if err != nil || signature.Scheme != manifest.Scheme || signature.Host != manifest.Host {
		return errors.New("runtime signature URL must share the manifest origin")
	}
	release, err := url.Parse(config.ReleaseBaseURL)
	if err != nil || release.Scheme != manifest.Scheme || release.Host != manifest.Host {
		return errors.New("runtime release URL must share the manifest origin")
	}
	return nil
}

func secureClient(base *http.Client, expectedURL string) *http.Client {
	client := &http.Client{Timeout: 30 * time.Second}
	if base != nil {
		*client = *base
		if client.Timeout == 0 {
			client.Timeout = 30 * time.Second
		}
	}
	expected, _ := url.Parse(expectedURL)
	previousCheck := client.CheckRedirect
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if request.URL.Scheme != expected.Scheme || request.URL.Host != expected.Host {
			return errors.New("cross-origin runtime redirect rejected")
		}
		if previousCheck != nil {
			return previousCheck(request, via)
		}
		if len(via) >= 5 {
			return errors.New("too many runtime redirects")
		}
		return nil
	}
	return client
}

func downloadBounded(ctx context.Context, client *http.Client, location string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %d", response.StatusCode)
	}
	if response.ContentLength > limit {
		return nil, errors.New("response exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("response exceeds size limit")
	}
	return data, nil
}

func ensurePrivateDirectory(root, path string) error {
	if info, err := os.Lstat(root); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("runtime cache root cannot be a symlink")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create runtime cache root: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return fmt.Errorf("secure runtime cache root: %w", err)
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("runtime cache directory cannot be a symlink")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create runtime cache: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure runtime cache: %w", err)
	}
	rootAbsolute, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve runtime cache root: %w", err)
	}
	pathAbsolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve runtime cache path: %w", err)
	}
	relative, err := filepath.Rel(rootAbsolute, pathAbsolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("runtime cache path escapes the cache root")
	}
	current := rootAbsolute
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("runtime cache path contains a symlink")
		}
	}
	return nil
}

func validateCachedAsset(path string, asset Asset) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, errors.New("runtime cache asset is not a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return false, errors.New("runtime cache asset permissions are too broad")
	}
	if info.Size() != asset.Size {
		if err := os.Remove(path); err != nil {
			return false, fmt.Errorf("remove damaged runtime cache: %w", err)
		}
		return false, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	digest := sha256.New()
	_, copyErr := io.Copy(digest, file)
	closeErr := file.Close()
	if copyErr != nil {
		return false, copyErr
	}
	if closeErr != nil {
		return false, closeErr
	}
	if hex.EncodeToString(digest.Sum(nil)) != asset.SHA256 {
		if err := os.Remove(path); err != nil {
			return false, fmt.Errorf("remove damaged runtime cache: %w", err)
		}
		return false, nil
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return false, fmt.Errorf("secure runtime cache asset: %w", err)
	}
	return true, nil
}

func downloadAsset(ctx context.Context, client *http.Client, location, target string, asset Asset) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download runtime asset: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download runtime asset: unexpected HTTP status %d", response.StatusCode)
	}
	if response.ContentLength >= 0 && response.ContentLength != asset.Size {
		return errors.New("runtime asset Content-Length mismatch")
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".runtime-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	keep := false
	defer func() {
		if !keep {
			_ = temporary.Close()
			_ = os.Remove(temporaryName)
		}
	}()
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(response.Body, asset.Size+1))
	if copyErr != nil {
		return fmt.Errorf("write runtime asset: %w", copyErr)
	}
	if written != asset.Size {
		return errors.New("runtime asset size mismatch")
	}
	if hex.EncodeToString(hash.Sum(nil)) != asset.SHA256 {
		return errors.New("runtime asset SHA-256 mismatch")
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Chmod(0o700); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, target); err != nil {
		if valid, validationErr := validateCachedAsset(target, asset); validationErr == nil && valid {
			return nil
		}
		return fmt.Errorf("promote runtime asset: %w", err)
	}
	keep = true
	return nil
}
