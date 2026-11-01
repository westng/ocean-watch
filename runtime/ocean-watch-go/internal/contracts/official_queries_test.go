package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestOfficialCandidateCatalogPreservesPublishedInventory(t *testing.T) {
	catalog, err := QianchuanOfficialCandidates()
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	tuples := make([][3]string, 0, len(catalog.Queries))
	for _, query := range catalog.Queries {
		if query.Executable || query.Status != OfficialQueryPublishedOnly {
			t.Fatalf("candidate grants permission: %s", query.QueryID)
		}
		counts[query.Group]++
		tuples = append(tuples, [3]string{query.UpstreamTool, query.APIPath, query.DocumentationURL})
	}
	expected := map[string]int{
		"accounts": 17, "finance": 17, "plans": 23, "assets": 14,
		"products": 5, "reports": 20, "live": 5, "aweme_orders": 17,
		"targeting": 21, "compliance": 9, "operations": 8,
	}
	for group, count := range expected {
		if counts[group] != count {
			t.Fatalf("%s count=%d, want %d", group, counts[group], count)
		}
	}
	data, err := json.Marshal(tuples)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != "e4e0c8f7ac1c18df11ca956eaf904be9c0d4526b8d5081a4955933f5316f8dc6" {
		t.Fatal("public query names, API paths or original document links changed")
	}
	list, ok := catalog.Find("qianchuan_uni_promotion_list_v1")
	if !ok || list.Notes == "" {
		t.Fatal("unverified list-document discrepancy must remain visible")
	}
	if _, ok := catalog.Find("qianchuan_report_all_promotion_get_v1"); ok {
		t.Fatal("SDK-only overall report was added to the public MCP inventory")
	}
}

func TestOfficialCandidateCatalogCannotBeMutatedIntoSharedPermission(t *testing.T) {
	catalog, err := QianchuanOfficialCandidates()
	if err != nil {
		t.Fatal(err)
	}
	catalog.Queries[0].Executable = true
	if _, err := decodeOfficialQueryCatalog(mustOfficialJSON(t, catalog)); !errors.Is(err, ErrInvalidOfficialQueryCatalog) {
		t.Fatalf("executable candidate accepted: %v", err)
	}
	fresh, err := QianchuanOfficialCandidates()
	if err != nil || fresh.Queries[0].Executable {
		t.Fatal("caller mutation leaked into embedded candidate metadata")
	}
	catalog = fresh
	catalog.Queries[1].APIPath = catalog.Queries[0].APIPath
	if _, err := decodeOfficialQueryCatalog(mustOfficialJSON(t, catalog)); !errors.Is(err, ErrInvalidOfficialQueryCatalog) {
		t.Fatalf("duplicate path accepted: %v", err)
	}
}

func TestOfficialFirstReleaseKeepsHistoricalDependenciesAndUsesRegisteredCapabilities(t *testing.T) {
	manifest, err := QianchuanFirstReleaseCandidates()
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Queries) != 15 {
		t.Fatalf("first-release queries=%d, want 15", len(manifest.Queries))
	}
	reused := map[string]string{
		"get_qianchuan_report_schema":       "qianchuan.report_schema",
		"report_qianchuan_custom":           "qianchuan.custom_report",
		"report_qianchuan_managed_accounts": "shared.managed_account_report",
	}
	for _, task := range manifest.Tasks {
		if task.Status != "planned" {
			t.Fatalf("unimplemented task declared ready: %s", task.TaskID)
		}
		if spec, registered := DefaultCapabilityRegistry.ByMCPTool(task.TargetMCPTool); !registered || spec.ID != task.CapabilityID {
			t.Fatalf("first-release task lacks its unique registered capability: %s", task.TargetMCPTool)
		}
		if capability, exists := reused[task.TargetMCPTool]; exists {
			if task.CapabilityID != capability || !task.ExistingCapability ||
				len(task.RequiredToolsets) != 1 || task.RequiredToolsets[0] != "reports" {
				t.Fatalf("report capability/policy drift: %#v", task)
			}
		}
	}
}

