package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/officialmcp"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
)

func TestOfficialProbeUsesExistingScopedTokenLease(t *testing.T) {
	manager := &fixtureProbeLeaser{}
	opened := false
	probe, err := openQianchuanOfficialProbe(context.Background(), manager, fixtureProbeControl{}, "123456789", "234567891",
		func(ctx context.Context, token string, _ requestcontrol.SharedController) (*officialmcp.Probe, error) {
			opened = true
			scope, hasAuthorization := requestcontrol.Authorization(ctx)
			advertiser, hasAdvertiser := requestcontrol.Advertiser(ctx)
			if !hasAuthorization || scope.Channel != "qianchuan" || scope.AuthorizationID != "fixture-authorization" ||
				!hasAdvertiser || advertiser != "123456789" {
				t.Fatal("lease/advertiser context was not passed to the adapter")
			}
			if token != "synthetic-fixture-token" {
				t.Fatal("scoped lease was not passed to the adapter")
			}
			return &officialmcp.Probe{}, nil
		})
	if err != nil || !opened || probe.advertiserID != "123456789" {
		t.Fatalf("scope assembly failed: %v", err)
	}
	if manager.query.Channel != "qianchuan" || manager.query.AdvertiserID != "123456789" ||
		manager.query.AuthAccountID != "234567891" || manager.query.AllowPending || manager.query.ForceRefresh {
		t.Fatalf("authorization scope expanded: %#v", manager.query)
	}
}

func TestOfficialProbeInvalidScopePrecedesTokenAndTransport(t *testing.T) {
	manager := &fixtureProbeLeaser{}
	_, err := openQianchuanOfficialProbe(context.Background(), manager, fixtureProbeControl{}, "123456789/other", "",
		func(context.Context, string, requestcontrol.SharedController) (*officialmcp.Probe, error) {
			t.Fatal("invalid scope opened a transport")
			return nil, nil
		})
	if !errors.Is(err, officialmcp.ErrProbeScope) || manager.called {
		t.Fatal("invalid scope acquired credentials")
	}
}

func TestOfficialProbeAuthorizationFailureIsSafeAndStopsConnection(t *testing.T) {
	manager := &fixtureProbeLeaser{fail: true}
	_, err := openQianchuanOfficialProbe(context.Background(), manager, fixtureProbeControl{}, "123456789", "",
		func(context.Context, string, requestcontrol.SharedController) (*officialmcp.Probe, error) {
			t.Fatal("failed authorization opened a transport")
			return nil, nil
		})
	if !errors.Is(err, ErrOfficialProbeAuthorization) {
		t.Fatalf("unsafe authorization failure: %v", err)
	}
}

type fixtureProbeLeaser struct {
	query        auth.TokenQuery
	called, fail bool
}

func (manager *fixtureProbeLeaser) Ensure(_ context.Context, query auth.TokenQuery) (auth.TokenLease, error) {
	manager.called, manager.query = true, query
	if manager.fail {
		return auth.TokenLease{}, errors.New("synthetic private error")
	}
	return auth.TokenLease{Channel: "qianchuan", AuthorizationID: "fixture-authorization", TokenRevision: 7, AccessToken: "synthetic-fixture-token"}, nil
}

type fixtureProbeControl struct{}

func (fixtureProbeControl) Acquire(context.Context, string) (func(error, *http.Response) error, error) {
	return func(error, *http.Response) error { return nil }, nil
}
