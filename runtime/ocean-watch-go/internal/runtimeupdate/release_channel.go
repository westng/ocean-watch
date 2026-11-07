package runtimeupdate

import (
	"encoding/json"
	"path/filepath"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/productversion"
)

func isPrerelease(value string) bool {
	v, err := productversion.Parse(value)
	return err == nil && v.Prerelease()
}

// Only the installed plugin identity selects the channel; business calls and
// shared mutable selection records cannot opt a stable process into alpha.
func (manager Manager) anchorVersion() string {
	if manager.anchorFixed {
		return manager.releaseAnchor
	}
	payload, err := readRegularFile(filepath.Join(manager.PluginRoot, ".codex-plugin", "plugin.json"))
	if err != nil {
		return ""
	}
	var identity struct{ Name, Version string }
	if json.Unmarshal(payload, &identity) != nil || identity.Name != pluginName {
		return ""
	}
	return identity.Version
}

func (manager Manager) allowVersion(value string) bool {
	v, err := productversion.Parse(value)
	if err != nil {
		return false
	}
	anchor := manager.anchorVersion()
	if isPrerelease(anchor) {
		return value == anchor
	}
	return !v.Prerelease()
}
