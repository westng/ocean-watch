package filesystem

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

var ErrManagedConfigInvalid = errors.New("managed configuration is invalid")

type ManagedConfigStore struct {
	CodexRoot string
	Root      string
	Path      string
}

func NewManagedConfigStore(getenv func(string) string, userHome string) (ManagedConfigStore, error) {
	codexRoot, err := filepath.Abs(CodexHome(getenv, userHome))
	if err != nil {
		return ManagedConfigStore{}, fmt.Errorf("resolve Codex state root: %w", err)
	}
	root := filepath.Join(codexRoot, "ads-plan-monitor")
	path := filepath.Join(root, "config.json")
	if err := ensureWithinRoot(root, path); err != nil {
		return ManagedConfigStore{}, err
	}
	return ManagedConfigStore{
		CodexRoot: filepath.Clean(codexRoot), Root: filepath.Clean(root), Path: filepath.Clean(path),
	}, nil
}

func (store ManagedConfigStore) Read(ctx context.Context) (map[string]any, error) {
	return store.read(ctx)
}

func (store ManagedConfigStore) ReadWithRevision(ctx context.Context) (map[string]any, string, error) {
	config, err := store.read(ctx)
	if err != nil {
		return nil, "", err
	}
	revision, err := JSONRevision(config)
	if err != nil {
		return nil, "", err
	}
	return config, revision, nil
}

func (store ManagedConfigStore) read(ctx context.Context) (map[string]any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	file, err := store.openConfig()
	if err != nil {
		return nil, err
	}
	defer file.Close()
	config, err := decodeJSON(file)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrManagedConfigInvalid, err)
	}
	return config, nil
}

func (store ManagedConfigStore) openConfig() (*os.File, error) {
	if err := ensureWithinRoot(store.Root, store.Path); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrManagedConfigInvalid, err)
	}
	codexInfo, err := os.Lstat(store.CodexRoot)
	if err != nil {
		return nil, fmt.Errorf("inspect Codex state root: %w", err)
	}
	if !codexInfo.IsDir() || codexInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: Codex state root must be a real directory", ErrManagedConfigInvalid)
	}
	rootInfo, err := os.Lstat(store.Root)
	if err != nil {
		return nil, fmt.Errorf("inspect managed state root: %w", err)
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: managed state root must be a real directory", ErrManagedConfigInvalid)
	}
	if runtime.GOOS != "windows" && rootInfo.Mode().Perm()&0o077 != 0 {
		return nil, os.ErrPermission
	}
	root, err := os.OpenRoot(store.Root)
	if err != nil {
		return nil, fmt.Errorf("open managed state root: %w", err)
	}
	defer root.Close()
	openedRootInfo, err := root.Stat(".")
	if err != nil || !os.SameFile(rootInfo, openedRootInfo) {
		return nil, fmt.Errorf("%w: managed state root changed while opening", ErrManagedConfigInvalid)
	}
	if runtime.GOOS != "windows" && openedRootInfo.Mode().Perm()&0o077 != 0 {
		return nil, os.ErrPermission
	}
	name := filepath.Base(store.Path)
	before, err := root.Lstat(name)
	if err != nil {
		return nil, fmt.Errorf("inspect managed configuration: %w", err)
	}
	if !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: managed configuration must be a regular file", ErrManagedConfigInvalid)
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open managed configuration: %w", err)
	}
	opened, statErr := file.Stat()
	after, lstatErr := root.Lstat(name)
	if statErr != nil || lstatErr != nil || !opened.Mode().IsRegular() || after.Mode()&os.ModeSymlink != 0 ||
		!os.SameFile(before, opened) || !os.SameFile(opened, after) {
		_ = file.Close()
		return nil, fmt.Errorf("%w: managed configuration changed while opening", ErrManagedConfigInvalid)
	}
	if runtime.GOOS != "windows" && opened.Mode().Perm()&0o077 != 0 {
		_ = file.Close()
		return nil, os.ErrPermission
	}
	return file, nil
}

func ensureWithinRoot(root, path string) error {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("resolve managed configuration path: %w", err)
	}
	if relative == ".." || filepath.IsAbs(relative) || len(relative) > 3 && relative[:3] == ".."+string(filepath.Separator) {
		return errors.New("managed configuration escaped its state root")
	}
	return nil
}
