package runtimeupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
)

const (
	pluginName      = "ocean-watch"
	marketplaceName = "ocean-watch"
)

var requiredResources = []string{
	".mcp.json",
	"bin/ocean-watch-launcher",
	"f2/resolve.py",
	"skills/ads-plan-monitor/SKILL.md",
	"skills/ads-plan-monitor/run",
	"skills/qc-plan-monitor/SKILL.md",
	"skills/qc-plan-monitor/run",
}

type Manifest struct {
	SchemaVersion int               `json:"schema_version"`
	Version       string            `json:"version"`
	Plugin        ManifestFile      `json:"plugin"`
	Resources     map[string]string `json:"resources"`
	SHA256        map[string]string `json:"sha256"`
}

type ManifestFile struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type Candidate struct {
	Version            string `json:"version"`
	PluginRoot         string `json:"plugin_root"`
	BinaryPath         string `json:"binary_path"`
	SHA256             string `json:"sha256"`
	BinarySize         int64  `json:"binary_size"`
	BinaryModifiedNsec int64  `json:"binary_modified_nsec"`
}

type State struct {
	SchemaVersion  int        `json:"schema_version"`
	Current        Candidate  `json:"current"`
	Previous       *Candidate `json:"previous,omitempty"`
	RejectedSHA256 string     `json:"rejected_sha256,omitempty"`
	UpdatedAt      string     `json:"updated_at"`
}

type Manager struct {
	CodexRoot      string
	PluginRoot     string
	GOOS           string
	GOARCH         string
	Now            func() time.Time
	Discover       func() (string, string, error)
	ProbeVersion   func(context.Context, string) ([]byte, error)
	ProbeTimeout   time.Duration
	AlwaysDiscover bool
}

func (manager Manager) Resolve(ctx context.Context) (Candidate, error) {
	if strings.TrimSpace(manager.CodexRoot) == "" {
		return Candidate{}, errors.New("Codex root is required")
	}
	lock, err := filesystem.AcquireLock(ctx, manager.lockPath(), 10*time.Second)
	if err != nil {
		return Candidate{}, err
	}
	defer lock.Release()
	return manager.resolveLocked(ctx)
}

func (manager Manager) ResolveLeased(ctx context.Context) (Candidate, *filesystem.FileLock, error) {
	if strings.TrimSpace(manager.CodexRoot) == "" {
		return Candidate{}, nil, errors.New("Codex root is required")
	}
	lock, err := filesystem.AcquireLock(ctx, manager.lockPath(), 10*time.Second)
	if err != nil {
		return Candidate{}, nil, err
	}
	defer lock.Release()
	candidate, err := manager.resolveLocked(ctx)
	if err != nil {
		return Candidate{}, nil, err
	}
	lease, err := filesystem.AcquireSharedLock(ctx, manager.runtimeLeasePath(candidate.SHA256), 10*time.Second)
	if err != nil {
		return Candidate{}, nil, err
	}
	return candidate, lease, nil
}

func (manager Manager) resolveLocked(ctx context.Context) (Candidate, error) {
	state, _ := manager.readState()
	if !manager.AlwaysDiscover && manager.canReuseLocalState(state) {
		return manager.selectRecordedCandidate(state.Current)
	}
	installedRoot, installedVersion, discoveryErr := manager.discoverInstalled()
	if manager.canReuseState(state, installedVersion, discoveryErr) {
		return manager.selectRecordedCandidate(state.Current)
	}
	fallbackSource, fallbackErr := manager.validateCandidate(ctx, manager.PluginRoot, "")
	installedSource, installedErr := manager.validateCandidate(ctx, installedRoot, installedVersion)
	fallback, fallbackInstallErr := manager.installCandidate(ctx, fallbackSource)
	installed, installedInstallErr := manager.installCandidate(ctx, installedSource)
	if fallbackErr == nil {
		fallbackErr = fallbackInstallErr
	}
	if installedErr == nil {
		installedErr = installedInstallErr
	}

	recordedCurrent, recordedCurrentErr := manager.validateRecordedCandidate(ctx, state.Current)
	selected := Candidate{}
	switch {
	case installedErr == nil && installed.SHA256 != state.RejectedSHA256 && manager.acceptInstalled(ctx, state.Current, installed):
		selected = installed
	case recordedCurrentErr == nil:
		selected = recordedCurrent
	case installedErr == nil:
		selected = installed
	case fallbackErr == nil:
		selected = fallback
	default:
		return Candidate{}, errors.Join(discoveryErr, installedErr, fallbackErr)
	}

	if !sameCandidateIdentity(selected, state.Current) {
		next := State{
			SchemaVersion: 2, Current: selected,
			RejectedSHA256: state.RejectedSHA256, UpdatedAt: manager.now().UTC().Format(time.RFC3339),
		}
		if selected.SHA256 != state.Current.SHA256 && selected.SHA256 != state.RejectedSHA256 {
			next.RejectedSHA256 = ""
		}
		if recordedCurrentErr == nil && !sameCandidateIdentity(recordedCurrent, selected) {
			previous := recordedCurrent
			next.Previous = &previous
		} else if fallbackErr == nil && !sameCandidateIdentity(fallback, selected) {
			previous := fallback
			next.Previous = &previous
		} else if state.Previous != nil && manager.validateRecorded(ctx, *state.Previous) == nil {
			next.Previous = state.Previous
		}
		if err := manager.writeState(next); err != nil {
			return Candidate{}, err
		}
	}
	return selected, nil
}

