package runtimeupdate

import (
	"context"
	"errors"
	"os"
)

// Diagnostic inspects bundles without selecting, installing, or changing state.
// Digests distinguish multiple bundles carrying the same version string.
type Diagnostic struct {
	RunningVersion   string   `json:"running_version,omitempty"`
	RunningSHA256    string   `json:"running_manifest_sha256,omitempty"`
	SelectedVersion  string   `json:"selected_version,omitempty"`
	SelectedSHA256   string   `json:"selected_manifest_sha256,omitempty"`
	InstalledVersion string   `json:"installed_version,omitempty"`
	InstalledSHA256  string   `json:"installed_manifest_sha256,omitempty"`
	Issues           []string `json:"issues"`
}

func (manager Manager) Diagnose(ctx context.Context) Diagnostic {
	result := Diagnostic{Issues: []string{}}
	running, err := manager.validateCandidate(ctx, manager.PluginRoot, "")
	if err != nil {
		result.Issues = append(result.Issues, "running_bundle_invalid")
	} else {
		result.RunningVersion, result.RunningSHA256 = running.Version, running.SHA256
	}
	state, stateErr := manager.readState()
	if stateErr == nil {
		result.SelectedVersion, result.SelectedSHA256 = state.Current.Version, state.Current.SHA256
		if err := manager.validateRecorded(ctx, state.Current); err != nil {
			result.Issues = append(result.Issues, "selected_bundle_invalid")
		} else if result.RunningSHA256 != "" && result.RunningSHA256 != state.Current.SHA256 {
			result.Issues = append(result.Issues, "running_bundle_differs_from_selected")
		}
	} else if !errors.Is(stateErr, os.ErrNotExist) {
		result.Issues = append(result.Issues, "runtime_state_invalid")
	}
	root, version, discoverErr := manager.discoverInstalled()
	if discoverErr != nil {
		// Direct local/Claude installations need not have a Codex cache.
		return result
	}
	result.InstalledVersion = version
	result.InstalledSHA256, _ = runtimeManifestHash(root)
	installed, err := manager.validateCandidate(ctx, root, version)
	if err != nil {
		result.Issues = append(result.Issues, "installed_bundle_invalid")
		return result
	}
	if stateErr == nil && installed.SHA256 != state.Current.SHA256 {
		result.Issues = append(result.Issues, "installed_bundle_not_selected")
	}
	return result
}
