package auth

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

type staticTokenProvider struct {
	lease TokenLease
	err   error
}

func (provider staticTokenProvider) Ensure(context.Context, TokenQuery) (TokenLease, error) {
	return provider.lease, provider.err
}

type staticDiscovery struct {
	snapshot domain.AdvertiserSnapshot
	err      error
}

func (discovery staticDiscovery) Discover(context.Context, string, string) (domain.AdvertiserSnapshot, error) {
	return discovery.snapshot, discovery.err
}

func TestAdvertiserSnapshotTransaction(t *testing.T) {
	now := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	initial := snapshotStateFixture()
	store := &memoryAuthorizationStore{channels: map[string]map[string]any{
		"qianchuan": configuration.CloneMap(initial),
	}}
	snapshot := domain.AdvertiserSnapshot{
		Accounts: []domain.AuthorizedAccount{
			{AccountID: "9000000000000001", AccountType: "ADVERTISER", AdvertiserIDs: []string{"1000000000000001"}},
			{AccountID: "9000000000000002", AccountType: "PLATFORM_ROLE_SHOP_ACCOUNT", AdvertiserIDs: []string{"1000000000000002"}},
		},
		AdvertiserIDs: []string{"1000000000000001", "1000000000000002"},
	}
	syncer := AdvertiserSnapshotSync{
		Tokens: staticTokenProvider{lease: TokenLease{
			Channel: "qianchuan", AuthorizationID: "target", TokenRevision: 1, AccessToken: "fixture-access",
		}},
		Authorizations: store, Discovery: staticDiscovery{snapshot: snapshot},
		Now: func() time.Time { return now },
	}
	result, err := syncer.Sync(context.Background(), AdvertiserSyncQuery{
		Channel: "qianchuan", AuthorizationID: "target",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AuthorizedAccounts != 2 || len(result.AdvertiserIDs) != 2 {
		t.Fatalf("unexpected sync result: %#v", result)
	}
	updated, _ := store.LoadChannel(context.Background(), "qianchuan")
	metadata := configuration.Object(configuration.Object(updated["authorizations"])["target"])
	if metadata["pending_account_sync"] != false || metadata["last_authorized_account_sync_at"] != now.Format(time.RFC3339Nano) {
		t.Fatalf("snapshot metadata was not activated: %#v", metadata)
	}
	if !reflect.DeepEqual(
		configuration.Object(updated["account_index"]),
		map[string]any{"9000000000000001": "target", "9000000000000002": "target"},
	) {
		t.Fatalf("account index = %#v", updated["account_index"])
	}
	advertiserIndex := configuration.Object(updated["advertiser_index"])
	if !reflect.DeepEqual(advertiserIndex["1000000000000002"], []any{"target"}) {
		t.Fatalf("advertiser index = %#v", advertiserIndex)
	}

	beforeFailure := configuration.CloneMap(updated)
	syncer.Discovery = staticDiscovery{err: errors.New("injected middle page failure")}
	if _, err := syncer.Sync(context.Background(), AdvertiserSyncQuery{
		Channel: "qianchuan", AuthorizationID: "target",
	}); err == nil {
		t.Fatal("failed discovery was accepted")
	}
	afterFailure, _ := store.LoadChannel(context.Background(), "qianchuan")
	if !reflect.DeepEqual(beforeFailure, afterFailure) {
		t.Fatalf("failed discovery changed active snapshot:\nbefore=%#v\nafter=%#v", beforeFailure, afterFailure)
	}
}

func TestAdvertiserSnapshotTransactionRequiresExplicitRebind(t *testing.T) {
	state := snapshotStateFixture()
	state["authorizations"].(map[string]any)["other"] = map[string]any{
		"token_revision": 1, "pending_account_sync": false,
		"authorized_accounts": []any{map[string]any{
			"account_id": "9000000000000099", "advertiser_ids": []any{"1000000000000099"},
		}},
		"advertiser_ids": []any{"1000000000000099"},
	}
	state["account_index"].(map[string]any)["9000000000000099"] = "other"
	state["advertiser_index"].(map[string]any)["1000000000000099"] = []any{"other"}
	snapshot := domain.AdvertiserSnapshot{
		Accounts: []domain.AuthorizedAccount{{
			AccountID: "9000000000000099", AdvertiserIDs: []string{"1000000000000099"},
		}},
		AdvertiserIDs: []string{"1000000000000099"},
	}
	before := configuration.CloneMap(state)
	if err := activateAdvertiserSnapshot(state, "target", snapshot, false, "fixture-time"); err == nil {
		t.Fatal("cross-authorization account conflict was accepted")
	}
	if !reflect.DeepEqual(before, state) {
		t.Fatal("rejected conflict mutated the candidate state")
	}
	if err := activateAdvertiserSnapshot(state, "target", snapshot, true, "fixture-time"); err != nil {
		t.Fatal(err)
	}
	other := configuration.Object(state["authorizations"].(map[string]any)["other"])
	if len(anyList(other["authorized_accounts"])) != 0 {
		t.Fatalf("rebound account remained under old authorization: %#v", other)
	}
}

func snapshotStateFixture() map[string]any {
	return map[string]any{
		"generation": 1,
		"authorizations": map[string]any{
			"target": map[string]any{
				"token_revision": 1, "pending_account_sync": true,
				"authorized_accounts": []any{map[string]any{
					"account_id": "9000000000000088", "advertiser_ids": []any{"1000000000000088"},
				}},
				"advertiser_ids": []any{"1000000000000088"},
			},
		},
		"account_index":    map[string]any{"9000000000000088": "target"},
		"advertiser_index": map[string]any{"1000000000000088": []any{"target"}},
	}
}

func TestAdvertiserSnapshotRejectsDiscoveryFromObsoleteCredential(t *testing.T) {
	store := &memoryAuthorizationStore{channels: map[string]map[string]any{"qianchuan": snapshotStateFixture()}}
	var selected map[string]any
	syncer := AdvertiserSnapshotSync{
		Authorizations: store,
		Tokens:         staticTokenProvider{lease: TokenLease{Channel: "qianchuan", AuthorizationID: "target", TokenRevision: 1, AccessToken: "fixture-access"}},
		Discovery: recoveryDiscoveryFunc(func(ctx context.Context) (domain.AdvertiserSnapshot, error) {
			if err := store.UpdateChannel(ctx, "qianchuan", func(state map[string]any) error {
				row := configuration.Object(configuration.Object(state["authorizations"])["target"])
				row["token_revision"], row["pending_account_sync"] = 2, true
				return nil
			}); err != nil {
				return domain.AdvertiserSnapshot{}, err
			}
			selected, _ = store.LoadChannel(ctx, "qianchuan")
			return domain.AdvertiserSnapshot{Accounts: []domain.AuthorizedAccount{{AccountID: "9001", AdvertiserIDs: []string{"1001"}}}, AdvertiserIDs: []string{"1001"}}, nil
		}),
	}
	if _, err := syncer.Sync(context.Background(), AdvertiserSyncQuery{Channel: "qianchuan", AuthorizationID: "target"}); err == nil {
		t.Fatal("obsolete credential discovery activated an account snapshot")
	}
	after, _ := store.LoadChannel(context.Background(), "qianchuan")
	if !reflect.DeepEqual(selected, after) {
		t.Fatal("obsolete discovery replaced the newer pending grant")
	}
}
