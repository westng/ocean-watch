package runtimeupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestResolveSwitchesToValidatedInstalledRuntimeWithoutDowngrade(t *testing.T) {
	root := t.TempDir()
	fallback := writeCandidate(t, root, "fallback", "1.0.9+codex.old", "old")
	installed := writeCandidate(t, filepath.Join(root, "codex", "plugins", "cache", marketplaceName, pluginName), "1.0.10+codex.new", "1.0.10+codex.new", "new")
	manager := testManager(root, fallback.PluginRoot, installed)

	selected, err := manager.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if selected.Version != installed.Version || selected.SHA256 != installed.SHA256 ||
		selected.BinaryPath == installed.BinaryPath {
		t.Fatalf("unexpected selected runtime: %#v", selected)
	}

	manager.Discover = func() (string, string, error) {
		return fallback.PluginRoot, fallback.Version, nil
	}
	selected, err = manager.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if selected.Version != installed.Version {
		t.Fatalf("automatic downgrade was accepted: %#v", selected)
	}

}

func TestResolveRejectsTamperedInstalledRuntime(t *testing.T) {
	root := t.TempDir()
	fallback := writeCandidate(t, root, "fallback", "1.0.9+codex.old", "old")
	installed := writeCandidate(t, filepath.Join(root, "codex", "plugins", "cache", marketplaceName, pluginName), "1.0.10+codex.new", "1.0.10+codex.new", "new")
	if err := os.WriteFile(installed.BinaryPath, []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	manager := testManager(root, fallback.PluginRoot, installed)
	selected, err := manager.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if selected.Version != fallback.Version {
		t.Fatalf("tampered installed runtime was selected: %#v", selected)
	}
}

func TestRejectRestoresPreviousAndRejectsCandidate(t *testing.T) {
	root := t.TempDir()
	fallback := writeCandidate(t, root, "fallback", "1.0.9+codex.old", "old")
	installed := writeCandidate(t, filepath.Join(root, "codex", "plugins", "cache", marketplaceName, pluginName), "1.0.10+codex.new", "1.0.10+codex.new", "new")
	manager := testManager(root, fallback.PluginRoot, installed)
	selected, err := manager.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	restored, err := manager.Reject(context.Background(), selected)
	if err != nil || restored.Version != fallback.Version {
		t.Fatalf("reject did not restore fallback: restored=%#v err=%v", restored, err)
	}
	state, err := manager.Status()
	if err != nil || state.Previous != nil || state.Current.Version != fallback.Version ||
		state.RejectedSHA256 != selected.SHA256 {
		t.Fatalf("rejected runtime state is unsafe: state=%#v err=%v", state, err)
	}
	selected, err = manager.Resolve(context.Background())
	if err != nil || selected.Version != fallback.Version {
		t.Fatalf("rejected runtime was automatically retried: selected=%#v err=%v", selected, err)
	}
	fixed := writeCandidate(t, filepath.Join(root, "codex", "plugins", "cache", marketplaceName, pluginName), "1.0.11+codex.fixed", "1.0.11+codex.fixed", "fixed")
	manager.Discover = func() (string, string, error) {
		return fixed.PluginRoot, fixed.Version, nil
	}
	selected, err = manager.Resolve(context.Background())
	if err != nil || selected.Version != fixed.Version {
		t.Fatalf("a later fixed runtime was blocked: selected=%#v err=%v", selected, err)
	}
}

func TestResolveRejectsTamperedPluginAndResource(t *testing.T) {
	for _, relative := range []string{".codex-plugin/plugin.json", "f2/resolve.py"} {
		t.Run(relative, func(t *testing.T) {
			root := t.TempDir()
			fallback := writeCandidate(t, root, "fallback", "1.0.9+codex.old", "old")
			installed := writeCandidate(t, filepath.Join(root, "codex", "plugins", "cache", marketplaceName, pluginName), "1.0.10+codex.new", "1.0.10+codex.new", "new")
			if err := os.WriteFile(filepath.Join(installed.PluginRoot, filepath.FromSlash(relative)), []byte("tampered"), 0o600); err != nil {
				t.Fatal(err)
			}
			selected, err := testManager(root, fallback.PluginRoot, installed).Resolve(context.Background())
			if err != nil || selected.Version != fallback.Version {
				t.Fatalf("tampered candidate was selected: selected=%#v err=%v", selected, err)
			}
		})
	}
}

func TestResolveReusesValidatedPrivateSlotWithoutRehashing(t *testing.T) {
	root := t.TempDir()
	fallback := writeCandidate(t, root, "fallback", "1.0.9+codex.same", "fallback")
	installed := writeCandidate(t, filepath.Join(root, "codex", "plugins", "cache", marketplaceName, pluginName), "1.0.9+codex.same", "1.0.9+codex.same", "installed")
	manager := testManager(root, fallback.PluginRoot, installed)
	selected, err := manager.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	manager.ProbeVersion = func(context.Context, string) ([]byte, error) {
		return nil, errors.New("full validation unexpectedly ran")
	}
	again, err := manager.Resolve(context.Background())
	if err != nil || again.SHA256 != selected.SHA256 {
		t.Fatalf("validated private slot was not reused: selected=%#v again=%#v err=%v", selected, again, err)
	}
}

func TestPruneObsoleteRuntimesWaitsForActiveLease(t *testing.T) {
	root := t.TempDir()
	oldSource := writeCandidate(t, root, "old", "1.0.9+codex.old", "old")
	newSource := writeCandidate(t, root, "new", "1.0.10+codex.new", "new")
	manager := testManager(root, oldSource.PluginRoot, oldSource)
	oldRuntime, err := manager.installCandidate(context.Background(), oldSource)
	if err != nil {
		t.Fatal(err)
	}
	manager.Discover = func() (string, string, error) {
		return newSource.PluginRoot, newSource.Version, nil
	}
	newRuntime, err := manager.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	lease, err := manager.AcquireRuntimeLease(context.Background(), oldRuntime)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.PruneObsoleteRuntimes(context.Background(), newRuntime); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldRuntime.PluginRoot); err != nil {
		t.Fatalf("leased runtime was removed: %v", err)
	}
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
	if err := manager.PruneObsoleteRuntimes(context.Background(), newRuntime); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldRuntime.PluginRoot); !os.IsNotExist(err) {
		t.Fatalf("released obsolete runtime still exists: %v", err)
	}
	if _, err := os.Stat(newRuntime.PluginRoot); err != nil {
		t.Fatalf("current runtime was removed: %v", err)
	}
}

