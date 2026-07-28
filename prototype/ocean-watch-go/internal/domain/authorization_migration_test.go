package domain

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPrepareLegacyMarketingAuthorizationBuildsPendingSnapshot(t *testing.T) {
	legacy := map[string]any{
		"app_id":        "fixture-app",
		"secret":        "fixture-secret",
		"access_token":  "fixture-access-token",
		"refresh_token": "fixture-refresh-token",
		"oauth_authorized_accounts": []any{
			map[string]any{
				"account_id": "9000000000000001", "account_role": "ADVERTISER",
				"account_name": "Fixture Advertiser",
			},
			map[string]any{
				"account_id": "9000000000000002", "account_role": "AGENT",
				"future_private_field": "must-not-copy",
			},
		},
		"authorized_advertiser_ids": []any{"9000000000000001", "1000000000000003"},
	}
	plan, err := PrepareLegacyMarketingAuthorization(
		map[string]any{"future": map[string]any{"preserved": true}}, legacy, "fixture_auth",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.CommitAuthorization || !plan.WriteAuthorizationSlot || plan.Result["pending_account_sync"] != true {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if plan.State["generation"] != 1 {
		t.Fatalf("generation = %#v", plan.State["generation"])
	}
	if !reflect.DeepEqual(plan.Credential, map[string]any{
		"access_token": "fixture-access-token", "refresh_token": "fixture-refresh-token",
	}) {
		t.Fatalf("credential projection = %#v", plan.Credential)
	}
	metadata := plan.State["authorizations"].(map[string]any)["fixture_auth"].(map[string]any)
	accounts := metadata["authorized_accounts"].([]any)
	if accounts[1].(map[string]any)["future_private_field"] != nil {
		t.Fatalf("unapproved account field survived: %#v", accounts[1])
	}
	if !reflect.DeepEqual(metadata["advertiser_ids"], []any{"9000000000000001"}) {
		t.Fatalf("advertiser index projection = %#v", metadata["advertiser_ids"])
	}
	if plan.State["future"].(map[string]any)["preserved"] != true {
		t.Fatal("unknown channel-state field was lost")
	}
}

func TestPrepareLegacyMarketingAuthorizationIsIdempotentForJournalID(t *testing.T) {
	legacy := map[string]any{"access_token": "fixture-token"}
	first, err := PrepareLegacyMarketingAuthorization(map[string]any{}, legacy, "fixture_auth")
	if err != nil {
		t.Fatal(err)
	}
	second, err := PrepareLegacyMarketingAuthorization(first.State, legacy, "fixture_auth")
	if err != nil {
		t.Fatal(err)
	}
	if second.CommitAuthorization || second.WriteAuthorizationSlot ||
		second.Result["reason"] != "legacy_authorization_already_migrated" {
		t.Fatalf("unexpected second plan: %#v", second)
	}
	if second.State["generation"] != 1 {
		t.Fatalf("idempotent migration changed generation: %#v", second.State)
	}
}

func TestPrepareLegacyMarketingAuthorizationRejectsLossyIDsAndPreservesNumbers(t *testing.T) {
	for _, accountID := range []any{"01", " 1", json.Number("1.5")} {
		_, err := PrepareLegacyMarketingAuthorization(map[string]any{}, map[string]any{
			"access_token": "fixture-token",
			"oauth_authorized_accounts": []any{map[string]any{
				"account_id": accountID, "account_role": "ADVERTISER",
			}},
		}, "fixture_auth")
		if err == nil {
			t.Fatalf("expected invalid account ID %#v to fail", accountID)
		}
	}
	plan, err := PrepareLegacyMarketingAuthorization(map[string]any{}, map[string]any{
		"access_token": "fixture-token",
		"oauth_authorized_accounts": []any{map[string]any{
			"account_id": "9007199254740993", "account_role": "ADVERTISER",
		}},
	}, "fixture_auth")
	if err != nil {
		t.Fatal(err)
	}
	if plan.State["account_index"].(map[string]any)["9007199254740993"] != "fixture_auth" {
		t.Fatalf("large ID was not preserved: %#v", plan.State)
	}
}
