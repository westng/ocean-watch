package contractrunner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
)

var fullGitSHAPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

func validateGitSHA(value string) error {
	if !fullGitSHAPattern.MatchString(value) || value == "0000000000000000000000000000000000000000" {
		return fmt.Errorf("git SHA must be a non-zero full lowercase SHA-1")
	}
	return nil
}

func nativePlatformID() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

func writeJSON(path string, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if err := validateEvidence(payload); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".contract-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(payload); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace contract evidence: %w", err)
	}
	return nil
}

func readCapture(path string) (Capture, string, error) {
	root := path
	info, err := os.Stat(path)
	if err != nil {
		return Capture{}, "", err
	}
	if info.IsDir() {
		path = filepath.Join(path, "capture.json")
	} else {
		root = filepath.Dir(path)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return Capture{}, "", err
	}
	var capture Capture
	if err := json.Unmarshal(payload, &capture); err != nil {
		return Capture{}, "", err
	}
	if capture.SchemaVersion != CaptureSchemaVersion || capture.Kind != "python-baseline" {
		return Capture{}, "", fmt.Errorf("unsupported baseline capture")
	}
	if err := validateGitSHA(capture.GitSHA); err != nil {
		return Capture{}, "", fmt.Errorf("baseline capture identity: %w", err)
	}
	if capture.Platform != nativePlatformID() {
		return Capture{}, "", fmt.Errorf("baseline capture platform does not match the native runner")
	}
	return capture, root, nil
}
