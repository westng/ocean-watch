package discovery

import (
	"context"
	"errors"
	"reflect"
	"testing"

	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	domainmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/marketing"
	portmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/marketing"
)

const (
	discoveryFixtureAdvertiserID = "1000000000000001"
	discoveryFixtureAuthAccount  = "9000000000000001"
	discoveryFixtureHighID       = "9007199254740993"
	discoveryFixtureToken        = "TEST_MARKETING_DISCOVERY_TOKEN_DO_NOT_USE"
)

type discoveryTokenSpy struct {
	queries []authapplication.TokenQuery
	err     error
}

func (spy *discoveryTokenSpy) Ensure(
	_ context.Context,
	query authapplication.TokenQuery,
) (authapplication.TokenLease, error) {
	spy.queries = append(spy.queries, query)
	if spy.err != nil {
		return authapplication.TokenLease{}, spy.err
	}
	return authapplication.TokenLease{
		Channel: "marketing", AuthorizationID: "fixture-authorization", AccessToken: discoveryFixtureToken,
	}, nil
}

type discoveryReaderStub struct {
	projectRequests   []portmarketing.ProjectDiscoveryRequest
	promotionRequests []portmarketing.PromotionDiscoveryRequest
	dpaRequests       []portmarketing.DPADiscoveryRequest
	eventRequests     []portmarketing.EventDiscoveryRequest
	deepBidRequests   []portmarketing.DeepBidDiscoveryRequest
	goalRequests      []portmarketing.GoalDiscoveryRequest
	adminRequests     []portmarketing.AdminDiscoveryRequest
	project           func(portmarketing.ProjectDiscoveryRequest) (domainmarketing.DiscoveryEnvelope, error)
	promotion         func(portmarketing.PromotionDiscoveryRequest) (domainmarketing.DiscoveryEnvelope, error)
	dpa               func(portmarketing.DPADiscoveryRequest) (domainmarketing.DiscoveryEnvelope, error)
	event             func(portmarketing.EventDiscoveryRequest) (domainmarketing.DiscoveryEnvelope, error)
	deepBid           func(portmarketing.DeepBidDiscoveryRequest) (domainmarketing.DiscoveryEnvelope, error)
	goal              func(portmarketing.GoalDiscoveryRequest) (domainmarketing.DiscoveryEnvelope, error)
	admin             func(portmarketing.AdminDiscoveryRequest) (domainmarketing.AdminEnvelope, error)
}

func (stub *discoveryReaderStub) FetchProjects(
	_ context.Context,
	request portmarketing.ProjectDiscoveryRequest,
) (domainmarketing.DiscoveryEnvelope, error) {
	stub.projectRequests = append(stub.projectRequests, request)
	if stub.project == nil {
		return domainmarketing.DiscoveryEnvelope{}, errors.New("unexpected project discovery request")
	}
	return stub.project(request)
}

func (stub *discoveryReaderStub) FetchPromotions(
	_ context.Context,
	request portmarketing.PromotionDiscoveryRequest,
) (domainmarketing.DiscoveryEnvelope, error) {
	stub.promotionRequests = append(stub.promotionRequests, request)
	if stub.promotion == nil {
		return domainmarketing.DiscoveryEnvelope{}, errors.New("unexpected promotion discovery request")
	}
	return stub.promotion(request)
}

func (stub *discoveryReaderStub) FetchDPA(
	_ context.Context,
	request portmarketing.DPADiscoveryRequest,
) (domainmarketing.DiscoveryEnvelope, error) {
	stub.dpaRequests = append(stub.dpaRequests, request)
	if stub.dpa == nil {
		return domainmarketing.DiscoveryEnvelope{}, errors.New("unexpected DPA discovery request")
	}
	return stub.dpa(request)
}

func (stub *discoveryReaderStub) FetchEvents(
	_ context.Context,
	request portmarketing.EventDiscoveryRequest,
) (domainmarketing.DiscoveryEnvelope, error) {
	stub.eventRequests = append(stub.eventRequests, request)
	if stub.event == nil {
		return domainmarketing.DiscoveryEnvelope{}, errors.New("unexpected event discovery request")
	}
	return stub.event(request)
}

