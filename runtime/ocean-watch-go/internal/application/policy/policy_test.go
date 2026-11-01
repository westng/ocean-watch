package policy

import (
	"context"
	"errors"
	auth "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/contracts"
	"testing"
)

func TestPolicySnapshotsConfigurationAndRejectsEscalation(t *testing.T) {
	config := Config{Mode: contracts.QueryModeRead, Toolsets: []string{"core", "qianchuan"}, AllowedSubjects: map[string]map[string][]string{"qianchuan": {"advertiser": {"123"}}}}
	instance, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	config.Toolsets[1] = "reports"
	config.AllowedSubjects["qianchuan"]["advertiser"][0] = "999"
	if instance.CheckCapability("qianchuan.account_health") != nil || instance.CheckSubject("qianchuan", "advertiser", "123") != nil {
		t.Fatal("caller mutated startup policy")
	}
	for _, id := range []string{"qianchuan.custom_report", "qianchuan.work_preflight", "qianchuan.plan_update_budget"} {
		if instance.CheckCapability(id) == nil {
			t.Fatalf("unexpected permission %s", id)
		}
	}
	for _, kind := range []string{"advertiser", "organization", "authorization"} {
		if instance.CheckSubject("qianchuan", kind, "999") == nil {
			t.Fatal("unbounded or foreign subject accepted")
		}
	}
	if instance.Check([]string{"unknown"}, contracts.QueryModeRead) == nil || (Instance{}).Check(nil, contracts.QueryModeRead) == nil {
		t.Fatal("unknown policy failed open")
	}
}
func TestPolicyRejectsUnknownConfiguration(t *testing.T) {
	for _, raw := range []map[string]any{{"business_policy": map[string]any{"mode": "write", "toolsets": []string{"core"}}}, {"business_policy": map[string]any{"mode": "read", "toolsets": []string{"unknown"}}}, {"business_policy": map[string]any{"mode": "read", "toolsets": []string{"core"}, "grant": true}}} {
		if _, err := FromConfig(raw); err == nil {
			t.Fatal("unknown config accepted")
		}
	}
}

type countTokens struct {
	count int
	query auth.TokenQuery
	lease auth.TokenLease
}

func (p *countTokens) Ensure(_ context.Context, query auth.TokenQuery) (auth.TokenLease, error) {
	p.count++
	p.query = query
	return p.lease, nil
}
func TestSDKTokenGuardDeniesBeforeProvider(t *testing.T) {
	instance, _ := New(Config{Mode: contracts.QueryModeRead, Toolsets: []string{"qianchuan"}, AllowedSubjects: map[string]map[string][]string{"qianchuan": {"advertiser": {"123"}}}})
	provider := &countTokens{}
	guard := Tokens{Instance: instance, Provider: provider, Required: []string{"qianchuan"}}
	if _, err := guard.Ensure(context.Background(), auth.TokenQuery{Channel: "qianchuan", AdvertiserID: "999"}); err == nil || provider.count != 0 {
		t.Fatal("foreign SDK read reached TokenManager")
	}
	guard.Required = []string{"reports"}
	if _, err := guard.Ensure(context.Background(), auth.TokenQuery{Channel: "qianchuan", AdvertiserID: "123"}); err == nil || provider.count != 0 {
		t.Fatal("disabled reports reached TokenManager")
	}
}

type guardAuthorizationStore struct {
	state map[string]any
	loads int
}

func (store *guardAuthorizationStore) LoadChannel(context.Context, string) (map[string]any, error) {
	store.loads++
	return store.state, nil
}
func (*guardAuthorizationStore) UpdateChannel(context.Context, string, func(map[string]any) error) error {
	return errors.New("unexpected authorization mutation")
}

func TestSDKGrantRestrictionResolvesBeforeTokenRefresh(t *testing.T) {
	instance, _ := New(Config{Mode: "read", Toolsets: []string{"core", "qianchuan"}, AllowedSubjects: map[string]map[string][]string{"qianchuan": {"advertiser": {"123"}, "authorization": {"allowed-grant"}}}})
	store := &guardAuthorizationStore{state: map[string]any{"authorizations": map[string]any{"foreign-grant": map[string]any{"advertiser_ids": []any{"123"}, "token_revision": 1}}, "advertiser_index": map[string]any{"123": []any{"foreign-grant"}}}}
	provider := &countTokens{lease: auth.TokenLease{AuthorizationID: "allowed-grant"}}
	guard := Tokens{Instance: instance, Provider: provider, Required: []string{"qianchuan"}, Authorizations: store}
	query := auth.TokenQuery{Channel: "qianchuan", AdvertiserID: "123"}
	if _, err := guard.Ensure(context.Background(), query); !errors.Is(err, ErrDenied) || provider.count != 0 || store.loads != 1 {
		t.Fatal("resolved foreign grant reached TokenManager")
	}
	store.state = map[string]any{"authorizations": map[string]any{"allowed-grant": map[string]any{"advertiser_ids": []any{"123"}, "token_revision": 1}}, "advertiser_index": map[string]any{"123": []any{"allowed-grant"}}}
	if _, err := guard.Ensure(context.Background(), query); err != nil || provider.count != 1 || provider.query.AuthorizationID != "allowed-grant" {
		t.Fatalf("allowed grant was not pinned for TokenManager: error=%v calls=%d", err, provider.count)
	}
	provider.lease.AuthorizationID = "foreign-grant"
	query.AuthorizationID = "allowed-grant"
	if _, err := guard.Ensure(context.Background(), query); !errors.Is(err, ErrDenied) {
		t.Fatal("provider replaced pinned grant")
	}
	before := provider.count
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := guard.Ensure(ctx, query); !errors.Is(err, context.Canceled) || provider.count != before {
		t.Fatal("cancelled guard touched TokenManager")
	}
}
