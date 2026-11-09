package oceanengine

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

// All credentials are fabricated in memory; persistent state is under TempDir.
type marketingFlowCredentials struct {
	mu      sync.Mutex
	entries map[string]map[string]any
}

func (store *marketingFlowCredentials) Read(_ context.Context, account string) (map[string]any, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if value, ok := store.entries[account]; ok {
		return configuration.CloneMap(value), nil
	}
	return nil, errors.New("fixture credential does not exist")
}

func (store *marketingFlowCredentials) Write(_ context.Context, account string, value map[string]any) (string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.entries[account] = configuration.CloneMap(value)
	return "fixture", nil
}

func TestMarketingAuthorizationSavesBeforeDiscoveryAndRecoversTheSameGrant(t *testing.T) {
	state := filesystem.AuthorizationStore{Root: t.TempDir()}
	credentials := &marketingFlowCredentials{entries: map[string]map[string]any{
		"oceanengine-app-marketing": {"app_id": "123", "secret": "fixture-secret"},
	}}
	transport := &discoveryTransport{perRoute: map[string]int{}}
	incomplete, exchanges, users := true, 0, 0
	transport.respond = func(request *http.Request, _ int) (int, string) {
		switch request.URL.Path {
		case "/open_api/oauth2/access_token/":
			exchanges++
			return 200, `{"code":0,"data":{"access_token":"fixture-access","refresh_token":"fixture-refresh","expires_in":3600,"refresh_token_expires_in":86400}}`
		case "/open_api/2/user/info/":
			users++
			return 200, `{"code":0,"data":{"id":7001,"app_id":123}}`
		case "/open_api/oauth2/advertiser/get/":
			saved, err := state.LoadChannel(request.Context(), "marketing")
			if err != nil || configuration.Object(configuration.Object(saved["authorizations"])["fixture-marketing-grant"])["pending_account_sync"] != true {
				t.Error("account discovery began before the durable pending grant was saved")
			}
			if _, err := credentials.Read(request.Context(), "oceanengine-auth-marketing-fixture-marketing-grant-r1"); err != nil {
				t.Error("account discovery began before credential persistence")
			}
			return 200, `{"code":0,"data":{"list":[
				{"account_id":1001,"account_role":"ADVERTISER"},
				{"account_id":9002,"account_role":"CUSTOMER_ADMIN"},
				{"account_id":9005,"account_role":"PLATFORM_ROLE_SHOP_ACCOUNT"},
				{"account_role":"PLATFORM_ROLE_AWEME","account_string_id":"fixture-sec-uid"}
			]}}`
		case "/open_api/2/customer_center/advertiser/list/":
			return 200, `{"code":0,"data":{"list":[{"advertiser_id":"1002"}],"page_info":{"total_page":1}}}`
		case "/open_api/2/advertiser/info/":
			if incomplete {
				return 200, `{"code":0,"data":[{"id":1001}]}`
			}
			return 200, `{"code":0,"data":[{"id":"1001"},{"advertiser_id":"1002"}]}`
		default:
			t.Error("account sync repeated OAuth or used an unexpected endpoint")
			return 404, `{}`
		}
	}
	factory, err := NewClientFactory(FactoryOptions{TransportFactory: func(HostProfile) http.RoundTripper { return transport }})
	if err != nil {
		t.Fatal(err)
	}
	oauth := OAuthAdapter{Factory: factory}
	locks := filesystem.RefreshLocker{StateRoot: state.Root}
	flow := auth.Service{
		Credentials: credentials, OAuth: oauth, Users: oauth,
		Authorizer: auth.Authorizer{Credentials: credentials, Authorizations: state, Locks: locks, NewID: func() (string, error) { return "fixture-marketing-grant", nil }},
		Syncer: auth.AdvertiserSnapshotSync{
			Tokens:         &auth.TokenManager{Credentials: credentials, Authorizations: state, Locks: locks, OAuth: oauth},
			Authorizations: state, Discovery: AdvertiserDiscoveryAdapter{Factory: factory},
		},
	}
	ctx := testRequestContext(t, "marketing")
	input := auth.RecoveryRequest{Channel: "marketing", Operation: "authorize", AdvertiserID: "1002"}
	first := auth.NewRecoveryCoordinator(ctx, input, auth.RecoverySession{Channel: "marketing", Operation: "authorize", Status: "awaiting_callback"}, "AD.fixture-state", time.Minute, flow)
	if err := first.AcceptCallback("AD.fixture-state", "fixture-code", ""); err != nil {
		t.Fatal(err)
	}
	awaitMarketingFlow(t, first)
	failed := first.View()
	if failed.Status != "failed" || !failed.SyncRequired || failed.CanResume || failed.AuthorizationID != "fixture-marketing-grant" ||
		failed.FailureEndpoint != advertiserInfoEndpoint || failed.FailureValidation != "INCOMPLETE_ADVERTISER_INFO" {
		t.Fatalf("incomplete discovery lost its saved grant or actionable failure: %+v", failed)
	}
	saved, err := state.LoadChannel(ctx, "marketing")
	if err != nil || len(configuration.Object(saved["advertiser_index"])) != 0 {
		t.Fatal("incomplete discovery activated advertising scope")
	}
	incomplete = false
	input.Operation, input.AuthorizationID = "sync_accounts", failed.AuthorizationID
	recovery := auth.NewRecoveryCoordinator(ctx, input, auth.RecoverySession{Channel: "marketing", Operation: "sync_accounts", AuthorizationID: failed.AuthorizationID, Status: "awaiting_user"}, "", time.Minute, flow)
	if err := recovery.StartSync(); err != nil {
		t.Fatal(err)
	}
	awaitMarketingFlow(t, recovery)
	completed := recovery.View()
	if completed.Status != "completed" || !completed.CanResume || completed.SyncRequired || completed.AccountCount != 2 || completed.AdvertiserCount != 2 {
		t.Fatalf("saved marketing grant did not recover: %+v", completed)
	}
	current, err := state.ReadChannel(ctx, "marketing")
	if err != nil || current.AuthorizationCount != 1 || !reflect.DeepEqual(current.AdvertiserIDs, []string{"1001", "1002"}) || current.PendingAccountSyncCount != 0 {
		t.Fatal("recovery duplicated the grant or failed to activate its exact advertiser scope")
	}
	if exchanges != 1 || users != 1 || current.Authorizations[0].TokenRevision != 1 || len(current.Authorizations[0].AccountDiscoveryIssues) != 2 {
		t.Fatal("recovery exchanged OAuth again, rotated credentials, or hid skipped roles")
	}
}

func awaitMarketingFlow(t *testing.T, coordinator *auth.RecoveryCoordinator) {
	t.Helper()
	select {
	case <-coordinator.Done():
	case <-time.After(5 * time.Second):
		coordinator.RequestCancel()
		t.Fatal("isolated marketing flow did not finish")
	}
}
