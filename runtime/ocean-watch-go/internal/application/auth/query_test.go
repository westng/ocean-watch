package auth

import (
	"context"
	"reflect"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

func TestQueryServiceReadsStatusAndMappingsWithoutCredentialWrites(t *testing.T) {
	for _, channel := range []string{"marketing", "qianchuan", "star_map"} {
		t.Run(channel, func(t *testing.T) {
			appAccount, err := domain.AppCredentialAccount(channel)
			if err != nil {
				t.Fatal(err)
			}
			tokenAccount, err := domain.AuthorizationCredentialAccount(channel, "auth-a", 2)
			if err != nil {
				t.Fatal(err)
			}
			credentials := &queryCredentials{values: map[string]map[string]any{
				appAccount: {"app_id": "fixture-app", "secret": "fixture-secret"},
				tokenAccount: {
					"access_token": "fixture-access", "refresh_token": "fixture-refresh",
					"access_token_expires_at": "2026-08-17T00:00:00Z",
				},
			}}
			authorizations := &queryAuthorizationStore{state: map[string]any{
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
			service := QueryService{Credentials: credentials, Authorizations: authorizations}

			result, err := service.Inspect(context.Background(), StatusQuery{
				Channel: channel, AdvertiserID: "2000000000000001",
			})
			if err != nil {
				t.Fatal(err)
			}
			if !result.Status.HasAppID || !result.Status.HasSecret || result.Status.Generation != 3 ||
				result.Status.AdvertiserIDAuthorized == nil || !*result.Status.AdvertiserIDAuthorized ||
				len(result.Status.Authorizations) != 1 || !result.Status.Authorizations[0].HasAccessToken ||
				result.Status.Authorizations[0].AccessTokenExpiresAt != "2026-08-17T00:00:00Z" {
				t.Fatalf("unexpected status: %#v", result.Status)
			}
			if result.Mappings.CredentialValuesExposed || len(result.Mappings.Mappings) != 1 ||
				result.Mappings.Mappings[0].Ambiguous || len(result.Mappings.Authorizations) != 1 ||
				!reflect.DeepEqual(result.Mappings.Authorizations[0].AdvertiserIDs, []string{"2000000000000001"}) {
				t.Fatalf("unexpected mappings: %#v", result.Mappings)
			}
			if credentials.writes != 0 || authorizations.updates != 0 {
				t.Fatalf("query mutated local state: credential_writes=%d authorization_updates=%d", credentials.writes, authorizations.updates)
			}
		})
	}
}

func TestQueryServiceMappingsDoesNotReadAppCredential(t *testing.T) {
	credentials := &queryCredentials{values: map[string]map[string]any{}}
	service := QueryService{
		Credentials: credentials,
		Authorizations: &queryAuthorizationStore{state: map[string]any{
			"generation": 0, "authorizations": map[string]any{},
			"account_index": map[string]any{}, "advertiser_index": map[string]any{},
		}},
	}
	if _, err := service.Mappings(context.Background(), StatusQuery{Channel: "qianchuan"}); err != nil {
		t.Fatal(err)
	}
	if len(credentials.reads) != 0 {
		t.Fatalf("mappings unexpectedly read credentials: %v", credentials.reads)
	}
}

func TestQueryPendingMappingDoesNotReportUsableAuthorization(t *testing.T) {
	credentials := &queryCredentials{values: map[string]map[string]any{
		"oceanengine-app-marketing":           {"app_id": "123", "secret": "fixture-secret"},
		"oceanengine-auth-marketing-grant-r2": {"access_token": "fixture-access", "refresh_token": "fixture-refresh"},
	}}
	store := &queryAuthorizationStore{state: map[string]any{
		"authorizations": map[string]any{"grant": map[string]any{
			"token_revision": 2, "pending_account_sync": true, "app_id": "123", "authorized_user_id": "7001",
			"authorized_accounts": []any{map[string]any{"account_id": "9001", "advertiser_ids": []any{"1001"}}}, "advertiser_ids": []any{"1001"},
		}},
		"account_index": map[string]any{"9001": "grant"}, "advertiser_index": map[string]any{"1001": []any{"grant"}},
	}}
	inspection, err := (QueryService{Credentials: credentials, Authorizations: store}).Inspect(context.Background(), StatusQuery{Channel: "marketing", AdvertiserID: "1001"})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Status.AdvertiserIDAuthorized == nil || *inspection.Status.AdvertiserIDAuthorized || !inspection.Status.Authorizations[0].PendingAccountSync {
		t.Fatal("retained account scope was reported as usable while pending")
	}
	row := inspection.Mappings.Authorizations[0]
	if row.AppID != "123" || row.AuthorizedUserID != "7001" || !row.PendingAccountSync || !reflect.DeepEqual(row.AdvertiserIDs, []string{"1001"}) {
		t.Fatalf("pending identity or previous mapping was lost: %+v", row)
	}
	if credentials.writes != 0 || store.updates != 0 {
		t.Fatal("inspection modified authorization state")
	}
}

type queryCredentials struct {
	values map[string]map[string]any
	reads  []string
	writes int
}

func (store *queryCredentials) Read(_ context.Context, account string) (map[string]any, error) {
	store.reads = append(store.reads, account)
	return store.values[account], nil
}

func (store *queryCredentials) Write(context.Context, string, map[string]any) (string, error) {
	store.writes++
	return "fixture", nil
}

type queryAuthorizationStore struct {
	state   map[string]any
	updates int
}

func (store *queryAuthorizationStore) LoadChannel(context.Context, string) (map[string]any, error) {
	return store.state, nil
}

func (store *queryAuthorizationStore) UpdateChannel(context.Context, string, func(map[string]any) error) error {
	store.updates++
	return nil
}
