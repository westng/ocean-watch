package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/f2config"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type f2ConfigurationFixture struct {
	starts, gets, cancels int
	sessionMissing        bool
	configurationStatus   f2config.Status
}

func (fixture *f2ConfigurationFixture) Start(context.Context) (f2config.Session, error) {
	fixture.starts++
	id := strings.Repeat("a", 48)
	return f2config.Session{SessionID: id, Status: "awaiting_user", ConfigurationURL: "http://127.0.0.1:12345/f2/configure?session=" + id, ExpiresAt: "2026-10-30T02:00:00Z", Backend: "macos-keychain"}, nil
}
func (fixture *f2ConfigurationFixture) Get(ctx context.Context, id string) (f2config.Session, error) {
	fixture.gets++
	if fixture.sessionMissing {
		return f2config.Session{}, domain.NewError("f2_configuration_session_not_found", "fixture-secret", 2, nil)
	}
	view, _ := fixture.Start(ctx)
	fixture.starts--
	view.ConfigurationURL = ""
	view.Status = "completed"
	view.CredentialSaved = true
	view.CanResume = true
	return view, nil
}
func (fixture *f2ConfigurationFixture) Status(context.Context) f2config.Status {
	return fixture.configurationStatus
}

func TestF2MissingSessionReportsIndependentVaultStatus(t *testing.T) {
	for _, configured := range []bool{true, false} {
		fixture := &f2ConfigurationFixture{sessionMissing: true, configurationStatus: f2config.Status{Configured: configured, Backend: "macos-keychain"}}
		session := connectTestServer(t, Runtime{F2Configuration: fixture})
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_f2_configuration_session", Arguments: map[string]any{"session_id": strings.Repeat("a", 48)}})
		if err != nil {
			t.Fatal(err)
		}
		data := decodeStructured[struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
				Details struct {
					Status       f2config.Status `json:"configuration_status"`
					NextAction   string          `json:"next_action"`
					RecoveryTool string          `json:"recovery_tool"`
				} `json:"details"`
			} `json:"error"`
		}](t, result)
		if !result.IsError || data.Error.Code != "F2_CONFIGURATION_SESSION_NOT_FOUND" || data.Error.Details.Status.Configured != configured || data.Error.Details.Status.OnlineVerified {
			t.Fatalf("missing safe vault status: %+v", data)
		}
		if configured && (data.Error.Details.NextAction != "retry_original_request" || data.Error.Details.RecoveryTool != "") {
			t.Fatal("saved credential must not trigger reconfiguration")
		}
		if !configured && data.Error.Details.RecoveryTool != "start_f2_configuration" {
			t.Fatal("missing credential must retain configuration recovery")
		}
		encoded, _ := json.Marshal(result)
		if strings.Contains(string(encoded), "fixture-secret") || strings.Contains(data.Error.Message, "本机 F2 配置不可用") {
			t.Fatal("session error leaked data or claimed a vault failure")
		}
	}
}
func (fixture *f2ConfigurationFixture) Cancel(ctx context.Context, id string) (f2config.Session, error) {
	fixture.cancels++
	view, _ := fixture.Get(ctx, id)
	fixture.gets--
	view.Status = "cancelled"
	view.CredentialSaved = false
	view.CanResume = false
	return view, nil
}

func TestF2MCPOnlyExchangesSafeSessionData(t *testing.T) {
	fixture, logs := &f2ConfigurationFixture{}, new(bytes.Buffer)
	session := connectTestServer(t, Runtime{F2Configuration: fixture, LogWriter: logs})
	for _, test := range []struct {
		name      string
		arguments map[string]any
	}{
		{"start_f2_configuration", map[string]any{"cookie": "sessionid=fixture-secret"}},
		{"start_f2_configuration", map[string]any{"port": 8787}},
		{"start_f2_configuration", map[string]any{"session_id": strings.Repeat("a", 48)}},
		{"get_f2_configuration_session", map[string]any{"session_id": strings.Repeat("a", 48), "cookie": "fixture-secret"}},
		{"get_f2_configuration_session", map[string]any{"session_id": "bad"}},
	} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: test.name, Arguments: test.arguments})
		if err == nil && !result.IsError {
			t.Fatal("invalid configuration input accepted")
		}
	}
	if fixture.starts != 0 || fixture.gets != 0 {
		t.Fatal("invalid input reached configuration service")
	}
	result := callTestTool(t, session, "start_f2_configuration", map[string]any{})
	view := decodeStructured[struct {
		Session f2config.Session `json:"session"`
	}](t, result).Session
	if result.IsError || view.ConfigurationURL == "" || fixture.starts != 1 {
		t.Fatal("configuration did not start")
	}
	for _, name := range []string{"get_f2_configuration_session", "cancel_f2_configuration_session"} {
		result = callTestTool(t, session, name, map[string]any{"session_id": view.SessionID})
		if result.IsError {
			t.Fatal("session operation failed")
		}
		encoded, _ := json.Marshal(result)
		if strings.Contains(string(encoded), "fixture-secret") {
			t.Fatal("credential exposed")
		}
	}
	if strings.Contains(logs.String(), "fixture-secret") || strings.Contains(logs.String(), "127.0.0.1:") {
		t.Fatal("private input or configuration URL logged")
	}
}

func TestF2MissingCookieHasConfigurationRecovery(t *testing.T) {
	for _, code := range []string{"f2_cookie_missing", "f2_cookie_invalid", "f2_credential_unavailable"} {
		failure := mapQianchuanPreflightError(domain.NewWorkLinkError(code, "fixture"))
		if failure.Code != "F2_CONFIGURATION_REQUIRED" || failure.Retryable || failure.Details["recovery_tool"] != "start_f2_configuration" {
			t.Fatalf("missing recovery for %s: %+v", code, failure)
		}
	}
}
