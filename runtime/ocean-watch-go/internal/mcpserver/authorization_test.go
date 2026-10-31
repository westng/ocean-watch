package mcpserver

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/oceanengine"
	auth "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type recoveryFixture struct {
	starts int
	last   auth.RecoveryRequest
}

func (fixture *recoveryFixture) Start(_ context.Context, input auth.RecoveryRequest) (auth.RecoverySession, error) {
	fixture.starts++
	fixture.last = input
	operation := input.Operation
	if operation == "" {
		operation = "authorize"
	}
	status := "awaiting_callback"
	authorizationURL := "https://qianchuan.jinritemai.com/openapi/qc/audit/oauth.html?app_id=123&redirect_uri=http%3A%2F%2F127.0.0.1%3A8787%2Foauth%2Fcallback&state=qianchuan%3Afixture%3Astate"
	if operation == "sync_accounts" {
		status = "awaiting_user"
		authorizationURL = "http://127.0.0.1:8787/oauth/confirm?session=" + strings.Repeat("a", 48)
	}
	return auth.RecoverySession{SessionID: strings.Repeat("a", 48), Channel: input.Channel, Operation: operation, Status: status,
		AuthorizationURL: authorizationURL, ExpiresAt: "2026-10-24T01:00:00Z"}, nil
}
func (fixture *recoveryFixture) Get(context.Context, string, string) (auth.RecoverySession, error) {
	return auth.RecoverySession{}, domain.NewError("authorization_session_not_found", "fixture", 1, nil)
}
func (fixture *recoveryFixture) Cancel(ctx context.Context, channel, id string) (auth.RecoverySession, error) {
	return fixture.Get(ctx, channel, id)
}

func TestAuthorizationMCPRejectsSecretsCommandsAndCrossOperationArguments(t *testing.T) {
	fixture, logs := &recoveryFixture{}, new(bytes.Buffer)
	session := connectTestServer(t, Runtime{AuthorizationRecovery: fixture, LogWriter: logs})
	for _, arguments := range []map[string]any{
		{}, {"channel": "qianchuan", "secret": "fixture-secret"},
		{"channel": "qianchuan", "redirect_uri": "https://untrusted.example"}, {"channel": "qianchuan", "command": "fixture-command"},
		{"channel": "qianchuan", "authorization_id": "fixture auth"}, {"channel": "qianchuan", "advertiser_id": 1001},
		{"channel": "qianchuan", "advertiser_id": ""}, {"channel": "qianchuan", "session_id": strings.Repeat("a", 48)},
	} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "start_authorization", Arguments: arguments})
		if err == nil && !result.IsError {
			t.Fatalf("unsafe arguments accepted: %v", arguments)
		}
	}
	if fixture.starts != 0 {
		t.Fatal("invalid tool input reached authorization service")
	}
	result := callTestTool(t, session, "start_authorization", map[string]any{"channel": "qianchuan", "advertiser_id": "1001"})
	if result.IsError || fixture.starts != 1 || fixture.last.Channel != "qianchuan" || fixture.last.AdvertiserID != "1001" {
		t.Fatal("valid recovery input not routed")
	}
	started := decodeStructured[struct {
		Session auth.RecoverySession `json:"session"`
	}](t, result)
	if started.Session.Status != "awaiting_callback" || !strings.HasPrefix(started.Session.AuthorizationURL, "https://qianchuan.jinritemai.com/") || strings.Contains(started.Session.AuthorizationURL, "/oauth/confirm") {
		t.Fatalf("start_authorization did not return the official OAuth URL: %+v", started.Session)
	}
	result = callTestTool(t, session, "start_authorization", map[string]any{"channel": "star_map", "authorization_id": "grant-1", "rebind_existing": false})
	if result.IsError || fixture.starts != 2 || fixture.last.Channel != "star_map" || fixture.last.AuthorizationID != "grant-1" || fixture.last.Operation != "reauthorize" || fixture.last.RebindExisting {
		t.Fatalf("explicit reauthorization was not routed safely: %#v", fixture.last)
	}
	result = callTestTool(t, session, "start_account_sync", map[string]any{"channel": "star_map", "authorization_id": "grant-1", "rebind_existing": true})
	if result.IsError || fixture.starts != 3 || fixture.last.Operation != "sync_accounts" || !fixture.last.RebindExisting {
		t.Fatalf("explicit account rebind was not routed: %#v", fixture.last)
	}
	syncStarted := decodeStructured[struct {
		Session auth.RecoverySession `json:"session"`
	}](t, result)
	if syncStarted.Session.Status != "awaiting_user" || !strings.HasPrefix(syncStarted.Session.AuthorizationURL, "http://127.0.0.1:") || !strings.Contains(syncStarted.Session.AuthorizationURL, "/oauth/confirm?session=") {
		t.Fatalf("start_account_sync changed the local confirmation contract: %+v", syncStarted.Session)
	}
	if bytes.Contains(logs.Bytes(), []byte("fixture-secret")) || bytes.Contains(logs.Bytes(), []byte("fixture-command")) || bytes.Contains(logs.Bytes(), []byte("/oauth/confirm")) {
		t.Fatal("authorization inputs leaked into logs")
	}
}

func TestInvalidRefreshErrorsHaveNonRetryableActionableRecoveryAcrossTransports(t *testing.T) {
	for _, code := range []int64{40103, 40107} {
		err := fmt.Errorf("fixture stage: %w", &oceanengine.EnvelopeError{Code: code, Message: "fixture refresh_token secret"})
		for name, mapper := range map[string]func(error) toolFailure{"qianchuan": mapOfficialQueryError, "marketing": mapMarketingOfficialError, "preflight": mapQianchuanPreflightError} {
			failure := mapper(err)
			if failure.Code != "REAUTHORIZATION_REQUIRED" || failure.Retryable || failure.Details["recovery_tool"] != "start_authorization" {
				t.Fatalf("%s code=%d result=%+v", name, code, failure)
			}
			if strings.Contains(failure.Message, "secret") {
				t.Fatal("provider error leaked to model")
			}
		}
	}
	for _, mapper := range []func(error) toolFailure{mapOfficialQueryError, mapMarketingOfficialError, mapQianchuanPreflightError} {
		failure := mapper(domain.NewError("authorization_pending_sync", "fixture", 1, nil))
		if failure.Code != "ACCOUNT_SYNC_REQUIRED" || failure.Retryable || failure.Details["recovery_tool"] != "start_account_sync" {
			t.Fatalf("pending discovery incorrectly requests OAuth: %+v", failure)
		}
	}
}
