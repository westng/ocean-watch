package contractrunner

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

const officialSDKVersion = "v1.1.92"

var (
	fullSHA256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	semverCorePattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
)

type CandidateIdentity struct {
	SchemaVersion            int    `json:"schema_version"`
	GitSHA                   string `json:"git_sha"`
	ProductVersion           string `json:"product_version"`
	PluginVersion            string `json:"plugin_version"`
	SDKVersion               string `json:"sdk_version"`
	SourceTreeSHA256         string `json:"source_tree_sha256"`
	CandidateChecksumsSHA256 string `json:"candidate_checksums_sha256"`
	ReleasePublicKeySHA256   string `json:"release_public_key_sha256"`
	Release                  bool   `json:"release"`
}

var candidateIdentityFields = map[string]struct{}{
	"schema_version": {}, "git_sha": {}, "product_version": {},
	"plugin_version": {}, "sdk_version": {}, "source_tree_sha256": {},
	"candidate_checksums_sha256": {}, "release_public_key_sha256": {},
	"release": {},
}

func LoadCandidateIdentity(path, expectedGitSHA string) (*CandidateIdentity, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read candidate identity: %w", err)
	}
	var document map[string]json.RawMessage
	if err := decodeSingleJSON(payload, &document); err != nil {
		return nil, fmt.Errorf("decode candidate identity: %w", err)
	}
	identityPayload := payload
	if nested, exists := document["candidate_identity"]; exists {
		identityPayload = nested
	}
	var fields map[string]json.RawMessage
	if err := decodeSingleJSON(identityPayload, &fields); err != nil {
		return nil, fmt.Errorf("decode candidate_identity: %w", err)
	}
	if err := validateCandidateIdentityFields(fields); err != nil {
		return nil, err
	}
	var identity CandidateIdentity
	if err := decodeSingleJSON(identityPayload, &identity); err != nil {
		return nil, fmt.Errorf("decode candidate_identity: %w", err)
	}
	if err := identity.Validate(expectedGitSHA); err != nil {
		return nil, err
	}
	return &identity, nil
}

func (identity CandidateIdentity) Validate(expectedGitSHA string) error {
	if identity.SchemaVersion != 1 {
		return fmt.Errorf("candidate_identity schema_version must be 1")
	}
	if err := validateGitSHA(identity.GitSHA); err != nil {
		return fmt.Errorf("candidate_identity git_sha: %w", err)
	}
	if expectedGitSHA != "" && identity.GitSHA != expectedGitSHA {
		return fmt.Errorf("candidate_identity git_sha does not match the requested git SHA")
	}
	if !semverCorePattern.MatchString(identity.ProductVersion) {
		return fmt.Errorf("candidate_identity product_version must be SemVer core")
	}
	if !strings.HasPrefix(identity.PluginVersion, identity.ProductVersion+"+codex.") || len(identity.PluginVersion) == len(identity.ProductVersion)+len("+codex.") {
		return fmt.Errorf("candidate_identity plugin_version does not match the product version")
	}
	if identity.SDKVersion != officialSDKVersion {
		return fmt.Errorf("candidate_identity sdk_version does not match the pinned SDK")
	}
	for name, value := range map[string]string{
		"source_tree_sha256":         identity.SourceTreeSHA256,
		"candidate_checksums_sha256": identity.CandidateChecksumsSHA256,
		"release_public_key_sha256":  identity.ReleasePublicKeySHA256,
	} {
		if !fullSHA256Pattern.MatchString(value) || value == strings.Repeat("0", 64) {
			return fmt.Errorf("candidate_identity %s must be a non-zero lowercase SHA-256", name)
		}
	}
	return nil
}

func validateCandidateIdentityFields(fields map[string]json.RawMessage) error {
	if len(fields) != len(candidateIdentityFields) {
		return fmt.Errorf("candidate_identity must contain exactly the sealed identity fields")
	}
	for field := range candidateIdentityFields {
		if _, exists := fields[field]; !exists {
			return fmt.Errorf("candidate_identity field is missing: %s", field)
		}
	}
	for field := range fields {
		if _, exists := candidateIdentityFields[field]; !exists {
			return fmt.Errorf("candidate_identity field is unexpected: %s", field)
		}
	}
	return nil
}

func decodeSingleJSON(payload []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return fmt.Errorf("multiple JSON values are not allowed")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
