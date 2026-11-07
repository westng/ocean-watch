package runtimeupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// NewManager gives an explicit repository checkout its own runtime selection
// state. Business configuration and credentials keep using the shared home.
func NewManager(codexRoot, pluginRoot string) Manager {
	manager := Manager{CodexRoot: codexRoot, PluginRoot: pluginRoot}
	manager.releaseAnchor = manager.anchorVersion()
	manager.anchorFixed = true
	if isPrerelease(manager.releaseAnchor) {
		manager.releaseManifestSHA256, _ = runtimeManifestHash(pluginRoot)
		if manager.releaseManifestSHA256 == "" {
			manager.releaseManifestSHA256 = "invalid"
		}
	}
	if strings.TrimSpace(codexRoot) == "" || strings.TrimSpace(pluginRoot) == "" {
		return manager
	}
	root, err := filepath.Abs(pluginRoot)
	if err != nil {
		return manager
	}
	manager.PluginRoot = root
	scope := checkoutRuntimeHome(codexRoot, root)
	if scope == "" {
		return manager
	}
	manager.CodexRoot, manager.LocalSource = scope, true
	manager.Discover = func() (string, string, error) {
		payload, err := readRegularFile(filepath.Join(root, ".codex-plugin", "runtime-manifest.json"))
		if err != nil {
			return "", "", err
		}
		var manifest Manifest
		if err := json.Unmarshal(payload, &manifest); err != nil {
			return "", "", err
		}
		if manifest.Version == "" {
			return "", "", errors.New("local Ocean Watch runtime manifest has no version")
		}
		return root, manifest.Version, nil
	}
	return manager
}

// ForHostContract isolates selection and rejection from older host sessions.
// Every scope still validates the complete bundle and its own tool contract.
func (manager Manager) ForHostContract(catalog []byte) Manager {
	digest := sha256.Sum256(catalog)
	manager.hostContractSHA256 = hex.EncodeToString(digest[:])
	return manager
}

func checkoutRuntimeHome(codexRoot, pluginRoot string) string {
	pool := filepath.Join(codexRoot, "ocean-watch", "checkouts")
	// A managed subprocess no longer has .git; recover its original scope from
	// the validated private slot path so doctor and host retention stay local.
	if relative, err := filepath.Rel(pool, pluginRoot); err == nil {
		parts := strings.Split(relative, string(filepath.Separator))
		if len(parts) == 5 && validSHA256(parts[0]) && parts[1] == "ocean-watch" &&
			parts[2] == "runtime" && parts[3] == "versions" && validSHA256(parts[4]) {
			return filepath.Join(pool, parts[0])
		}
	}
	cache := filepath.Join(codexRoot, "plugins", "cache")
	if relative, err := filepath.Rel(cache, pluginRoot); err == nil && relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ""
	}
	git, err := os.Lstat(filepath.Join(pluginRoot, ".git"))
	if err != nil || git.Mode()&os.ModeSymlink != 0 || !git.IsDir() && !git.Mode().IsRegular() {
		return ""
	}
	digest := sha256.Sum256([]byte(pluginRoot))
	return filepath.Join(pool, hex.EncodeToString(digest[:]))
}
