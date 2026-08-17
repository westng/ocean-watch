package domain

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestResolveAuthorizationUsesAdvertiserIndexAndRevision(t *testing.T) {
	state := authorizationFixture()
	binding, err := ResolveAuthorization("marketing", state, "1000000000000002", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if binding.AuthorizationID != "auth_two" || binding.TokenRevision != 3 ||
		binding.CredentialAccount != "oceanengine-auth-marketing-auth_two-r3" {
		t.Fatalf("unexpected binding: %#v", binding)
	}
	if !reflect.DeepEqual(binding.AccountIDs, []string{"9000000000000002"}) ||
		!reflect.DeepEqual(binding.AdvertiserIDs, []string{"1000000000000002"}) {
		t.Fatalf("unexpected binding scope: %#v", binding)
	}
}

func TestResolveAuthorizationSupportsAccountAndExplicitSelection(t *testing.T) {
	state := authorizationFixture()
	for _, test := range []struct {
		accountID       string
		authorizationID string
		want            string
	}{
		{accountID: "9000000000000001", want: "auth_one"},
		{authorizationID: "auth_two", want: "auth_two"},
	} {
		binding, err := ResolveAuthorization("qianchuan", state, "", test.accountID, test.authorizationID, false)
		if err != nil {
			t.Fatal(err)
		}
		if binding.AuthorizationID != test.want || binding.Channel != "qianchuan" {
			t.Fatalf("binding = %#v, want %s", binding, test.want)
		}
	}
}

func TestResolveAuthorizationRejectsAmbiguousPendingAndInvalidRevision(t *testing.T) {
	state := authorizationFixture()
	state["advertiser_index"].(map[string]any)["1000000000000001"] = []any{"auth_one", "auth_two"}
	if _, err := ResolveAuthorization("marketing", state, "1000000000000001", "", "", false); err == nil || err.Code != "authorization_ambiguous" {
		t.Fatalf("expected ambiguity, got %#v", err)
	}

	metadata := state["authorizations"].(map[string]any)["auth_one"].(map[string]any)
	metadata["pending_account_sync"] = true
	state["advertiser_index"].(map[string]any)["1000000000000001"] = []any{"auth_one"}
	if _, err := ResolveAuthorization("marketing", state, "1000000000000001", "", "", false); err == nil || err.Code != "authorization_pending_sync" {
		t.Fatalf("expected pending failure, got %#v", err)
	}
	if _, err := ResolveAuthorization("marketing", state, "1000000000000001", "", "", true); err != nil {
		t.Fatalf("allow pending failed: %v", err)
	}

	metadata["pending_account_sync"] = false
	metadata["token_revision"] = json.Number("0")
	if _, err := ResolveAuthorization("marketing", state, "1000000000000001", "", "", false); err == nil || err.Code != "authorization_state_invalid" {
		t.Fatalf("expected revision failure, got %#v", err)
	}
}

func TestResolveAuthorizationNeverFallsAcrossChannels(t *testing.T) {
	state := authorizationFixture()
	if _, err := ResolveAuthorization("unknown", state, "1000000000000001", "", "", false); err == nil || err.Code != "unknown_channel" {
		t.Fatalf("expected channel failure, got %#v", err)
	}
	account, err := AuthorizationCredentialAccount("qianchuan", "auth_one", 2)
	if err != nil || account != "oceanengine-auth-qianchuan-auth_one-r2" {
		t.Fatalf("credential account = %q, %v", account, err)
	}
}

func authorizationFixture() map[string]any {
	return map[string]any{
		"generation": json.Number("4"),
		"authorizations": map[string]any{
			"auth_one": map[string]any{
				"token_revision":       json.Number("1"),
				"pending_account_sync": false,
				"advertiser_ids":       []any{"1000000000000001"},
				"authorized_accounts": []any{map[string]any{
					"account_id": "9000000000000001", "advertiser_ids": []any{"1000000000000001"},
				}},
			},
			"auth_two": map[string]any{
				"token_revision":       json.Number("3"),
				"pending_account_sync": false,
				"advertiser_ids":       []any{"1000000000000002"},
				"authorized_accounts": []any{map[string]any{
					"account_id": "9000000000000002", "advertiser_ids": []any{"1000000000000002"},
				}},
			},
		},
		"account_index": map[string]any{
			"9000000000000001": "auth_one", "9000000000000002": "auth_two",
		},
		"advertiser_index": map[string]any{
			"1000000000000001": []any{"auth_one"}, "1000000000000002": []any{"auth_two"},
		},
	}
}
