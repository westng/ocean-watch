package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type starMapAuthorizationStub struct {
	query  authapplication.StatusQuery
	calls  int
	result authapplication.InspectionResult
	err    error
}

func (stub *starMapAuthorizationStub) Inspect(_ context.Context, query authapplication.StatusQuery) (authapplication.InspectionResult, error) {
	stub.query = query
	stub.calls++
	return stub.result, stub.err
}

func TestStarMapAuthorizationUsesSharedInspectorWithIsolatedChannelAndSubjectOutput(t *testing.T) {
	const subjectID = "3000000000000001"
	const private = "PRIVATE_STAR_MAP_RAW_ACCOUNT"
	stub := &starMapAuthorizationStub{result: authapplication.InspectionResult{
		Status: authapplication.StatusResult{
			Channel: "star_map", HasAppID: true, HasSecret: true, AuthorizationCount: 1,
			AuthorizedAccountCount: 1, AuthorizedAdvertiserCount: 1, Generation: 4,
			AdvertiserIDAuthorized: boolPointer(true),
			Authorizations: []authapplication.AuthorizationStatus{{AuthorizationID: "star-auth", TokenRevision: 2,
				AppID: private, AuthorizedUserID: private, HasAccessToken: true, HasRefreshToken: true,
				AccessTokenExpiresAt: "2026-11-01T12:00:00Z", RefreshTokenExpiresAt: "2026-12-01T12:00:00Z",
			}},
		},
		Mappings: authapplication.MappingResult{
			Mappings: []authapplication.AdvertiserMapping{{AdvertiserID: subjectID, AuthorizationIDs: []string{"star-auth"}}},
			Authorizations: []authapplication.AuthorizationMapping{{AuthorizationID: "star-auth", TokenRevision: 2,
				HasAccessToken: true, HasRefreshToken: true, PendingAccountSync: true,
				AppID: private, AuthorizedUserID: private, AdvertiserIDs: []string{subjectID},
				AuthorizedAccounts: []authapplication.AuthorizedAccount{{AccountName: private}},
			}},
		},
	}}
	logs := new(bytes.Buffer)
	session := connectTestServer(t, Runtime{StarMapAuth: stub, LogWriter: logs})
	defer session.Close()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_star_map_authorization", Arguments: map[string]any{"subject_id": subjectID},
	})
	if err != nil || result.IsError {
		t.Fatalf("authorization result=%#v err=%v", result, err)
	}
	output := decodeStructured[starMapAuthorizationOutput](t, result)
	if stub.calls != 1 || stub.query.Channel != "star_map" || stub.query.AdvertiserID != subjectID ||
		output.Channel != "star_map" || output.Source != "local_state" || output.Status.Generation != 4 ||
		output.Status.SubjectID == nil || *output.Status.SubjectID != subjectID || output.Status.AuthorizedSubjectCount != 1 ||
		len(output.Mappings) != 1 || output.Mappings[0].SubjectKind != "star_account" || output.Mappings[0].SubjectID != subjectID ||
		len(output.Authorizations) != 1 || !output.Authorizations[0].PendingAccountSync || output.Authorizations[0].SubjectIDs[0] != subjectID {
		t.Fatalf("Star Map channel or subject contract changed: query=%#v output=%#v", stub.query, output)
	}
	raw, _ := json.Marshal(result.StructuredContent)
	for _, forbidden := range []string{private, `"advertiser_id"`, `"advertiser_ids"`, `"app_id"`, `"authorized_user_id"`} {
		if bytes.Contains(raw, []byte(forbidden)) || bytes.Contains(logs.Bytes(), []byte(forbidden)) {
			t.Fatalf("private or advertiser-specific field escaped Star Map projection: %s", forbidden)
		}
	}
}

func TestStarMapAuthorizationRejectsOtherChannelsAndSanitizesFailures(t *testing.T) {
	stub := &starMapAuthorizationStub{err: domain.NewError("authorization_not_found", "private fixture", 1, map[string]any{"private": "must not escape"})}
	session := connectTestServer(t, Runtime{StarMapAuth: stub})
	defer session.Close()
	for _, args := range []map[string]any{
		{"channel": "marketing"}, {"advertiser_id": "100"}, {"subject_id": "bad-id"}, {"subject_id": "10000000000000000000"},
	} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_star_map_authorization", Arguments: args})
		if err == nil && (result == nil || !result.IsError) {
			t.Fatalf("invalid Star Map arguments accepted: %#v", args)
		}
	}
	if stub.calls != 0 {
		t.Fatal("invalid arguments reached the authorization service")
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_star_map_authorization", Arguments: map[string]any{"subject_id": "100"}})
	if err != nil || !result.IsError {
		t.Fatalf("missing authorization result=%#v err=%v", result, err)
	}
	raw, _ := json.Marshal(result)
	if !bytes.Contains(raw, []byte("Star Map authorization was not found")) || bytes.Contains(raw, []byte("private fixture")) || bytes.Contains(raw, []byte("must not escape")) {
		t.Fatal("Star Map failure used another channel or leaked internal details")
	}
}

func TestStarMapManagedAccountsRemainSeparateFromAdvertisingChannels(t *testing.T) {
	reader := &managedAccountReaderStub{book: domain.AccountBook{
		SchemaVersion: domain.ManagedAccountSchemaVersion,
		Accounts: map[domain.Channel][]domain.ManagedAccount{
			domain.Marketing: {{Channel: domain.Marketing, AdvertiserID: "100", Name: "marketing", Enabled: true}},
			domain.Qianchuan: {{Channel: domain.Qianchuan, AdvertiserID: "200", Name: "qianchuan", Enabled: true}},
			domain.StarMap: {
				{Channel: domain.StarMap, AdvertiserID: "300", Name: "star", Enabled: true},
				{Channel: domain.StarMap, AdvertiserID: "301", Name: "disabled star", Enabled: false},
			},
		},
	}}
	session := connectTestServer(t, Runtime{ManagedAccounts: reader})
	defer session.Close()
	for _, includeDisabled := range []bool{false, true} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_managed_accounts", Arguments: map[string]any{"channel": "star_map", "include_disabled": includeDisabled}})
		if err != nil || result.IsError {
			t.Fatalf("Star Map accounts result=%#v err=%v", result, err)
		}
		output := decodeStructured[managedAccountsOutput](t, result)
		want := 1
		if includeDisabled {
			want = 2
		}
		if output.TotalCount != want || len(output.Accounts) != want {
			t.Fatalf("Star Map filter ignored disabled-account choice: %#v", output)
		}
		for _, account := range output.Accounts {
			if account.Channel != "star_map" || account.SubjectKind != "star_account" || account.SubjectID == "" {
				t.Fatalf("Star Map account mixed with advertising channels: %#v", account)
			}
		}
	}
}
