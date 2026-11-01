package runtimeupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestHostContractRejectionCannotPoisonAnotherOrLegacyContract(t *testing.T) {
	root := t.TempDir()
	oldSource := writeCandidate(t, root, "old", "1.0.9+codex.old", "old")
	newSource := writeCandidate(t, root, "new", "1.0.10+codex.new", "new")
	legacy := testManager(root, oldSource.PluginRoot, newSource)
	selected, err := legacy.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Reject(context.Background(), selected); err != nil {
		t.Fatal(err)
	}
	legacyBefore, err := os.ReadFile(legacy.statePath())
	if err != nil {
		t.Fatal(err)
	}

	currentHost := legacy.ForHostContract([]byte("new host tool contract"))
	selected, err = currentHost.Resolve(context.Background())
	if err != nil || selected.SHA256 != newSource.SHA256 {
		t.Fatalf("legacy rejection blocked a fresh contract: selected=%#v err=%v", selected, err)
	}
	otherHost := legacy.ForHostContract([]byte("different host tool contract"))
	otherSelected, err := otherHost.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := otherHost.Reject(context.Background(), otherSelected); err != nil {
		t.Fatal(err)
	}

	// Recreating the same contract must preserve quarantine, while another
	// contract remains free to validate this otherwise healthy bundle.
	sameOtherHost := legacy.ForHostContract([]byte("different host tool contract"))
	selected, err = sameOtherHost.Resolve(context.Background())
	if err != nil || selected.SHA256 != oldSource.SHA256 {
		t.Fatalf("same contract retried its rejected bundle: selected=%#v err=%v", selected, err)
	}
	selected, err = currentHost.Resolve(context.Background())
	if err != nil || selected.SHA256 != newSource.SHA256 {
		t.Fatalf("another contract leaked rejection: selected=%#v err=%v", selected, err)
	}
	legacyAfter, err := os.ReadFile(legacy.statePath())
	if err != nil || string(legacyBefore) != string(legacyAfter) {
		t.Fatalf("host contract mutated legacy state: err=%v", err)
	}
	if currentHost.lockPath() != legacy.lockPath() || currentHost.runtimeLeasePath(selected.SHA256) != otherHost.runtimeLeasePath(selected.SHA256) {
		t.Fatal("host contracts stopped sharing installation locks and bundle leases")
	}
}

func TestPruneRetainsIdleRuntimeReferencesAcrossHostContracts(t *testing.T) {
	root := t.TempDir()
	sources := []Candidate{
		writeCandidate(t, root, "legacy", "1.0.9+codex.legacy", "legacy"),
		writeCandidate(t, root, "host-a", "1.0.10+codex.a", "a"),
		writeCandidate(t, root, "host-b", "1.0.11+codex.b", "b"),
		writeCandidate(t, root, "unused", "1.0.12+codex.unused", "unused"),
	}
	managers := []Manager{
		testManager(root, sources[0].PluginRoot, sources[0]),
		testManager(root, sources[1].PluginRoot, sources[1]).ForHostContract([]byte("host a")),
		testManager(root, sources[2].PluginRoot, sources[2]).ForHostContract([]byte("host b")),
	}
	selected := make([]Candidate, len(managers))
	for i, manager := range managers {
		var err error
		selected[i], err = manager.Resolve(context.Background())
		if err != nil {
			t.Fatal(err)
		}
	}
	unused, err := managers[2].installCandidate(context.Background(), sources[3])
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{2, 0} {
		if err := managers[i].PruneObsoleteRuntimes(context.Background(), selected[i]); err != nil {
			t.Fatal(err)
		}
		for _, candidate := range selected {
			if _, err := os.Stat(candidate.PluginRoot); err != nil {
				t.Fatalf("idle current runtime removed by another contract: %v", err)
			}
		}
	}
	if _, err := os.Stat(unused.PluginRoot); !os.IsNotExist(err) {
		t.Fatalf("unreferenced runtime was not pruned: %v", err)
	}
}

func TestPruneStopsBeforeDeletingWhenForeignContractStateIsUnsafe(t *testing.T) {
	for _, kind := range []string{"malformed-state", "symlink-contract"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			source := writeCandidate(t, root, "current", "1.0.9+codex.current", "current")
			obsoleteSource := writeCandidate(t, root, "obsolete", "1.0.8+codex.obsolete", "obsolete")
			manager := testManager(root, source.PluginRoot, source).ForHostContract([]byte("current host"))
			current, err := manager.Resolve(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			obsolete, err := manager.installCandidate(context.Background(), obsoleteSource)
			if err != nil {
				t.Fatal(err)
			}
			foreign := manager.ForHostContract([]byte("foreign host"))
			if kind == "malformed-state" {
				if err := os.MkdirAll(filepath.Dir(foreign.statePath()), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(foreign.statePath(), []byte("invalid json"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Symlink(t.TempDir(), filepath.Dir(foreign.statePath())); err != nil {
					t.Fatal(err)
				}
			}
			if err := manager.PruneObsoleteRuntimes(context.Background(), current); err == nil {
				t.Fatal("cleanup ignored an unsafe foreign contract state")
			}
			if _, err := os.Stat(obsolete.PluginRoot); err != nil {
				t.Fatalf("cleanup deleted a slot before verifying all references: %v", err)
			}
		})
	}
}

func TestHostContractWithNoFallbackDoesNotRetryItsRejectedBundle(t *testing.T) {
	root := t.TempDir()
	source := writeCandidate(t, root, "only", "1.0.9+codex.only", "only")
	manager := testManager(root, source.PluginRoot, source).ForHostContract([]byte("same contract"))
	selected, err := manager.Resolve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Reject(context.Background(), selected); !errors.Is(err, ErrNoPreviousRuntime) {
		t.Fatalf("unexpected quarantine result: %v", err)
	}
	if _, err := manager.Resolve(context.Background()); !errors.Is(err, ErrNoUsableRuntime) {
		t.Fatalf("quarantined bundle was retried: %v", err)
	}
}
