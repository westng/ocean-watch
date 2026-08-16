package cli

import (
	"bytes"
	"context"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application"
	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
)

func TestAuthStatusAndMappingsKeepCLIContract(t *testing.T) {
	credentials := &authQueryCredentials{values: map[string]map[string]any{
		"oceanengine-app-qianchuan": {"app_id": "fixture-app", "secret": "fixture-secret"},
		"oceanengine-auth-qianchuan-auth-a-r2": {
			"access_token": "fixture-access", "refresh_token": "fixture-refresh",
			"access_token_expires_at":  "2026-08-17T00:00:00Z",
			"refresh_token_expires_at": "2026-09-17T00:00:00Z",
		},
	}}
	authorizations := &authQueryAuthorizations{state: map[string]any{
		"generation": 3,
		"authorizations": map[string]any{"auth-a": map[string]any{
			"token_revision": 2, "pending_account_sync": false,
			"advertiser_ids": []any{"2000000000000001"},
			"authorized_accounts": []any{map[string]any{
				"account_id": "9000000000000001", "account_name": "fixture account",
				"account_role": "ADMIN", "account_type": "SHOP",
				"advertiser_ids": []any{"2000000000000001"},
			}},
		}},
		"account_index":    map[string]any{"9000000000000001": "auth-a"},
		"advertiser_index": map[string]any{"2000000000000001": []any{"auth-a"}},
	}}

	status := runAuthQuery(t, credentials, authorizations, "status", "--channel", "qianchuan", "--advertiser-id", "2000000000000001")
	statusResult := status["result"].(map[string]any)
	if status["mode"] != "authorization_status" || statusResult["channel"] != "qianchuan" ||
		statusResult["has_app_id"] != true || statusResult["has_secret"] != true ||
		statusResult["advertiser_id_authorized"] != true || statusResult["authorization_count"] != float64(1) {
		t.Fatalf("authorization status contract changed: %#v", status)
	}

	mappings := runAuthQuery(t, credentials, authorizations, "mappings", "--channel", "qianchuan", "--advertiser-id", "2000000000000001")
	mappingResult := mappings["result"].(map[string]any)
	if mappings["mode"] != "authorization_mappings" || mappingResult["credential_values_exposed"] != false ||
		mappingResult["mapping_count"] != float64(1) || mappingResult["authorization_count"] != float64(1) {
		t.Fatalf("authorization mappings contract changed: %#v", mappings)
	}
	if credentials.writes != 0 || authorizations.updates != 0 {
		t.Fatalf("authorization queries mutated state: credentials=%d authorizations=%d", credentials.writes, authorizations.updates)
	}
}

func runAuthQuery(
	t *testing.T,
	credentials *authQueryCredentials,
	authorizations *authQueryAuthorizations,
	action string,
	args ...string,
) map[string]any {
	t.Helper()
	stdout := new(bytes.Buffer)
	runner := Runner{
		Stdout: stdout, Credentials: credentials, Routes: application.DefaultRouteManifest(),
		Auth: AuthRuntime{Authorizations: authorizations},
	}
	command := append([]string{"auth", action}, args...)
	if code := runner.Execute(context.Background(), command); code != 0 {
		t.Fatalf("auth %s returned %d: %s", action, code, stdout.String())
	}
	return decodeSingleJSONObject(t, stdout.Bytes())
}

type authQueryCredentials struct {
	values map[string]map[string]any
	writes int
}

func (*authQueryCredentials) BackendName() string { return "fixture" }

func (store *authQueryCredentials) Read(_ context.Context, account string) (map[string]any, error) {
	return store.values[account], nil
}

func (store *authQueryCredentials) Write(context.Context, string, map[string]any) (string, error) {
	store.writes++
	return "fixture", nil
}

type authQueryAuthorizations struct {
	state   map[string]any
	updates int
}

func (store *authQueryAuthorizations) LoadChannel(context.Context, string) (map[string]any, error) {
	return store.state, nil
}

func (store *authQueryAuthorizations) UpdateChannel(context.Context, string, func(map[string]any) error) error {
	store.updates++
	return nil
}

var _ authapplication.CredentialStore = (*authQueryCredentials)(nil)
var _ authapplication.AuthorizationStore = (*authQueryAuthorizations)(nil)