func (stub *discoveryReaderStub) FetchDeepBids(
	_ context.Context,
	request portmarketing.DeepBidDiscoveryRequest,
) (domainmarketing.DiscoveryEnvelope, error) {
	stub.deepBidRequests = append(stub.deepBidRequests, request)
	if stub.deepBid == nil {
		return domainmarketing.DiscoveryEnvelope{}, errors.New("unexpected deep-bid discovery request")
	}
	return stub.deepBid(request)
}

func (stub *discoveryReaderStub) FetchGoals(
	_ context.Context,
	request portmarketing.GoalDiscoveryRequest,
) (domainmarketing.DiscoveryEnvelope, error) {
	stub.goalRequests = append(stub.goalRequests, request)
	if stub.goal == nil {
		return domainmarketing.DiscoveryEnvelope{}, errors.New("unexpected goal discovery request")
	}
	return stub.goal(request)
}

func (stub *discoveryReaderStub) FetchAdminInfo(
	_ context.Context,
	request portmarketing.AdminDiscoveryRequest,
) (domainmarketing.AdminEnvelope, error) {
	stub.adminRequests = append(stub.adminRequests, request)
	if stub.admin == nil {
		return domainmarketing.AdminEnvelope{}, errors.New("unexpected admin discovery request")
	}
	return stub.admin(request)
}

func (stub *discoveryReaderStub) callCount() int {
	return len(stub.projectRequests) + len(stub.promotionRequests) + len(stub.dpaRequests) +
		len(stub.eventRequests) + len(stub.deepBidRequests) + len(stub.goalRequests) + len(stub.adminRequests)
}

func discoveryEnvelope(endpoint string) domainmarketing.DiscoveryEnvelope {
	return domainmarketing.DiscoveryEnvelope{
		Code: 0, Message: "OK", RequestID: endpoint + "-request",
		Response: map[string]any{"code": 0, "message": "OK", "data": map[string]any{}},
		PageInfo: &domainmarketing.PageInfo{Page: 1, PageSize: 20, TotalPages: 1, TotalNumber: 1},
	}
}

func TestMarketingDiscoveryValidationStopsBeforeTokenResolution(t *testing.T) {
	tests := []struct {
		name string
		run  func(Service) error
	}{
		{
			name: "project enum",
			run: func(service Service) error {
				_, err := service.QueryProjects(context.Background(), ProjectQuery{
					CredentialScope: CredentialScope{AdvertiserID: discoveryFixtureAdvertiserID},
					LandingType:     "NOT_REAL",
				})
				return err
			},
		},
		{
			name: "promotion ID",
			run: func(service Service) error {
				_, err := service.QueryPromotions(context.Background(), PromotionQuery{
					CredentialScope: CredentialScope{AdvertiserID: discoveryFixtureAdvertiserID},
					PromotionIDs:    []string{"not-an-id"},
				})
				return err
			},
		},
		{
			name: "DPA mode",
			run: func(service Service) error {
				_, err := service.QueryDPA(context.Background(), DPAQuery{
					CredentialScope: CredentialScope{AdvertiserID: discoveryFixtureAdvertiserID},
					Mode:            "other", PlatformID: discoveryFixtureHighID,
				})
				return err
			},
		},
		{
			name: "event page",
			run: func(service Service) error {
				_, err := service.QueryEvents(context.Background(), EventQuery{
					CredentialScope: CredentialScope{AdvertiserID: discoveryFixtureAdvertiserID},
					Page:            -1,
				})
				return err
			},
		},
		{
			name: "deep bid action",
			run: func(service Service) error {
				_, err := service.QueryDeepBids(context.Background(), DeepBidQuery{
					CredentialScope: CredentialScope{AdvertiserID: discoveryFixtureAdvertiserID},
				})
				return err
			},
		},
		{
			name: "goal required fields",
			run: func(service Service) error {
				_, err := service.QueryGoals(context.Background(), GoalQuery{
					CredentialScope: CredentialScope{AdvertiserID: discoveryFixtureAdvertiserID},
				})
				return err
			},
		},
		{
			name: "admin code",
			run: func(service Service) error {
				_, err := service.QueryAdmin(context.Background(), AdminQuery{
					CredentialScope: CredentialScope{AdvertiserID: discoveryFixtureAdvertiserID},
				})
				return err
			},
		},
		{
			name: "city names",
			run: func(service Service) error {
				_, err := service.ResolveCities(context.Background(), CityQuery{
					CredentialScope: CredentialScope{AdvertiserID: discoveryFixtureAdvertiserID},
					CityCSV:         "cities.csv", CountryCodes: []string{"CN"},
				})
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tokens := &discoveryTokenSpy{}
			reader := &discoveryReaderStub{}
			if err := test.run(Service{Tokens: tokens, Reader: reader}); err == nil {
				t.Fatal("invalid discovery query was accepted")
			}
			if len(tokens.queries) != 0 || reader.callCount() != 0 {
				t.Fatalf("invalid discovery crossed boundary: tokens=%d calls=%d", len(tokens.queries), reader.callCount())
			}
		})
	}
}

