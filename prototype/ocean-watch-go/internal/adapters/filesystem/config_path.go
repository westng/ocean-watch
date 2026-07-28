package filesystem

import (
	"os"
	"path/filepath"
)

const (
	ConfigEnv    = "ADS_PLAN_MONITOR_CONFIG"
	CodexHomeEnv = "CODEX_HOME"
)

func CodexHome(getenv func(string) string, userHome string) string {
	if value := getenv(CodexHomeEnv); value != "" {
		return value
	}
	return filepath.Join(userHome, ".codex")
}

func ResolveConfigPath(explicit, cwd string, getenv func(string) string, userHome string) string {
	if explicit != "" {
		return expandHome(explicit, userHome)
	}
	if value := getenv(ConfigEnv); value != "" {
		return expandHome(value, userHome)
	}
	for root := cwd; root != filepath.Dir(root); root = filepath.Dir(root) {
		manifest := filepath.Join(root, ".codex-plugin", "plugin.json")
		project := filepath.Join(root, "config", "ads-plan-monitor", "config.json")
		if fileExists(manifest) && fileExists(project) {
			return project
		}
	}
	return filepath.Join(CodexHome(getenv, userHome), "ads-plan-monitor", "config.json")
}

func ResolveInitializationConfigPath(
	explicit string,
	homeConfig bool,
	cwd string,
	getenv func(string) string,
	userHome string,
) string {
	if homeConfig {
		return filepath.Join(CodexHome(getenv, userHome), "ads-plan-monitor", "config.json")
	}
	if explicit != "" {
		return expandHome(explicit, userHome)
	}
	if value := getenv(ConfigEnv); value != "" {
		return expandHome(value, userHome)
	}
	for root := cwd; ; root = filepath.Dir(root) {
		if fileExists(filepath.Join(root, ".codex-plugin", "plugin.json")) {
			return filepath.Join(root, "config", "ads-plan-monitor", "config.json")
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
	}
	return filepath.Join(CodexHome(getenv, userHome), "ads-plan-monitor", "config.json")
}

func ResolvePluginRoot(cwd string) string {
	for root := cwd; ; root = filepath.Dir(root) {
		if fileExists(filepath.Join(root, ".codex-plugin", "plugin.json")) {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			return ""
		}
	}
}

func expandHome(path, userHome string) string {
	if path == "~" {
		return userHome
	}
	if len(path) > 2 && path[:2] == "~/" {
		return filepath.Join(userHome, path[2:])
	}
	return filepath.Clean(path)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
