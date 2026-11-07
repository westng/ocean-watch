package runtimeupdate

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStableRuntimeCannotDiscoverOrReuseAlpha(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, "codex", "plugins", "cache", marketplaceName, pluginName)
	stable := writeCandidate(t, cache, "1.0.9+codex.stable", "1.0.9+codex.stable", "stable")
	alpha := writeCandidate(t, cache, "2.0.0-alpha.1+codex.alpha", "2.0.0-alpha.1+codex.alpha", "alpha")
	manager := testManager(root, stable.PluginRoot, alpha)
	manager.Discover = nil
	if _, version, err := manager.discoverInstalled(); err != nil || version != stable.Version {
		t.Fatalf("stable discovered alpha: %s %v", version, err)
	}
	manager.Discover = func() (string, string, error) { return alpha.PluginRoot, alpha.Version, nil }
	if _, err := manager.validateCandidate(context.Background(), alpha.PluginRoot, alpha.Version); err == nil {
		t.Fatal("direct discovery bypassed release channel")
	}
	selected, err := manager.Resolve(context.Background())
	if err != nil || selected.Version != stable.Version {
		t.Fatalf("stable did not retain stable fallback: %+v %v", selected, err)
	}
}

func TestExplicitAlphaIsPinnedAndHasSeparateSelectionState(t *testing.T) {
	root := t.TempDir()
	alpha := writeCandidate(t, root, "alpha", "1.1.0-alpha.1+codex.one", "alpha")
	later := writeCandidate(t, root, "later", "1.1.0-alpha.2+codex.two", "later")
	stable := writeCandidate(t, root, "stable", "1.0.9+codex.stable", "stable")
	manager := testManager(root, alpha.PluginRoot, later)
	other := testManager(root, stable.PluginRoot, stable)
	if manager.statePath() == other.statePath() {
		t.Fatal("alpha shared stable selection state")
	}
	selected, err := manager.Resolve(context.Background())
	if err != nil || selected.Version != alpha.Version {
		t.Fatalf("explicit alpha was not pinned: %+v %v", selected, err)
	}
	if _, err := manager.validateCandidate(context.Background(), later.PluginRoot, later.Version); err == nil {
		t.Fatal("alpha silently switched to another preview")
	}
}

func TestAlphaCannotHotSwitchSameVersionBytes(t *testing.T) {
	root := t.TempDir()
	alpha := writeCandidate(t, root, "alpha", "1.1.0-alpha.1+codex.one", "first")
	fixture := testManager(root, alpha.PluginRoot, alpha)
	manager := NewManager(fixture.CodexRoot, alpha.PluginRoot)
	manager.GOOS = fixture.GOOS
	manager.GOARCH = fixture.GOARCH
	manager.ProbeVersion = fixture.ProbeVersion
	first, err := manager.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = writeCandidate(t, root, "alpha", "1.1.0-alpha.1+codex.one", "changed")
	selected, err := manager.Resolve(context.Background())
	if err != nil || selected.SHA256 != first.SHA256 {
		t.Fatalf("alpha hot-switched same version: %+v %v", selected, err)
	}
}
