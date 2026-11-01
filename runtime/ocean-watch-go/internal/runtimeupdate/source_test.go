package runtimeupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckoutUsesOwnF2BundleWithoutChangingInstalledRuntime(t *testing.T) {
	root := t.TempDir()
	codexRoot := filepath.Join(root, "codex")
	version := "1.0.9+codex.fixture"
	cacheRoot := filepath.Join(codexRoot, "plugins", "cache", marketplaceName, pluginName)
	cached := writeSourceF2(t, writeCandidate(t, cacheRoot, version, version, "installed"), "# installed wrapper\n")
	installedManager := configureSourceManager(NewManager(codexRoot, cached.PluginRoot))
	if _, err := installedManager.Resolve(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(installedManager.statePath())
	if err != nil {
		t.Fatal(err)
	}

	checkout := writeSourceF2(t, writeCandidate(t, root, "checkout", version, "local"), "# local wrapper with x-tt-argus\n")
	if err := os.Mkdir(filepath.Join(checkout.PluginRoot, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	localManager := configureSourceManager(NewManager(codexRoot, checkout.PluginRoot))
	selected, err := localManager.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if selected.SHA256 != checkout.SHA256 {
		t.Fatalf("checkout launch selected the installed bundle: selected=%s want=%s", selected.SHA256, checkout.SHA256)
	}
	content, err := os.ReadFile(filepath.Join(selected.PluginRoot, "f2", "resolve.py"))
	if err != nil || string(content) != "# local wrapper with x-tt-argus\n" {
		t.Fatalf("checkout F2 wrapper was not selected: %q %v", content, err)
	}
	after, err := os.ReadFile(installedManager.statePath())
	if err != nil || string(before) != string(after) {
		t.Fatalf("checkout changed the installed runtime state: %v", err)
	}

	updated := writeSourceF2(t, writeCandidate(t, root, "checkout", version, "local-updated"), "# updated local wrapper\n")
	selected, err = localManager.Resolve(context.Background())
	if err != nil || selected.SHA256 != updated.SHA256 {
		t.Fatalf("same-version checkout update was ignored: selected=%s want=%s err=%v", selected.SHA256, updated.SHA256, err)
	}
	managedManager := configureSourceManager(NewManager(codexRoot, selected.PluginRoot))
	if managedManager.CodexRoot != localManager.CodexRoot {
		t.Fatal("managed checkout subprocess lost its runtime scope")
	}
	if diagnostic := managedManager.Diagnose(context.Background()); len(diagnostic.Issues) != 0 {
		t.Fatalf("valid checkout subprocess reported runtime drift: %#v", diagnostic)
	}
	after, err = os.ReadFile(installedManager.statePath())
	if err != nil || string(before) != string(after) {
		t.Fatalf("checkout update or diagnosis changed the installed runtime state: %v", err)
	}
}

func TestInvalidCheckoutCannotFallBackToInstalledBundle(t *testing.T) {
	root := t.TempDir()
	codexRoot := filepath.Join(root, "codex")
	version := "1.0.9+codex.fixture"
	cacheRoot := filepath.Join(codexRoot, "plugins", "cache", marketplaceName, pluginName)
	cached := writeCandidate(t, cacheRoot, version, version, "installed")
	installedManager := configureSourceManager(NewManager(codexRoot, cached.PluginRoot))
	if _, err := installedManager.Resolve(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(installedManager.statePath())
	if err != nil {
		t.Fatal(err)
	}
	checkout := writeCandidate(t, root, "checkout", version, "local")
	if err := os.Mkdir(filepath.Join(checkout.PluginRoot, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout.PluginRoot, "f2", "resolve.py"), []byte("unverified changes"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := configureSourceManager(NewManager(codexRoot, checkout.PluginRoot))
	if _, err := manager.Resolve(context.Background()); err == nil {
		t.Fatal("invalid local resources fell back to an installed bundle")
	}
	after, err := os.ReadFile(installedManager.statePath())
	if err != nil || string(before) != string(after) {
		t.Fatalf("invalid checkout changed the installed runtime state: %v", err)
	}
}

func TestInstalledGitCacheStillSelectsCompatibleUpdates(t *testing.T) {
	root := t.TempDir()
	codexRoot := filepath.Join(root, "codex")
	cacheRoot := filepath.Join(codexRoot, "plugins", "cache", marketplaceName, pluginName)
	old := writeCandidate(t, cacheRoot, "1.0.9+codex.old", "1.0.9+codex.old", "old")
	newer := writeCandidate(t, cacheRoot, "1.0.10+codex.new", "1.0.10+codex.new", "new")
	if err := os.Mkdir(filepath.Join(old.PluginRoot, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	manager := configureSourceManager(NewManager(codexRoot, old.PluginRoot))
	selected, err := manager.Resolve(context.Background())
	if err != nil || selected.SHA256 != newer.SHA256 {
		t.Fatalf("installed cache was treated as a checkout: selected=%s want=%s err=%v", selected.SHA256, newer.SHA256, err)
	}
	if manager.CodexRoot != codexRoot || manager.LocalSource {
		t.Fatal("installed cache stopped using the host runtime scope")
	}
}

func TestWorktreesKeepIndependentLocalRuntimeSelections(t *testing.T) {
	root := t.TempDir()
	codexRoot := filepath.Join(root, "codex")
	version := "1.0.9+codex.fixture"
	first := writeCandidate(t, root, "first", version, "first")
	second := writeCandidate(t, root, "second", version, "second")
	for _, source := range []Candidate{first, second} {
		if err := os.WriteFile(filepath.Join(source.PluginRoot, ".git"), []byte("gitdir: fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	firstManager := configureSourceManager(NewManager(codexRoot, first.PluginRoot))
	secondManager := configureSourceManager(NewManager(codexRoot, second.PluginRoot))
	for _, run := range []struct {
		manager Manager
		source  Candidate
	}{{firstManager, first}, {secondManager, second}} {
		selected, err := run.manager.Resolve(context.Background())
		if err != nil || selected.SHA256 != run.source.SHA256 {
			t.Fatalf("worktree used another source: selected=%s want=%s err=%v", selected.SHA256, run.source.SHA256, err)
		}
	}
	if firstManager.CodexRoot == secondManager.CodexRoot {
		t.Fatal("different worktrees share a runtime selection")
	}
	if _, err := (Manager{CodexRoot: codexRoot}).Status(); !os.IsNotExist(err) {
		t.Fatalf("worktree launches created a host runtime selection: %v", err)
	}
}

func TestCheckoutCanRunItsExplicitOlderBuildWithoutDowngradingHost(t *testing.T) {
	root := t.TempDir()
	codexRoot := filepath.Join(root, "codex")
	cacheRoot := filepath.Join(codexRoot, "plugins", "cache", marketplaceName, pluginName)
	cached := writeCandidate(t, cacheRoot, "1.0.10+codex.host", "1.0.10+codex.host", "host")
	installedManager := configureSourceManager(NewManager(codexRoot, cached.PluginRoot))
	if _, err := installedManager.Resolve(context.Background()); err != nil {
		t.Fatal(err)
	}
	checkout := writeCandidate(t, root, "checkout", "1.0.9+codex.local", "local")
	if err := os.Mkdir(filepath.Join(checkout.PluginRoot, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	manager := configureSourceManager(NewManager(codexRoot, checkout.PluginRoot))
	if _, err := manager.Resolve(context.Background()); err != nil {
		t.Fatal(err)
	}
	older := writeCandidate(t, root, "checkout", "1.0.8+codex.branch", "older-branch")
	selected, err := manager.Resolve(context.Background())
	if err != nil || selected.SHA256 != older.SHA256 {
		t.Fatalf("explicit checkout build was ignored: selected=%s want=%s err=%v", selected.SHA256, older.SHA256, err)
	}
	state, err := installedManager.Status()
	if err != nil || state.Current.SHA256 != cached.SHA256 {
		t.Fatalf("checkout downgraded the installed host: %#v %v", state, err)
	}
}

func TestCheckoutStillRequiresAStateHome(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager("", root).Resolve(context.Background()); err == nil {
		t.Fatal("checkout resolved without a state home")
	}
}

func configureSourceManager(manager Manager) Manager {
	manager.GOOS, manager.GOARCH = "linux", "amd64"
	manager.AlwaysDiscover = true
	manager.ProbeVersion = func(_ context.Context, path string) ([]byte, error) {
		payload, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(path)), "runtime-manifest.json"))
		if err != nil {
			return nil, err
		}
		var manifest Manifest
		if err := json.Unmarshal(payload, &manifest); err != nil {
			return nil, err
		}
		return []byte("ocean-watch " + strings.SplitN(manifest.Version, "+", 2)[0]), nil
	}
	return manager
}

func writeSourceF2(t *testing.T, candidate Candidate, source string) Candidate {
	t.Helper()
	if err := os.WriteFile(filepath.Join(candidate.PluginRoot, "f2", "resolve.py"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(candidate.PluginRoot, ".codex-plugin", "runtime-manifest.json")
	payload, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Resources["f2/resolve.py"] = hashPayload([]byte(source))
	payload, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	candidate.SHA256 = hex.EncodeToString(digest[:])
	return candidate
}