func (manager Manager) canReuseLocalState(state State) bool {
	if state.SchemaVersion != 2 || state.Current.Version == "" || !manager.isRuntimeSlot(state.Current) ||
		manager.quickValidateRecorded(state.Current) != nil {
		return false
	}
	payload, err := readRegularFile(filepath.Join(manager.PluginRoot, ".codex-plugin", "runtime-manifest.json"))
	if err != nil {
		return false
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]) == state.Current.SHA256
}

func (manager Manager) canReuseState(state State, installedVersion string, discoveryErr error) bool {
	if state.SchemaVersion != 2 || state.Current.Version == "" || !manager.isRuntimeSlot(state.Current) ||
		manager.quickValidateRecorded(state.Current) != nil {
		return false
	}
	return discoveryErr == nil && installedVersion == state.Current.Version
}

func (manager Manager) isRuntimeSlot(candidate Candidate) bool {
	versionsRoot := filepath.Join(manager.CodexRoot, "ocean-watch", "runtime", "versions")
	relative, err := filepath.Rel(versionsRoot, candidate.PluginRoot)
	return err == nil && relative != "." && relative != "" &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator)) && relative != ".." &&
		filepath.Base(candidate.PluginRoot) == candidate.SHA256
}

func (manager Manager) quickValidateRecorded(candidate Candidate) error {
	if candidate.BinaryPath == "" || candidate.PluginRoot == "" || candidate.SHA256 == "" {
		return errors.New("recorded Ocean Watch runtime is empty")
	}
	if filepath.Dir(filepath.Dir(candidate.BinaryPath)) != filepath.Join(candidate.PluginRoot, ".codex-plugin") {
		return errors.New("recorded Ocean Watch runtime path is invalid")
	}
	for _, path := range []string{
		candidate.BinaryPath,
		filepath.Join(candidate.PluginRoot, ".codex-plugin", "runtime-manifest.json"),
		filepath.Join(candidate.PluginRoot, ".codex-plugin", "plugin.json"),
	} {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("recorded Ocean Watch runtime file is invalid")
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o222 != 0 {
			return errors.New("recorded Ocean Watch runtime slot is mutable")
		}
	}
	binaryInfo, _ := os.Lstat(candidate.BinaryPath)
	if binaryInfo == nil || binaryInfo.Size() != candidate.BinarySize ||
		binaryInfo.ModTime().UnixNano() != candidate.BinaryModifiedNsec {
		return errors.New("recorded Ocean Watch runtime binary identity changed")
	}
	manifestBytes, err := readRegularFile(filepath.Join(candidate.PluginRoot, ".codex-plugin", "runtime-manifest.json"))
	if err != nil {
		return err
	}
	manifestHash := sha256.Sum256(manifestBytes)
	if hex.EncodeToString(manifestHash[:]) != candidate.SHA256 {
		return errors.New("recorded Ocean Watch runtime manifest identity changed")
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(manifestBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil || manifest.SchemaVersion != 1 ||
		manifest.Version != candidate.Version || manifest.Plugin.Name != pluginName ||
		manifest.Plugin.Version != candidate.Version {
		return errors.New("recorded Ocean Watch runtime manifest is invalid")
	}
	resources := map[string]string{".codex-plugin/plugin.json": manifest.Plugin.SHA256}
	for path, expected := range manifest.Resources {
		if !safeResourcePath(path) {
			return fmt.Errorf("recorded Ocean Watch runtime resource path is invalid: %s", path)
		}
		resources[path] = expected
	}
	for _, required := range requiredResources {
		if manifest.Resources[required] == "" {
			return fmt.Errorf("recorded Ocean Watch runtime resource is missing: %s", required)
		}
	}
	for path, expected := range resources {
		payload, err := readRegularFile(filepath.Join(candidate.PluginRoot, filepath.FromSlash(path)))
		if err != nil || !matchesHash(payload, expected) {
			return fmt.Errorf("recorded Ocean Watch runtime resource changed: %s", path)
		}
	}
	return nil
}

func (manager Manager) selectRecordedCandidate(candidate Candidate) (Candidate, error) {
	if err := manager.quickValidateRecorded(candidate); err != nil {
		return Candidate{}, err
	}
	name, err := manager.binaryName()
	if err != nil {
		return Candidate{}, err
	}
	if filepath.Base(candidate.BinaryPath) == name {
		return candidate, nil
	}
	manifestBytes, err := readRegularFile(filepath.Join(candidate.PluginRoot, ".codex-plugin", "runtime-manifest.json"))
	if err != nil {
		return Candidate{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return Candidate{}, err
	}
	binaryPath := filepath.Join(candidate.PluginRoot, ".codex-plugin", "bin", name)
	binaryBytes, err := readRegularFile(binaryPath)
	if err != nil || !matchesHash(binaryBytes, manifest.SHA256[name]) {
		return Candidate{}, errors.New("recorded Ocean Watch runtime platform binary changed")
	}
	binaryInfo, err := os.Lstat(binaryPath)
	if err != nil {
		return Candidate{}, err
	}
	candidate.BinaryPath = binaryPath
	candidate.BinarySize = binaryInfo.Size()
	candidate.BinaryModifiedNsec = binaryInfo.ModTime().UnixNano()
	return candidate, nil
}

func (manager Manager) Reject(ctx context.Context, rejected Candidate) (Candidate, error) {
	lock, err := filesystem.AcquireLock(ctx, manager.lockPath(), 10*time.Second)
	if err != nil {
		return Candidate{}, err
	}
	defer lock.Release()
	state, err := manager.readState()
	if err != nil {
		return Candidate{}, err
	}
	current, currentErr := manager.validateRecordedCandidate(ctx, state.Current)
	if !sameCandidateIdentity(state.Current, rejected) {
		if currentErr != nil {
			return state.Current, nil
		}
		return current, nil
	}
	if state.Previous == nil {
		return Candidate{}, errors.New("no previous Ocean Watch runtime is available")
	}
	previous, err := manager.validateRecordedCandidate(ctx, *state.Previous)
	if err != nil {
		return Candidate{}, fmt.Errorf("previous Ocean Watch runtime is invalid: %w", err)
	}
	state.Current = previous
	state.Previous = nil
	state.RejectedSHA256 = rejected.SHA256
	state.UpdatedAt = manager.now().UTC().Format(time.RFC3339)
	if err := manager.writeState(state); err != nil {
		return Candidate{}, err
	}
	return previous, nil
}

func (manager Manager) Status() (State, error) {
	return manager.readState()
}

func (manager Manager) AcquireRuntimeLease(ctx context.Context, candidate Candidate) (*filesystem.FileLock, error) {
	if !manager.isRuntimeSlot(candidate) {
		return nil, errors.New("Ocean Watch runtime lease target is invalid")
	}
	return filesystem.AcquireSharedLock(ctx, manager.runtimeLeasePath(candidate.SHA256), 10*time.Second)
}

func (manager Manager) PruneObsoleteRuntimes(ctx context.Context, keep Candidate) error {
	if !manager.isRuntimeSlot(keep) {
		return errors.New("Ocean Watch runtime cleanup target is invalid")
	}
	lock, err := filesystem.AcquireLock(ctx, manager.lockPath(), 10*time.Second)
	if err != nil {
		return err
	}
	defer lock.Release()
	state, stateErr := manager.readState()
	if stateErr == nil && manager.isRuntimeSlot(state.Current) {
		keep = state.Current
	}
	versionsRoot := filepath.Join(manager.CodexRoot, "ocean-watch", "runtime", "versions")
	entries, err := os.ReadDir(versionsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read Ocean Watch runtime slots: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || entry.Name() == keep.SHA256 || !validSHA256(entry.Name()) {
			continue
		}
		lease, acquired, acquireErr := filesystem.TryAcquireLock(manager.runtimeLeasePath(entry.Name()))
		if acquireErr != nil {
			return acquireErr
		}
		if !acquired {
			continue
		}
		removeErr := os.RemoveAll(filepath.Join(versionsRoot, entry.Name()))
		releaseErr := lease.Release()
		if removeErr != nil || releaseErr != nil {
			return errors.Join(removeErr, releaseErr)
		}
	}
	if stateErr == nil && sameCandidateIdentity(state.Current, keep) && state.Previous != nil {
		state.Previous = nil
		state.UpdatedAt = manager.now().UTC().Format(time.RFC3339)
		if err := manager.writeState(state); err != nil {
			return err
		}
	}
	return nil
}

func (manager Manager) InstalledCacheRoot() string {
	return filepath.Join(manager.CodexRoot, "plugins", "cache", marketplaceName, pluginName)
}

func (manager Manager) PreserveDeletedHostRoot(ctx context.Context, candidate Candidate) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	hostRoot := filepath.Clean(strings.TrimSpace(manager.PluginRoot))
	cacheRoot := filepath.Clean(manager.InstalledCacheRoot())
	if !filepath.IsAbs(hostRoot) || filepath.Dir(hostRoot) != cacheRoot {
		return nil
	}
	if _, err := productVersion(filepath.Base(hostRoot)); err != nil {
		return errors.New("Ocean Watch Host cache path has an invalid version")
	}
	if err := manager.validateAliasTarget(ctx, candidate); err != nil {
		return fmt.Errorf("validate Ocean Watch Host alias target: %w", err)
	}
	if err := manager.preserveHostAlias(hostRoot, candidate); err != nil {
		return err
	}
	return manager.refreshManagedHostAliases(candidate)
}

func (manager Manager) preserveHostAlias(hostRoot string, candidate Candidate) error {
	if info, err := os.Lstat(hostRoot); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		resolved, resolveErr := managedSymlinkTarget(hostRoot)
		candidateRoot, candidateErr := filepath.EvalSymlinks(candidate.PluginRoot)
		if resolveErr == nil && candidateErr == nil && filepath.Clean(resolved) == filepath.Clean(candidateRoot) {
			return nil
		}
		if resolveErr != nil || !manager.isAllowedAliasTarget(resolved) {
			return errors.New("Ocean Watch Host cache alias already points elsewhere")
		}
		temporary := hostRoot + ".next"
		_ = os.Remove(temporary)
		if err := os.Symlink(candidate.PluginRoot, temporary); err != nil {
			return fmt.Errorf("stage Ocean Watch Host cache alias: %w", err)
		}
		if err := os.Rename(temporary, hostRoot); err != nil {
			_ = os.Remove(temporary)
			return fmt.Errorf("replace Ocean Watch Host cache alias: %w", err)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect Ocean Watch Host cache path: %w", err)
	}
	if err := os.Symlink(candidate.PluginRoot, hostRoot); err != nil {
		if info, statErr := os.Lstat(hostRoot); statErr == nil && info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		return fmt.Errorf("preserve Ocean Watch Host cache path: %w", err)
	}
	return nil
}

func (manager Manager) refreshManagedHostAliases(candidate Candidate) error {
	cacheRoot := manager.InstalledCacheRoot()
	entries, err := os.ReadDir(cacheRoot)
	if err != nil {
		return fmt.Errorf("read Ocean Watch Host cache aliases: %w", err)
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		if _, err := productVersion(entry.Name()); err != nil {
			continue
		}
		path := filepath.Join(cacheRoot, entry.Name())
		resolved, resolveErr := managedSymlinkTarget(path)
		if resolveErr != nil || !manager.isAllowedAliasTarget(resolved) {
			return errors.New("Ocean Watch Host cache alias points outside managed roots")
		}
		if err := manager.preserveHostAlias(path, candidate); err != nil {
			return err
		}
	}
	return nil
}

func managedSymlinkTarget(path string) (string, error) {
	target, err := os.Readlink(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(target); resolveErr == nil {
		return filepath.Clean(resolved), nil
	}
	return filepath.Clean(target), nil
}

func (manager Manager) PreserveInstalledHostRoot(ctx context.Context) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	hostRoot := filepath.Clean(strings.TrimSpace(manager.PluginRoot))
	if !filepath.IsAbs(hostRoot) || filepath.Dir(hostRoot) != filepath.Clean(manager.InstalledCacheRoot()) {
		return nil
	}
	if info, err := os.Lstat(hostRoot); err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	lock, err := filesystem.AcquireLock(ctx, manager.lockPath(), 10*time.Second)
	if err != nil {
		return err
	}
	defer lock.Release()
	installedRoot, installedVersion, err := manager.discoverInstalled()
	if err != nil {
		return err
	}
	candidate, err := manager.validateCandidate(ctx, installedRoot, installedVersion)
	if err != nil {
		return err
	}
	installed, err := manager.installCandidate(ctx, candidate)
	if err != nil {
		return err
	}
	return manager.PreserveDeletedHostRoot(ctx, installed)
}

func (manager Manager) validateAliasTarget(ctx context.Context, candidate Candidate) error {
	if manager.isRuntimeSlot(candidate) {
		return manager.validateRecorded(ctx, candidate)
	}
	validated, err := manager.validateCandidate(ctx, candidate.PluginRoot, candidate.Version)
	if err != nil {
		return err
	}
	if validated.SHA256 != candidate.SHA256 || validated.BinaryPath != candidate.BinaryPath {
		return errors.New("Ocean Watch Host alias target identity changed")
	}
	return nil
}

func (manager Manager) isAllowedAliasTarget(path string) bool {
	for _, root := range []string{
		filepath.Join(manager.CodexRoot, "ocean-watch", "runtime", "versions"),
		manager.InstalledCacheRoot(),
	} {
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			resolvedRoot = root
		}
		relative, err := filepath.Rel(resolvedRoot, path)
		if err == nil && relative != "." && relative != ".." &&
			!strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (manager Manager) discoverInstalled() (string, string, error) {
	if manager.Discover != nil {
		return manager.Discover()
	}
	cacheRoot := manager.InstalledCacheRoot()
	entries, err := os.ReadDir(cacheRoot)
	if err != nil {
		return "", "", fmt.Errorf("read installed Ocean Watch versions: %w", err)
	}
	selectedRoot := ""
	selectedVersion := ""
	selectedModified := time.Time{}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		version := entry.Name()
		if _, err := productVersion(version); err != nil {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		comparison := 1
		if selectedVersion != "" {
			comparison, err = compareProductVersions(version, selectedVersion)
			if err != nil {
				continue
			}
		}
		if selectedVersion == "" || comparison > 0 ||
			comparison == 0 && (info.ModTime().After(selectedModified) ||
				info.ModTime().Equal(selectedModified) && version > selectedVersion) {
			selectedRoot = filepath.Join(cacheRoot, version)
			selectedVersion = version
			selectedModified = info.ModTime()
		}
	}
	if selectedVersion == "" {
		return "", "", errors.New("installed Ocean Watch plugin version was not found")
	}
	return selectedRoot, selectedVersion, nil
}

func (manager Manager) validateCandidate(ctx context.Context, root, expectedVersion string) (Candidate, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || root == string(filepath.Separator) {
		return Candidate{}, errors.New("Ocean Watch plugin root is invalid")
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return Candidate{}, errors.New("Ocean Watch plugin root must be a real directory")
	}
	manifestPath := filepath.Join(root, ".codex-plugin", "runtime-manifest.json")
	manifestBytes, err := readRegularFile(manifestPath)
	if err != nil {
		return Candidate{}, fmt.Errorf("read Ocean Watch runtime manifest: %w", err)
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(manifestBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Candidate{}, fmt.Errorf("decode Ocean Watch runtime manifest: %w", err)
	}
	if manifest.Version == "" || expectedVersion != "" && manifest.Version != expectedVersion {
		return Candidate{}, errors.New("Ocean Watch runtime version does not match the installed plugin")
	}
	if manifest.SchemaVersion != 1 || manifest.Plugin.Name != pluginName ||
		manifest.Plugin.Version != manifest.Version {
		return Candidate{}, errors.New("Ocean Watch runtime manifest identity is invalid")
	}
	pluginPath := filepath.Join(root, ".codex-plugin", "plugin.json")
	pluginBytes, err := readRegularFile(pluginPath)
	if err != nil {
		return Candidate{}, fmt.Errorf("read Ocean Watch plugin manifest: %w", err)
	}
	if !matchesHash(pluginBytes, manifest.Plugin.SHA256) {
		return Candidate{}, errors.New("Ocean Watch plugin manifest hash does not match its runtime manifest")
	}
	var plugin struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(pluginBytes, &plugin); err != nil || plugin.Name != pluginName || plugin.Version != manifest.Version {
		return Candidate{}, errors.New("Ocean Watch plugin identity does not match its runtime manifest")
	}
	for _, required := range requiredResources {
		if manifest.Resources[required] == "" {
			return Candidate{}, fmt.Errorf("Ocean Watch runtime manifest is missing required resource %s", required)
		}
	}
	for resourcePath, expectedHash := range manifest.Resources {
		if !safeResourcePath(resourcePath) {
			return Candidate{}, fmt.Errorf("Ocean Watch runtime resource path is invalid: %s", resourcePath)
		}
		resourceBytes, err := readRegularFile(filepath.Join(root, filepath.FromSlash(resourcePath)))
		if err != nil {
			return Candidate{}, fmt.Errorf("read Ocean Watch runtime resource %s: %w", resourcePath, err)
		}
		if !matchesHash(resourceBytes, expectedHash) {
			return Candidate{}, fmt.Errorf("Ocean Watch runtime resource %s hash does not match its manifest", resourcePath)
		}
	}
	binaryNames := make([]string, 0, len(manifest.SHA256))
	for name := range manifest.SHA256 {
		if filepath.Base(name) != name || name == "." || name == ".." {
			return Candidate{}, errors.New("Ocean Watch runtime manifest binary name is invalid")
		}
		binaryNames = append(binaryNames, name)
	}
	sort.Strings(binaryNames)
	for _, name := range binaryNames {
		wantHash := strings.ToLower(strings.TrimSpace(manifest.SHA256[name]))
		if len(wantHash) != sha256.Size*2 {
			return Candidate{}, errors.New("Ocean Watch runtime manifest has an invalid platform hash")
		}
		binaryBytes, readErr := readRegularFile(filepath.Join(root, ".codex-plugin", "bin", name))
		if readErr != nil {
			return Candidate{}, fmt.Errorf("read Ocean Watch runtime %s: %w", name, readErr)
		}
		actualHash := sha256.Sum256(binaryBytes)
		if hex.EncodeToString(actualHash[:]) != wantHash {
			return Candidate{}, fmt.Errorf("Ocean Watch runtime hash does not match its manifest: %s", name)
		}
	}
	name, err := manager.binaryName()
	if err != nil {
		return Candidate{}, err
	}
	if manifest.SHA256[name] == "" {
		return Candidate{}, errors.New("Ocean Watch runtime manifest has no platform hash")
	}
	binaryPath := filepath.Join(root, ".codex-plugin", "bin", name)
	probe := manager.ProbeVersion
	if probe == nil {
		probe = func(ctx context.Context, path string) ([]byte, error) {
			return exec.CommandContext(ctx, path, "--version").Output()
		}
	}
	probeTimeout := manager.ProbeTimeout
	if probeTimeout <= 0 {
		probeTimeout = 2 * time.Second
	}
	probeContext, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	output, err := probe(probeContext, binaryPath)
	if err != nil || strings.TrimSpace(string(output)) != "ocean-watch "+strings.SplitN(manifest.Version, "+", 2)[0] {
		return Candidate{}, errors.New("Ocean Watch runtime self-reported version does not match its manifest")
	}
	bundleHash := sha256.Sum256(manifestBytes)
	binaryInfo, err := os.Stat(binaryPath)
	if err != nil {
		return Candidate{}, err
	}
	return Candidate{
		Version: manifest.Version, PluginRoot: root, BinaryPath: binaryPath,
		SHA256: hex.EncodeToString(bundleHash[:]), BinarySize: binaryInfo.Size(),
		BinaryModifiedNsec: binaryInfo.ModTime().UnixNano(),
	}, nil
}

func (manager Manager) installCandidate(ctx context.Context, source Candidate) (Candidate, error) {
	if source.BinaryPath == "" {
		return Candidate{}, errors.New("Ocean Watch runtime source is empty")
	}
	slotRoot := filepath.Join(manager.CodexRoot, "ocean-watch", "runtime", "versions", source.SHA256)
	binaryName := filepath.Base(source.BinaryPath)
	installed := Candidate{
		Version: source.Version, PluginRoot: slotRoot,
		BinaryPath: filepath.Join(slotRoot, ".codex-plugin", "bin", binaryName), SHA256: source.SHA256,
		BinarySize: source.BinarySize, BinaryModifiedNsec: source.BinaryModifiedNsec,
	}
	if manager.validateRecorded(ctx, installed) == nil {
		if err := freezeRuntimeSlot(installed.PluginRoot); err != nil {
			return Candidate{}, err
		}
		return manager.validateCandidate(ctx, installed.PluginRoot, installed.Version)
	}
	parent := filepath.Dir(slotRoot)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return Candidate{}, fmt.Errorf("create Ocean Watch runtime versions root: %w", err)
	}
	temporary, err := os.MkdirTemp(parent, ".install-")
	if err != nil {
		return Candidate{}, fmt.Errorf("create Ocean Watch runtime staging directory: %w", err)
	}
	defer os.RemoveAll(temporary)
	manifestBytes, err := readRegularFile(filepath.Join(source.PluginRoot, ".codex-plugin", "runtime-manifest.json"))
	if err != nil {
		return Candidate{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return Candidate{}, err
	}
	files := [][2]string{
		{filepath.Join(source.PluginRoot, ".codex-plugin", "runtime-manifest.json"), filepath.Join(temporary, ".codex-plugin", "runtime-manifest.json")},
		{filepath.Join(source.PluginRoot, ".codex-plugin", "plugin.json"), filepath.Join(temporary, ".codex-plugin", "plugin.json")},
	}
	binaryNames := make([]string, 0, len(manifest.SHA256))
	for name := range manifest.SHA256 {
		if filepath.Base(name) != name || name == "." || name == ".." {
			return Candidate{}, errors.New("Ocean Watch runtime manifest binary name is invalid")
		}
		binaryNames = append(binaryNames, name)
	}
	sort.Strings(binaryNames)
	for _, name := range binaryNames {
		files = append(files, [2]string{
			filepath.Join(source.PluginRoot, ".codex-plugin", "bin", name),
			filepath.Join(temporary, ".codex-plugin", "bin", name),
		})
	}
	resourcePaths := make([]string, 0, len(manifest.Resources))
	for resourcePath := range manifest.Resources {
		if !safeResourcePath(resourcePath) {
			return Candidate{}, fmt.Errorf("Ocean Watch runtime resource path is invalid: %s", resourcePath)
		}
		resourcePaths = append(resourcePaths, resourcePath)
	}
	sort.Strings(resourcePaths)
	for _, resourcePath := range resourcePaths {
		files = append(files, [2]string{
			filepath.Join(source.PluginRoot, filepath.FromSlash(resourcePath)),
			filepath.Join(temporary, filepath.FromSlash(resourcePath)),
		})
	}
	for _, paths := range files {
		payload, readErr := readRegularFile(paths[0])
		if readErr != nil {
			return Candidate{}, fmt.Errorf("read Ocean Watch runtime resource: %w", readErr)
		}
		if mkdirErr := os.MkdirAll(filepath.Dir(paths[1]), 0o700); mkdirErr != nil {
			return Candidate{}, mkdirErr
		}
		mode := os.FileMode(0o600)
		if paths[0] == source.BinaryPath || filepath.Base(paths[0]) == "ocean-watch-launcher" || filepath.Ext(paths[0]) == ".exe" {
			mode = 0o700
		}
		if writeErr := os.WriteFile(paths[1], payload, mode); writeErr != nil {
			return Candidate{}, fmt.Errorf("stage Ocean Watch runtime resource: %w", writeErr)
		}
	}
	if err := freezeRuntimeSlot(temporary); err != nil {
		return Candidate{}, fmt.Errorf("freeze Ocean Watch runtime slot: %w", err)
	}
	if err := os.Rename(temporary, slotRoot); err != nil {
		if validated, validateErr := manager.validateCandidate(ctx, slotRoot, source.Version); validateErr == nil {
			return validated, nil
		}
		if runtime.GOOS == "windows" {
			return Candidate{}, fmt.Errorf("activate Ocean Watch runtime slot: %w", err)
		}
		if legacyErr := incompleteRuntimeSlotMatches(source, manifest, slotRoot); legacyErr != nil {
			return Candidate{}, fmt.Errorf("activate Ocean Watch runtime slot: %w", err)
		}
		replaced, replaceErr := replaceInvalidRuntimeSlot(temporary, slotRoot)
		if replaceErr != nil {
			return Candidate{}, fmt.Errorf("replace incomplete Ocean Watch runtime slot: %w", replaceErr)
		}
		defer os.RemoveAll(replaced)
	}
	validated, err := manager.validateCandidate(ctx, slotRoot, source.Version)
	if err != nil {
		return Candidate{}, fmt.Errorf("validate installed Ocean Watch runtime slot: %w", err)
	}
	return validated, nil
}

func incompleteRuntimeSlotMatches(source Candidate, manifest Manifest, slotRoot string) error {
	allowed := map[string]struct{}{
		".codex-plugin/runtime-manifest.json": {},
		".codex-plugin/plugin.json":           {},
	}
	for relative := range manifest.Resources {
		allowed[relative] = struct{}{}
	}
	for name := range manifest.SHA256 {
		allowed[filepath.ToSlash(filepath.Join(".codex-plugin", "bin", name))] = struct{}{}
	}
	seen := make(map[string]struct{}, len(allowed))
	err := filepath.WalkDir(slotRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("legacy runtime slot contains a symlink")
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		relative, relativeErr := filepath.Rel(slotRoot, path)
		if relativeErr != nil {
			return relativeErr
		}
		relative = filepath.ToSlash(relative)
		if _, ok := allowed[relative]; !ok {
			return fmt.Errorf("incomplete runtime slot contains unexpected file %s", relative)
		}
		sourcePayload, sourceErr := readRegularFile(filepath.Join(source.PluginRoot, filepath.FromSlash(relative)))
		if sourceErr != nil {
			return sourceErr
		}
		payload, readErr := readRegularFile(path)
		if readErr != nil || !bytes.Equal(payload, sourcePayload) {
			return fmt.Errorf("incomplete runtime slot file changed: %s", relative)
		}
		seen[relative] = struct{}{}
		return nil
	})
	if err != nil {
		return err
	}
	for _, required := range []string{".codex-plugin/runtime-manifest.json", ".codex-plugin/plugin.json"} {
		if _, ok := seen[required]; !ok {
			return errors.New("incomplete runtime slot has no signed identity")
		}
	}
	if len(seen) == len(allowed) {
		return errors.New("runtime slot is already complete")
	}
	return nil
}

func replaceInvalidRuntimeSlot(staged, destination string) (string, error) {
	parent := filepath.Dir(destination)
	backup, err := os.MkdirTemp(parent, ".replaced-")
	if err != nil {
		return "", err
	}
	if err := os.Remove(backup); err != nil {
		return "", err
	}
	if err := os.Rename(destination, backup); err != nil {
		return "", err
	}
	if err := os.Rename(staged, destination); err != nil {
		if restoreErr := os.Rename(backup, destination); restoreErr != nil {
			return backup, errors.Join(err, fmt.Errorf("restore incomplete runtime slot: %w", restoreErr))
		}
		return "", err
	}
	return backup, nil
}

func freezeRuntimeSlot(root string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	manifestBytes, err := readRegularFile(filepath.Join(root, ".codex-plugin", "runtime-manifest.json"))
	if err != nil {
		return err
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return err
	}
	for _, path := range []string{
		filepath.Join(root, ".codex-plugin", "runtime-manifest.json"),
		filepath.Join(root, ".codex-plugin", "plugin.json"),
	} {
		if err := os.Chmod(path, 0o400); err != nil {
			return err
		}
	}
	for resourcePath := range manifest.Resources {
		mode := os.FileMode(0o400)
		if resourcePath == "bin/ocean-watch-launcher" || strings.HasSuffix(resourcePath, "/run") {
			mode = 0o500
		}
		if err := os.Chmod(filepath.Join(root, filepath.FromSlash(resourcePath)), mode); err != nil {
			return err
		}
	}
	binaryRoot := filepath.Join(root, ".codex-plugin", "bin")
	entries, err := os.ReadDir(binaryRoot)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			if err := os.Chmod(filepath.Join(binaryRoot, entry.Name()), 0o500); err != nil {
				return err
			}
		}
	}
	return nil
}

func (manager Manager) validateRecorded(ctx context.Context, candidate Candidate) error {
	_, err := manager.validateRecordedCandidate(ctx, candidate)
	return err
}

func (manager Manager) validateRecordedCandidate(ctx context.Context, candidate Candidate) (Candidate, error) {
	if candidate.BinaryPath == "" {
		return Candidate{}, errors.New("recorded Ocean Watch runtime is empty")
	}
	validated, err := manager.validateCandidate(ctx, candidate.PluginRoot, candidate.Version)
	if err != nil {
		return Candidate{}, err
	}
	if !sameCandidateIdentity(validated, candidate) {
		return Candidate{}, errors.New("recorded Ocean Watch runtime identity changed")
	}
	return validated, nil
}

func sameCandidateIdentity(left, right Candidate) bool {
	return left.Version != "" && left.Version == right.Version && left.SHA256 == right.SHA256 &&
		left.PluginRoot == right.PluginRoot
}

func (manager Manager) acceptInstalled(ctx context.Context, current, installed Candidate) bool {
	if current.Version == "" || manager.validateRecorded(ctx, current) != nil {
		return true
	}
	comparison, err := compareProductVersions(installed.Version, current.Version)
	return err == nil && comparison >= 0
}

func compareProductVersions(left, right string) (int, error) {
	leftParts, err := productVersion(left)
	if err != nil {
		return 0, err
	}
	rightParts, err := productVersion(right)
	if err != nil {
		return 0, err
	}
	for index := range leftParts {
		if leftParts[index] < rightParts[index] {
			return -1, nil
		}
		if leftParts[index] > rightParts[index] {
			return 1, nil
		}
	}
	return 0, nil
}

func productVersion(version string) ([3]int, error) {
	base := strings.SplitN(strings.TrimSpace(version), "+", 2)[0]
	parts := strings.Split(base, ".")
	if len(parts) != 3 {
		return [3]int{}, errors.New("Ocean Watch runtime version is invalid")
	}
	result := [3]int{}
	for index, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return [3]int{}, errors.New("Ocean Watch runtime version is invalid")
		}
		result[index] = value
	}
	return result, nil
}