func TestResolveLeasedProtectsSelectedRuntimeUntilRelease(t *testing.T) {
	root := t.TempDir()
	oldSource := writeCandidate(t, root, "old", "1.0.9+codex.old", "old")
	newSource := writeCandidate(t, root, "new", "1.0.10+codex.new", "new")
	manager := testManager(root, oldSource.PluginRoot, oldSource)
	oldRuntime, lease, err := manager.ResolveLeased(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	manager.Discover = func() (string, string, error) {
		return newSource.PluginRoot, newSource.Version, nil
	}
	newRuntime, err := manager.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.PruneObsoleteRuntimes(context.Background(), newRuntime); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldRuntime.PluginRoot); err != nil {
		t.Fatalf("atomically leased runtime was removed: %v", err)
	}
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
	if err := manager.PruneObsoleteRuntimes(context.Background(), newRuntime); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldRuntime.PluginRoot); !os.IsNotExist(err) {
		t.Fatalf("released runtime still exists: %v", err)
	}
}

func TestDiscoverInstalledSelectsHighestProductAndLatestSameVersion(t *testing.T) {
	root := t.TempDir()
	cacheRoot := filepath.Join(root, "codex", "plugins", "cache", marketplaceName, pluginName)
	old := writeCandidate(t, cacheRoot, "1.0.9+codex.old", "1.0.9+codex.old", "old")
	latestSameProduct := writeCandidate(t, cacheRoot, "1.0.9+codex.latest", "1.0.9+codex.latest", "latest")
	newProduct := writeCandidate(t, cacheRoot, "1.0.10+codex.new", "1.0.10+codex.new", "new")
	oldTime := time.Unix(1, 0)
	latestTime := time.Unix(3, 0)
	if err := os.Chtimes(old.PluginRoot, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(latestSameProduct.PluginRoot, latestTime, latestTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newProduct.PluginRoot, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cacheRoot, "not-a-version"), 0o700); err != nil {
		t.Fatal(err)
	}
	manager := Manager{CodexRoot: filepath.Join(root, "codex")}
	selectedRoot, selectedVersion, err := manager.discoverInstalled()
	if err != nil || selectedRoot != newProduct.PluginRoot || selectedVersion != newProduct.Version {
		t.Fatalf("unexpected installed discovery: root=%q version=%q err=%v", selectedRoot, selectedVersion, err)
	}
	if err := os.RemoveAll(newProduct.PluginRoot); err != nil {
		t.Fatal(err)
	}
	selectedRoot, selectedVersion, err = manager.discoverInstalled()
	if err != nil || selectedRoot != latestSameProduct.PluginRoot || selectedVersion != latestSameProduct.Version {
		t.Fatalf("same-product discovery ignored latest install: root=%q version=%q err=%v", selectedRoot, selectedVersion, err)
	}
}

