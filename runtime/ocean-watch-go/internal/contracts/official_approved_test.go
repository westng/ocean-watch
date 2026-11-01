package contracts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOfficialCompiledCatalogRetainsAllPublishedIdentitiesAndEvidence(t *testing.T) {
	published, err := QianchuanOfficialCandidates()
	if err != nil {
		t.Fatal(err)
	}
	approved, err := ApprovedOfficialQueries()
	if err != nil {
		t.Fatal(err)
	}
	if len(approved) != 156 {
		t.Fatal("catalog identity count changed")
	}
	for i, query := range approved {
		t.Run(query.QueryID, func(t *testing.T) {
			if query.QueryID != published.Queries[i].QueryID || query.APIPath != published.Queries[i].APIPath || query.Policy.BusinessMethod != "GET" || query.Policy.Effect != EffectOfficialRead || query.Policy.MinimumMode != QueryModeRead {
				t.Fatal("identity or business method drift")
			}
			if query.Executable && (len(query.HostInput) == 0 || len(query.OutputFields) == 0 || query.Policy.SourceDigest == "" || query.Policy.ContractFingerprint == "" || len(query.ScopeKinds) == 0 || query.DocumentDigest == "") {
				t.Fatal("enabled contract missing evidence")
			}
			var schema map[string]any
			if json.Unmarshal(query.HostInput, &schema) != nil {
				t.Fatal("invalid input schema")
			}
			props, _ := schema["properties"].(map[string]any)
			for name := range props {
				if reservedOfficialQueryField(name) || strings.Contains(strings.ToLower(name), "token") {
					t.Fatal("credential in Host input")
				}
			}
			if !query.Executable && len(query.MissingEvidence) == 0 {
				t.Fatal("blocked item lacks precise evidence gap")
			}
		})
	}
}
func TestOfficialMissingNegativeWordToolHasFrozenDocumentedMapping(t *testing.T) {
	queries, _ := ApprovedOfficialQueries()
	for _, query := range queries {
		if query.QueryID != "qianchuan_ad_pivativewords_get_v1" {
			continue
		}
		if query.ExecutionQueryID != "qianchuan_ad_detail_get_v1" || query.ProjectionPath != "pivative_words" || query.MappingEvidence == "" {
			t.Fatal("missing explicit mapping")
		}
		return
	}
	t.Fatal("published negative-word tool was dropped")
}
func TestOfficialContractDecodingIsOwnedPerCall(t *testing.T) {
	first, _ := ApprovedOfficialQueries()
	first[0].Policy.RequiredToolsets[0] = "unknown"
	first[0].OutputFields[0].Path = "corrupted"
	second, _ := ApprovedOfficialQueries()
	if second[0].Policy.RequiredToolsets[0] == "unknown" || second[0].OutputFields[0].Path == "corrupted" {
		t.Fatal("shared contract mutated")
	}
}