func (manager Manager) binaryName() (string, error) {
	goos := manager.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := manager.GOARCH
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	switch goos + "/" + goarch {
	case "darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64":
		return "ocean-watch_" + goos + "_" + goarch, nil
	case "windows/amd64":
		return "ocean-watch_windows_amd64.exe", nil
	default:
		return "", fmt.Errorf("Ocean Watch does not support runtime platform %s/%s", goos, goarch)
	}
}

func (manager Manager) readState() (State, error) {
	payload, err := readRegularFile(manager.statePath())
	if err != nil {
		return State{}, err
	}
	var state State
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil || state.SchemaVersion != 2 {
		return State{}, errors.New("Ocean Watch runtime state is invalid")
	}
	return state, nil
}

func (manager Manager) writeState(state State) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return filesystem.AtomicWritePrivateFile(manager.statePath(), payload)
}

func (manager Manager) statePath() string {
	return filepath.Join(manager.CodexRoot, "ocean-watch", "runtime", "state.json")
}

func (manager Manager) lockPath() string {
	return filepath.Join(manager.CodexRoot, "ocean-watch", "runtime", "resolve.lock")
}

func (manager Manager) runtimeLeasePath(sha256 string) string {
	return filepath.Join(manager.CodexRoot, "ocean-watch", "runtime", "leases", sha256+".lock")
}

func (manager Manager) now() time.Time {
	if manager.Now != nil {
		return manager.Now()
	}
	return time.Now()
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("file must be a regular non-symlink file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o022 != 0 {
		return nil, errors.New("file must not be writable by group or other")
	}
	return os.ReadFile(path)
}

func matchesHash(payload []byte, expected string) bool {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if len(expected) != sha256.Size*2 {
		return false
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]) == expected
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func safeResourcePath(path string) bool {
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(path))))
	return path != "" && cleaned == path && path != "." && !filepath.IsAbs(filepath.FromSlash(path)) &&
		path != ".." && !strings.HasPrefix(path, "../")
}