func TestResolveBoundsRuntimeVersionProbe(t *testing.T) {
	root := t.TempDir()
	fallback := writeCandidate(t, root, "fallback", "1.0.9+codex.old", "old")
	installed := writeCandidate(t, filepath.Join(root, "codex", "plugins", "cache", marketplaceName, pluginName), "1.0.10+codex.new", "1.0.10+codex.new", "new")
	manager := testManager(root, fallback.PluginRoot, installed)
	manager.ProbeTimeout = 10 * time.Millisecond
	manager.ProbeVersion = func(ctx context.Context, _ string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	started := time.Now()
	if _, err := manager.Resolve(context.Background()); err == nil {
		t.Fatal("timed-out runtime probes unexpectedly selected a candidate")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("runtime probe timeout was not bounded: %s", elapsed)
	}
}

func TestResolveSelectsCurrentArchitectureFromSharedSlot(t *testing.T) {
	root := t.TempDir()
	source := writeCandidate(t, filepath.Join(root, "codex", "plugins", "cache", marketplaceName, pluginName), "1.0.9+codex.shared", "1.0.9+codex.shared", "shared")
	linuxManager := testManager(root, source.PluginRoot, source)
	linux, err := linuxManager.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(linux.BinaryPath) != "ocean-watch_linux_amd64" {
		t.Fatalf("Linux runtime=%q", linux.BinaryPath)
	}

	armManager := testManager(root, source.PluginRoot, source)
	armManager.GOOS = "darwin"
	armManager.GOARCH = "arm64"
	arm, err := armManager.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if arm.PluginRoot != linux.PluginRoot || arm.SHA256 != linux.SHA256 ||
		filepath.Base(arm.BinaryPath) != "ocean-watch_darwin_arm64" {
		t.Fatalf("shared slot did not select arm64: linux=%#v arm=%#v", linux, arm)
	}
	state, err := armManager.Status()
	if err != nil || filepath.Base(state.Current.BinaryPath) != "ocean-watch_linux_amd64" {
		t.Fatalf("platform selection rewrote shared identity state: state=%#v err=%v", state, err)
	}
}

func TestInstallCandidateReplacesIncompleteManagedSlot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("in-use runtime slot replacement is Unix-only")
	}
	root := t.TempDir()
	source := writeCandidate(t, root, "source", "1.0.9+codex.new", "new")
	manager := testManager(root, source.PluginRoot, source)
	slotRoot := filepath.Join(manager.CodexRoot, "ocean-watch", "runtime", "versions", source.SHA256)
	if err := os.MkdirAll(filepath.Join(slotRoot, ".codex-plugin", "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{
		".codex-plugin/runtime-manifest.json",
		".codex-plugin/plugin.json",
		".codex-plugin/bin/ocean-watch_linux_amd64",
		"f2/resolve.py",
		"bin/ocean-watch-launcher",
	} {
		payload, err := os.ReadFile(filepath.Join(source.PluginRoot, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		destination := filepath.Join(slotRoot, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	installed, err := manager.installCandidate(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if installed.PluginRoot != slotRoot {
		t.Fatalf("installed slot=%q want=%q", installed.PluginRoot, slotRoot)
	}
	if _, err := os.Stat(filepath.Join(slotRoot, "skills", "qc-plan-monitor", "SKILL.md")); err != nil {
		t.Fatalf("Host resources were not restored: %v", err)
	}
	for _, name := range []string{"ocean-watch_linux_amd64", "ocean-watch_darwin_arm64"} {
		if _, err := os.Stat(filepath.Join(slotRoot, ".codex-plugin", "bin", name)); err != nil {
			t.Fatalf("signed platform runtime was not installed: %s: %v", name, err)
		}
	}
}

func TestInstallCandidatePreservesUnexpectedManagedSlot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("in-use runtime slot replacement is Unix-only")
	}
	root := t.TempDir()
	source := writeCandidate(t, root, "source", "1.0.9+codex.new", "new")
	manager := testManager(root, source.PluginRoot, source)
	slotRoot := filepath.Join(manager.CodexRoot, "ocean-watch", "runtime", "versions", source.SHA256)
	if err := os.MkdirAll(slotRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(slotRoot, "unexpected")
	if err := os.WriteFile(marker, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.installCandidate(context.Background(), source); err == nil {
		t.Fatal("unexpected managed slot was replaced")
	}
	payload, err := os.ReadFile(marker)
	if err != nil || string(payload) != "preserve" {
		t.Fatalf("unexpected managed slot was modified: payload=%q err=%v", payload, err)
	}
}

func testManager(root, fallbackRoot string, installed Candidate) Manager {
	return Manager{
		CodexRoot: filepath.Join(root, "codex"), PluginRoot: fallbackRoot,
		GOOS: "linux", GOARCH: "amd64", Now: func() time.Time { return time.Unix(1, 0) },
		AlwaysDiscover: true,
		Discover: func() (string, string, error) {
			return installed.PluginRoot, installed.Version, nil
		},
		ProbeVersion: func(_ context.Context, path string) ([]byte, error) {
			payload, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(path)), "runtime-manifest.json"))
			if err != nil {
				return nil, err
			}
			var manifest Manifest
			if err := json.Unmarshal(payload, &manifest); err != nil {
				return nil, err
			}
			return []byte("ocean-watch " + strings.SplitN(manifest.Version, "+", 2)[0]), nil
		},
	}
}

func writeCandidate(t *testing.T, root, name, version, body string) Candidate {
	t.Helper()
	pluginRoot := filepath.Join(root, name)
	binaryPath := filepath.Join(pluginRoot, ".codex-plugin", "bin", "ocean-watch_linux_amd64")
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	armBody := []byte(body + "-arm64")
	if err := os.WriteFile(filepath.Join(pluginRoot, ".codex-plugin", "bin", "ocean-watch_darwin_arm64"), armBody, 0o700); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(body))
	hashText := hex.EncodeToString(hash[:])
	pluginPayload := []byte(`{"name":"ocean-watch","version":"` + version + `"}`)
	if err := os.WriteFile(filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"), pluginPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginRoot, "f2"), 0o700); err != nil {
		t.Fatal(err)
	}
	resources := writeTestHostResources(t, pluginRoot)
	manifest := Manifest{
		SchemaVersion: 1, Version: version,
		Plugin:    ManifestFile{Name: pluginName, Version: version, SHA256: hashPayload(pluginPayload)},
		Resources: resources,
		SHA256: map[string]string{
			"ocean-watch_linux_amd64":  hashText,
			"ocean-watch_darwin_arm64": hashPayload(armBody),
		},
	}
	payload, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(pluginRoot, ".codex-plugin", "runtime-manifest.json"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	bundleHash := sha256.Sum256(payload)
	binaryInfo, _ := os.Stat(binaryPath)
	return Candidate{
		Version: version, PluginRoot: pluginRoot, BinaryPath: binaryPath,
		SHA256: hex.EncodeToString(bundleHash[:]), BinarySize: binaryInfo.Size(),
		BinaryModifiedNsec: binaryInfo.ModTime().UnixNano(),
	}
}

func writeTestHostResources(t *testing.T, pluginRoot string) map[string]string {
	t.Helper()
	resources := map[string][]byte{
		".mcp.json":                        []byte(`{"mcpServers":{}}`),
		"f2/resolve.py":                    []byte("# fixture\n"),
		"bin/ocean-watch-launcher":         []byte("launcher"),
		"skills/ads-plan-monitor/SKILL.md": []byte("ads skill"),
		"skills/ads-plan-monitor/run":      []byte("ads run"),
		"skills/qc-plan-monitor/SKILL.md":  []byte("qc skill"),
		"skills/qc-plan-monitor/run":       []byte("qc run"),
	}
	hashes := make(map[string]string, len(resources))
	for relative, payload := range resources {
		path := filepath.Join(pluginRoot, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o600)
		if relative == "bin/ocean-watch-launcher" || strings.HasSuffix(relative, "/run") {
			mode = 0o700
		}
		if err := os.WriteFile(path, payload, mode); err != nil {
			t.Fatal(err)
		}
		hashes[relative] = hashPayload(payload)
	}
	return hashes
}

func TestPreserveDeletedHostRootCreatesAndAdvancesValidatedAlias(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Host aliases are not used on Windows")
	}
	root := t.TempDir()
	cacheRoot := filepath.Join(root, "codex", "plugins", "cache", marketplaceName, pluginName)
	hostRoot := filepath.Join(cacheRoot, "1.0.9+codex.host")
	firstSource := writeCandidate(t, cacheRoot, "1.0.9+codex.first", "1.0.9+codex.first", "first")
	manager := testManager(root, hostRoot, firstSource)
	first, err := manager.installCandidate(context.Background(), firstSource)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.PreserveDeletedHostRoot(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	assertSymlinkTarget(t, hostRoot, first.PluginRoot)

	secondSource := writeCandidate(t, cacheRoot, "1.0.9+codex.second", "1.0.9+codex.second", "second")
	second, err := manager.installCandidate(context.Background(), secondSource)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.PreserveDeletedHostRoot(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	assertSymlinkTarget(t, hostRoot, second.PluginRoot)
}

func TestPreserveDeletedHostRootRefreshesOlderManagedAliases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Host cache aliases are Unix-only")
	}
	root := t.TempDir()
	cacheRoot := filepath.Join(root, "codex", "plugins", "cache", marketplaceName, pluginName)
	hostRoot := filepath.Join(cacheRoot, "1.0.9+codex.host")
	olderRoot := filepath.Join(cacheRoot, "1.0.9+codex.older")
	old := writeCandidate(t, root, "old", "1.0.9+codex.old", "old")
	current := writeCandidate(t, root, "current", "1.0.10+codex.current", "current")
	manager := testManager(root, hostRoot, current)
	oldRuntime, err := manager.installCandidate(context.Background(), old)
	if err != nil {
		t.Fatal(err)
	}
	currentRuntime, err := manager.installCandidate(context.Background(), current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldRuntime.PluginRoot, olderRoot); err != nil {
		t.Fatal(err)
	}
	if err := manager.PreserveDeletedHostRoot(context.Background(), currentRuntime); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(currentRuntime.PluginRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{hostRoot, olderRoot} {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || resolved != want {
			t.Fatalf("Host alias %q resolved to %q: %v", path, resolved, err)
		}
	}
}

func TestPreserveDeletedHostRootNeverOverwritesRealOrForeignPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Host aliases are not used on Windows")
	}
	root := t.TempDir()
	cacheRoot := filepath.Join(root, "codex", "plugins", "cache", marketplaceName, pluginName)
	source := writeCandidate(t, cacheRoot, "1.0.9+codex.source", "1.0.9+codex.source", "source")
	manager := testManager(root, filepath.Join(cacheRoot, "1.0.9+codex.host"), source)
	candidate, err := manager.installCandidate(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(manager.PluginRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := manager.PreserveDeletedHostRoot(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(manager.PluginRoot); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("real Host path was replaced: info=%#v err=%v", info, err)
	}
	if err := os.Remove(manager.PluginRoot); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(root, "foreign")
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, manager.PluginRoot); err != nil {
		t.Fatal(err)
	}
	if err := manager.PreserveDeletedHostRoot(context.Background(), candidate); err == nil {
		t.Fatal("foreign Host alias was accepted")
	}
}

func TestPreserveInstalledHostRootBootstrapsDeletedCachedPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Host aliases are not used on Windows")
	}
	root := t.TempDir()
	cacheRoot := filepath.Join(root, "codex", "plugins", "cache", marketplaceName, pluginName)
	hostRoot := filepath.Join(cacheRoot, "1.0.9+codex.deleted")
	installed := writeCandidate(t, cacheRoot, "1.0.9+codex.installed", "1.0.9+codex.installed", "installed")
	manager := testManager(root, hostRoot, installed)
	manager.Discover = nil
	if err := manager.PreserveInstalledHostRoot(context.Background()); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(hostRoot)
	if err != nil || !strings.Contains(resolved, filepath.Join("ocean-watch", "runtime", "versions")) {
		t.Fatalf("Host alias did not target a private slot: resolved=%q err=%v", resolved, err)
	}

	outside := filepath.Join(root, "missing-workspace")
	manager.PluginRoot = outside
	if err := manager.PreserveInstalledHostRoot(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(outside); !os.IsNotExist(err) {
		t.Fatalf("non-cache path was changed: %v", err)
	}
}

func assertSymlinkTarget(t *testing.T, path, target string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("Host alias is missing: info=%#v err=%v", info, err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	want, wantErr := filepath.EvalSymlinks(target)
	if err != nil || wantErr != nil || filepath.Clean(resolved) != filepath.Clean(want) {
		t.Fatalf("Host alias target=%q want=%q err=%v", resolved, target, err)
	}
}

func hashPayload(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
