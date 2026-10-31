package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	auth "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
)

type diagnosticRecoveryFixture struct {
	*recoveryFixture
	view auth.RecoverySession
}

func (fixture diagnosticRecoveryFixture) Get(context.Context, string, string) (auth.RecoverySession, error) {
	return fixture.view, nil
}

func TestAuthorizationDiagnosticsPreserveTheStructuredToolContract(t *testing.T) {
	for _, requestID := range []string{"request-fixture", "https://example.test/?secret=fixture-private-secret"} {
		t.Run(requestID, func(t *testing.T) {
			fixture := diagnosticRecoveryFixture{recoveryFixture: &recoveryFixture{}, view: auth.RecoverySession{
				SessionID: strings.Repeat("a", 48), Channel: "qianchuan", Operation: "authorize", Status: "failed", ExpiresAt: "2026-10-29T01:00:00Z",
				ErrorCode: "AUTHORIZATION_FAILED", FailureStage: "credential_storage", FailureReason: "CREDENTIAL_VERIFY_FAILED", OfficialCode: 40001, OfficialRequestID: requestID, HTTPStatus: 200,
			}}
			logs := new(bytes.Buffer)
			session := connectTestServer(t, Runtime{AuthorizationRecovery: fixture, LogWriter: logs})
			result := callTestTool(t, session, "get_authorization_session", map[string]any{"channel": "qianchuan", "session_id": strings.Repeat("a", 48)})
			if result.IsError {
				t.Fatal("new diagnostics violated the established output schema")
			}
			payload := decodeStructured[struct {
				Session auth.RecoverySession `json:"session"`
			}](t, result)
			if payload.Session.Status != "failed" || payload.Session.ErrorCode != "AUTHORIZATION_FAILED" || payload.Session.FailureStage != "" {
				t.Fatal("diagnostics changed structured session output")
			}
			encoded, err := json.Marshal(result.Meta["ocean_watch.authorization_failure"])
			if err != nil {
				t.Fatal(err)
			}
			var diagnostic auth.RecoverySession
			if err := json.Unmarshal(encoded, &diagnostic); err != nil {
				t.Fatal(err)
			}
			if diagnostic.FailureStage != "credential_storage" || diagnostic.FailureReason != "CREDENTIAL_VERIFY_FAILED" || diagnostic.OfficialCode != 40001 {
				t.Fatal("MCP did not retain actionable safe diagnostics")
			}
			if requestID == "request-fixture" && diagnostic.OfficialRequestID != requestID {
				t.Fatal("safe official request identifier was lost")
			}
			if len(result.Content) != 2 {
				t.Fatal("MCP caller did not receive diagnostic text")
			}
			all, _ := json.Marshal(result)
			if bytes.Contains(all, []byte("fixture-private-secret")) || bytes.Contains(logs.Bytes(), []byte("fixture-private-secret")) {
				t.Fatal("unsafe request identifier leaked through MCP or logs")
			}
		})
	}
}

func TestAuthorizedUserFailureFitsTheExistingSessionContract(t *testing.T) {
	fixture := diagnosticRecoveryFixture{recoveryFixture: &recoveryFixture{}, view: auth.RecoverySession{
		SessionID: strings.Repeat("a", 48), Channel: "marketing", Operation: "reauthorize", Status: "failed", ExpiresAt: "2026-10-29T01:00:00Z",
		AuthorizationID: "existing-grant", ErrorCode: "AUTHORIZATION_IDENTITY_MISMATCH", FailureStage: "authorized_user", FailureReason: "AUTHORIZATION_IDENTITY_MISMATCH",
	}}
	session := connectTestServer(t, Runtime{AuthorizationRecovery: fixture})
	result := callTestTool(t, session, "get_authorization_session", map[string]any{"channel": "marketing", "session_id": strings.Repeat("a", 48)})
	if result.IsError {
		t.Fatal("identity failure broke the session output schema")
	}
	payload := decodeStructured[struct {
		Session auth.RecoverySession `json:"session"`
	}](t, result)
	if payload.Session.ErrorCode != fixture.view.ErrorCode || payload.Session.FailureStage != "" || payload.Session.CanResume {
		t.Fatal("identity failure changed the structured session boundary")
	}
	encoded, _ := json.Marshal(result.Meta["ocean_watch.authorization_failure"])
	var diagnostic auth.RecoverySession
	if err := json.Unmarshal(encoded, &diagnostic); err != nil {
		t.Fatal(err)
	}
	if diagnostic.FailureStage != "authorized_user" || diagnostic.FailureReason != "AUTHORIZATION_IDENTITY_MISMATCH" {
		t.Fatal("new identity failure was not available through safe diagnostics")
	}
}