func TestMarketingDiscoveryQueriesPreserveDiagnosticsAndScope(t *testing.T) {
	tokens := &discoveryTokenSpy{}
	reader := &discoveryReaderStub{
		project: func(request portmarketing.ProjectDiscoveryRequest) (domainmarketing.DiscoveryEnvelope, error) {
			if request.LandingType != "SHOP" || request.MarketingGoal != "VIDEO_AND_IMAGE" ||
				request.DeliveryMode != "PROCEDURAL" || request.Page != 1 || request.PageSize != DefaultPageSize {
				t.Fatalf("project defaults changed: %#v", request)
			}
			return discoveryEnvelope("project"), nil
		},
		promotion: func(request portmarketing.PromotionDiscoveryRequest) (domainmarketing.DiscoveryEnvelope, error) {
			if request.ProjectID != discoveryFixtureHighID ||
				!reflect.DeepEqual(request.PromotionIDs, []string{discoveryFixtureHighID}) {
				t.Fatalf("promotion IDs changed: %#v", request)
			}
			return discoveryEnvelope("promotion"), nil
		},
		dpa: func(request portmarketing.DPADiscoveryRequest) (domainmarketing.DiscoveryEnvelope, error) {
			if request.Mode != "asset-detail" || request.PlatformID != discoveryFixtureHighID ||
				request.UniqueProductID != discoveryFixtureHighID {
				t.Fatalf("DPA request changed: %#v", request)
			}
			return discoveryEnvelope("dpa"), nil
		},
		event: func(request portmarketing.EventDiscoveryRequest) (domainmarketing.DiscoveryEnvelope, error) {
			if request.AssetType != "THIRD_EXTERNAL" ||
				!reflect.DeepEqual(request.AssetIDs, []string{discoveryFixtureHighID}) ||
				request.PageSize != DefaultEventPageSize {
				t.Fatalf("event request changed: %#v", request)
			}
			return discoveryEnvelope("event"), nil
		},
		deepBid: func(request portmarketing.DeepBidDiscoveryRequest) (domainmarketing.DiscoveryEnvelope, error) {
			if request.AssetID != discoveryFixtureHighID || request.ExternalAction != "AD_CONVERT_TYPE_APP_ORDER" ||
				request.ProductSetting != "SINGLE" {
				t.Fatalf("deep-bid request changed: %#v", request)
			}
			return discoveryEnvelope("deep-bid"), nil
		},
		goal: func(request portmarketing.GoalDiscoveryRequest) (domainmarketing.DiscoveryEnvelope, error) {
			if request.AssetID != discoveryFixtureHighID || !request.IncludeAsset ||
				request.LandingType != "SHOP" || request.AdType != "ALL" || request.DeliveryType != "NORMAL" {
				t.Fatalf("goal request changed: %#v", request)
			}
			envelope := discoveryEnvelope("goal")
			envelope.Response = map[string]any{"code": 0, "data": map[string]any{
				"goals": []any{map[string]any{"external_action": "AD_CONVERT_TYPE_APP_ORDER"}},
			}}
			return envelope, nil
		},
	}
	scope := CredentialScope{AdvertiserID: discoveryFixtureAdvertiserID, AuthAccountID: discoveryFixtureAuthAccount}
	tests := []struct {
		name     string
		endpoint string
		run      func(Service) (Result, error)
	}{
		{
			name: "projects", endpoint: ProjectEndpoint,
			run: func(service Service) (Result, error) {
				return service.QueryProjects(context.Background(), ProjectQuery{CredentialScope: scope})
			},
		},
		{
			name: "promotions", endpoint: PromotionEndpoint,
			run: func(service Service) (Result, error) {
				return service.QueryPromotions(context.Background(), PromotionQuery{
					CredentialScope: scope, ProjectID: discoveryFixtureHighID,
					PromotionIDs: []string{discoveryFixtureHighID, discoveryFixtureHighID},
				})
			},
		},
		{
			name: "DPA", endpoint: DPAAssetDetailEndpoint,
			run: func(service Service) (Result, error) {
				return service.QueryDPA(context.Background(), DPAQuery{
					CredentialScope: scope, Mode: "asset-detail", PlatformID: discoveryFixtureHighID,
					UniqueProductID: discoveryFixtureHighID,
				})
			},
		},
		{
			name: "events", endpoint: EventEndpoint,
			run: func(service Service) (Result, error) {
				return service.QueryEvents(context.Background(), EventQuery{
					CredentialScope: scope, AssetIDs: []string{discoveryFixtureHighID, discoveryFixtureHighID},
				})
			},
		},
		{
			name: "deep bids", endpoint: DeepBidEndpoint,
			run: func(service Service) (Result, error) {
				return service.QueryDeepBids(context.Background(), DeepBidQuery{
					CredentialScope: scope, AssetID: discoveryFixtureHighID,
					ExternalAction: "AD_CONVERT_TYPE_APP_ORDER",
				})
			},
		},
		{
			name: "goals", endpoint: GoalEndpoint,
			run: func(service Service) (Result, error) {
				return service.QueryGoals(context.Background(), GoalQuery{
					CredentialScope: scope, LandingType: "SHOP", AdType: "ALL",
					AssetID: discoveryFixtureHighID, IncludeAsset: true,
				})
			},
		},
	}
	service := Service{Tokens: tokens, Reader: reader}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.run(service)
			if err != nil {
				t.Fatal(err)
			}
			if result.Endpoint != test.endpoint || result.ResponseCode != 0 || result.ResponseMessage != "OK" ||
				result.RequestID == "" || result.Response["code"] != 0 {
				t.Fatalf("diagnostic contract changed: %#v", result)
			}
		})
	}
	if len(tokens.queries) != len(tests) {
		t.Fatalf("token calls = %d, want %d", len(tokens.queries), len(tests))
	}
	for _, query := range tokens.queries {
		if query.Channel != "marketing" || query.AdvertiserID != discoveryFixtureAdvertiserID ||
			query.AuthAccountID != discoveryFixtureAuthAccount {
			t.Fatalf("token scope changed: %#v", query)
		}
	}
}