func TestOfficialQueryRequestPreservesDecimalAndLongIntegerTokens(t *testing.T) {
	input := `{"query_id":"qianchuan_ad_quota_get_v1","scope":{"kind":"advertiser","id":"9007199254740993"},"parameters":{"object_id":9007199254740993,"amount":1234567890.123456789,"nested":{"ids":[9007199254740995]}}}`
	request, err := DecodeOfficialQueryRequest(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if request.Limit != 200 || string(request.Parameters["object_id"]) != "9007199254740993" ||
		string(request.Parameters["amount"]) != "1234567890.123456789" {
		t.Fatalf("precision/default changed: %#v", request)
	}
	data := mustOfficialJSON(t, request)
	if !strings.Contains(string(data), "9007199254740995") {
		t.Fatal("nested long ID lost precision")
	}
}

func TestOfficialQueryRequestRejectsAuthorityAndAmbiguousInput(t *testing.T) {
	base := `{"query_id":"qianchuan_ad_quota_get_v1","scope":{"kind":"advertiser","id":"123456789"},"parameters":{}}`
	for name, input := range map[string]string{
		"token":               strings.Replace(base, `"parameters":{}`, `"parameters":{"Access-Token":"synthetic-secret"}`, 1),
		"nested headers":      strings.Replace(base, `"parameters":{}`, `"parameters":{"filter":{"headers":{"x":"synthetic-secret"}}}`, 1),
		"server URL":          strings.Replace(base, `"parameters":{}`, `"parameters":{},"server_url":"https://example.invalid/"`, 1),
		"mode":                strings.Replace(base, `"parameters":{}`, `"parameters":{},"execution_mode":"execute"`, 1),
		"scope override":      strings.Replace(base, `"kind":"advertiser"`, `"kind":"advertiser","allowed_accounts":["999"]`, 1),
		"scope case alias":    strings.Replace(base, `"kind":"advertiser"`, `"Kind":"advertiser"`, 1),
		"root case alias":     strings.Replace(base, "query_id", "Query_ID", 1),
		"duplicate scope":     strings.Replace(base, `"parameters":{}`, `"parameters":{},"scope":{"kind":"advertiser","id":"999"}`, 1),
		"duplicate parameter": strings.Replace(base, `"parameters":{}`, `"parameters":{"object_id":1,"object_id":2}`, 1),
		"extra JSON":          base + " {}",
		"numeric subject":     strings.Replace(base, `"id":"123456789"`, `"id":123456789`, 1),
		"unknown scope":       strings.Replace(base, "advertiser", "user", 1),
		"limit":               strings.Replace(base, `"parameters":{}`, `"parameters":{},"limit":1001`, 1),
		"large input":         strings.Repeat(" ", MaxOfficialQueryRequestBytes) + base,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := DecodeOfficialQueryRequest(strings.NewReader(input))
			if !errors.Is(err, ErrInvalidOfficialQueryRequest) || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatalf("unsafe input/error: %v", err)
			}
		})
	}
}

func TestOfficialQueryScopeTypesRemainDistinct(t *testing.T) {
	for _, kind := range []string{"advertiser", "agent", "shop", "customer_center", "organization", "authorization", "public_catalog"} {
		id := "123456789"
		if kind == "authorization" {
			id = "fixture-authorization"
		}
		if kind == "public_catalog" {
			id = ""
		}
		input := mustOfficialJSON(t, OfficialQueryRequest{
			QueryID: "fixture_query", Scope: OfficialQueryScope{Kind: kind, ID: id},
			Parameters: map[string]json.RawMessage{},
		})
		request, err := DecodeOfficialQueryRequest(strings.NewReader(string(input)))
		if err != nil || request.Scope.Kind != kind || request.Scope.ID != id {
			t.Fatalf("scope=%s lost distinction: %#v %v", kind, request.Scope, err)
		}
	}
}

func mustOfficialJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
