package contracts

import (
	_ "embed"
	"errors"
)

const QianchuanFirstReleaseVersion = "S_R1-draft-2026-11-01"

var ErrInvalidOfficialReleaseManifest = errors.New("invalid official first-release candidate manifest")

//go:embed catalog/qianchuan-first-release-2026-11-01.json
var qianchuanFirstReleaseJSON []byte

// This historical draft freezes the originally agreed first-release dependencies,
// never dispatch permission or current readiness. Current surfaces and execution
// contracts are owned by CapabilityRegistry and ApprovedOfficialQueries.
type OfficialReleaseManifest struct {
	Version        string                          `json:"manifest_version"`
	CatalogVersion string                          `json:"catalog_version"`
	Status         string                          `json:"status"`
	Tasks          []OfficialReleaseTask           `json:"tasks"`
	Queries        []OfficialReleaseQueryCandidate `json:"query_candidates"`
}

type OfficialReleaseTask struct {
	TaskID             string             `json:"task_id"`
	TargetMCPTool      string             `json:"target_mcp_tool"`
	CapabilityID       string             `json:"capability_id"`
	ExistingCapability bool               `json:"existing_capability"`
	Backend            string             `json:"backend"`
	RequiredQueries    []string           `json:"required_queries"`
	ConditionalQueries []string           `json:"conditional_queries"`
	RequiredToolsets   []string           `json:"required_toolsets"`
	MinimumMode        QueryExecutionMode `json:"minimum_mode"`
	Status             string             `json:"status"`
	ContractVersion    string             `json:"contract_version"`
	SceneValidation    string             `json:"scene_validation"`
	MissingEvidence    []string           `json:"missing_evidence"`
}

type OfficialReleaseQueryCandidate struct {
	QueryID         string              `json:"query_id"`
	Policy          OfficialQueryPolicy `json:"proposed_policy"`
	ContractVersion string              `json:"contract_version"`
	Status          string              `json:"status"`
	MethodEvidence  string              `json:"method_evidence,omitempty"`
	MissingEvidence []string            `json:"missing_evidence"`
}

func QianchuanFirstReleaseCandidates() (OfficialReleaseManifest, error) {
	var manifest OfficialReleaseManifest
	if decodeOneJSON(qianchuanFirstReleaseJSON, &manifest) != nil ||
		manifest.Version != QianchuanFirstReleaseVersion ||
		manifest.CatalogVersion != QianchuanOfficialCatalogVersion ||
		manifest.Status != "draft" || len(manifest.Tasks) != 9 {
		return OfficialReleaseManifest{}, ErrInvalidOfficialReleaseManifest
	}
	catalog, err := QianchuanOfficialCandidates()
	if err != nil {
		return OfficialReleaseManifest{}, err
	}
	queries := map[string]bool{}
	for _, query := range manifest.Queries {
		_, published := catalog.Find(query.QueryID)
		policy := query.Policy
		if !published || queries[query.QueryID] || query.Status != "contract_pending" ||
			query.ContractVersion != manifest.Version || len(query.MissingEvidence) == 0 ||
			policy.CapabilityID == "" || policy.Channel != "qianchuan" ||
			policy.Effect != EffectOfficialRead || policy.MinimumMode != QueryModeRead ||
			!validDraftToolsets(policy.RequiredToolsets) || policy.ScopeResolver != "pending" ||
			(policy.BusinessMethod != "" && (policy.BusinessMethod != "GET" || query.MethodEvidence == "")) ||
			(len(policy.InputSchema) != 0 && string(policy.InputSchema) != "null") ||
			(len(policy.OutputSchema) != 0 && string(policy.OutputSchema) != "null") ||
			policy.ContractFingerprint != "" {
			return OfficialReleaseManifest{}, ErrInvalidOfficialReleaseManifest
		}
		queries[query.QueryID] = true
	}
	tasks, tools := map[string]bool{}, map[string]bool{}
	for _, task := range manifest.Tasks {
		existingSpec, existing := DefaultCapabilityRegistry.ByID(task.CapabilityID)
		if task.TaskID == "" || task.TargetMCPTool == "" || task.CapabilityID == "" ||
			tasks[task.TaskID] || tools[task.TargetMCPTool] ||
			(task.ExistingCapability && !existing) || (existing && existingSpec.MCPTool != task.TargetMCPTool) || task.Status != "planned" ||
			task.ContractVersion != manifest.Version || task.SceneValidation != "pending" ||
			task.MinimumMode != QueryModeRead || !validDraftToolsets(task.RequiredToolsets) ||
			len(task.MissingEvidence) == 0 {
			return OfficialReleaseManifest{}, ErrInvalidOfficialReleaseManifest
		}
		for _, queryID := range append(append([]string{}, task.RequiredQueries...), task.ConditionalQueries...) {
			if !queries[queryID] {
				return OfficialReleaseManifest{}, ErrInvalidOfficialReleaseManifest
			}
		}
		tasks[task.TaskID], tools[task.TargetMCPTool] = true, true
	}
	return manifest, nil
}

func validDraftToolsets(toolsets []string) bool {
	if len(toolsets) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, toolset := range toolsets {
		if seen[toolset] || (toolset != "core" && toolset != "qianchuan" && toolset != "reports") {
			return false
		}
		seen[toolset] = true
	}
	return true
}