func TestMarketingCityResolutionUsesOneTokenForMultipleCountryCodes(t *testing.T) {
	tokens := &discoveryTokenSpy{}
	reader := &discoveryReaderStub{admin: func(request portmarketing.AdminDiscoveryRequest) (domainmarketing.AdminEnvelope, error) {
		if request.AccessToken != discoveryFixtureToken || len(request.Codes) != 1 {
			t.Fatalf("admin credential request changed: %#v", request)
		}
		nodes := []domainmarketing.AdminNode{{Name: "北京市", Code: "11"}}
		if request.Codes[0] == "CHN" {
			nodes = append(nodes, domainmarketing.AdminNode{Name: "上海市", Code: "31"})
		}
		return domainmarketing.AdminEnvelope{
			Code: 0, Message: "OK", RequestID: "admin-" + request.Codes[0],
			Response: map[string]any{"code": 0, "data": map[string]any{}}, Nodes: nodes,
		}, nil
	}}
	result, err := (Service{Tokens: tokens, Reader: reader}).ResolveCities(context.Background(), CityQuery{
		CredentialScope: CredentialScope{
			AdvertiserID: discoveryFixtureAdvertiserID, AuthAccountID: discoveryFixtureAuthAccount,
		},
		CityCSV: "cities.csv", CityNames: []string{"北京", "上海"},
		CountryCodes: []string{"CN", "CN", "CHN", "156"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens.queries) != 1 || len(reader.adminRequests) != 2 || result.BestCountryCode != "CHN" ||
		result.ResolvedCount != 2 || len(result.Missing) != 0 ||
		!reflect.DeepEqual(result.Resolved, []ResolvedCity{{Name: "北京", Code: "11"}, {Name: "上海", Code: "31"}}) {
		t.Fatalf("city resolution changed: tokens=%#v requests=%#v result=%#v", tokens.queries, reader.adminRequests, result)
	}
}
