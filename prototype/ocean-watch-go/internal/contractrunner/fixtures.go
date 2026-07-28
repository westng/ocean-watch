package contractrunner

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var caseIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func ReadFixtureManifest(root string) (FixtureManifest, error) {
	if root == "" {
		return FixtureManifest{SchemaVersion: 1}, nil
	}
	payload, err := os.ReadFile(filepath.Join(root, "cases.json"))
	if err != nil {
		return FixtureManifest{}, fmt.Errorf("read fixture manifest: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	var manifest FixtureManifest
	if err := decoder.Decode(&manifest); err != nil {
		return FixtureManifest{}, fmt.Errorf("decode fixture manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return FixtureManifest{}, fmt.Errorf("decode fixture manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 {
		return FixtureManifest{}, fmt.Errorf("unsupported fixture schema version %d", manifest.SchemaVersion)
	}
	seen := map[string]struct{}{}
	for index := range manifest.Cases {
		if err := validateCaseSpec(manifest.Cases[index], root); err != nil {
			return FixtureManifest{}, fmt.Errorf("case %d: %w", index, err)
		}
		if _, exists := seen[manifest.Cases[index].ID]; exists {
			return FixtureManifest{}, fmt.Errorf("duplicate fixture case ID %q", manifest.Cases[index].ID)
		}
		seen[manifest.Cases[index].ID] = struct{}{}
	}
	return manifest, nil
}

func validateCaseSpec(spec CaseSpec, fixtureRoot string) error {
	if !caseIDPattern.MatchString(spec.ID) {
		return errors.New("case ID must contain only lowercase letters, digits, dot, underscore, or dash")
	}
	if len(spec.Argv) == 0 {
		return errors.New("argv must not be empty")
	}
	if spec.TimeoutMS < 0 || spec.TimeoutMS > 300000 {
		return errors.New("timeout_ms must be between 0 and 300000")
	}
	if spec.NetworkPolicy != "" && spec.NetworkPolicy != "forbidden" {
		return errors.New("only the forbidden network policy is supported by deterministic fixtures")
	}
	for _, normalizer := range spec.Normalizers {
		if normalizer != "trim-trailing-space" &&
			normalizer != "qianchuan-template-ids" &&
			normalizer != "launcher-command" {
			return fmt.Errorf("unsupported normalizer: %s", normalizer)
		}
	}
	if spec.Fixture != "" {
		path, err := safeJoin(fixtureRoot, spec.Fixture)
		if err != nil {
			return err
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("fixture directory is unavailable: %s", spec.Fixture)
		}
	}
	return nil
}

func safeJoin(root, relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", errors.New("fixture path must be relative")
	}
	clean := filepath.Clean(relative)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("fixture path escapes its root")
	}
	joined := filepath.Join(root, clean)
	relativeToRoot, err := filepath.Rel(filepath.Clean(root), joined)
	if err != nil || relativeToRoot == ".." || strings.HasPrefix(relativeToRoot, ".."+string(filepath.Separator)) {
		return "", errors.New("fixture path escapes its root")
	}
	return joined, nil
}

func copyDirectory(source, target string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("fixture symbolic links are forbidden: %s", relative)
		}
		if info.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported fixture file: %s", relative)
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		inputCloseErr := input.Close()
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if inputCloseErr != nil {
			return inputCloseErr
		}
		if closeErr != nil {
			return closeErr
		}
		return os.Chtimes(destination, info.ModTime(), info.ModTime())
	})
}
