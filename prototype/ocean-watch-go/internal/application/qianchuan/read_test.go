package qianchuan

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	authapplication "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/auth"
	domainqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/qianchuan"
	portqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/qianchuan"
)

type tokenSpy struct {
	queries []authapplication.TokenQuery
}

func (spy *tokenSpy) Ensure(_ context.Context, query authapplication.TokenQuery) (authapplication.TokenLease, error) {
	spy.queries = append(spy.queries, query)
	return authapplication.TokenLease{
		Channel: query.Channel, AuthorizationID: "fixture-authorization",
		AccessToken: "TEST_ACCESS_TOKEN_DO_NOT_USE",
	}, nil
}

type readStub struct {
	productRequests  []portqianchuan.ProductPageRequest
	planRequests     []portqianchuan.PlanPageRequest
	detailRequests   []portqianchuan.PlanDetailRequest
	materialRequests []portqianchuan.MaterialPageRequest
	creatorRequests  []portqianchuan.AuthorizedCreatorPageRequest
	videoRequests    []portqianchuan.CreatorVideoPageRequest
	products         func(portqianchuan.ProductPageRequest) (domainqianchuan.ProductPage, error)
	plans            func(portqianchuan.PlanPageRequest) (domainqianchuan.PlanPage, error)
	detail           func(portqianchuan.PlanDetailRequest) (domainqianchuan.PlanDetail, error)
	materials        func(portqianchuan.MaterialPageRequest) (domainqianchuan.MaterialPage, error)
	creators         func(portqianchuan.AuthorizedCreatorPageRequest) (domainqianchuan.AuthorizedCreatorPage, error)
	videos           func(portqianchuan.CreatorVideoPageRequest) (domainqianchuan.CreatorVideoPage, error)
}

func (stub *readStub) FetchProducts(_ context.Context, request portqianchuan.ProductPageRequest) (domainqianchuan.ProductPage, error) {
	stub.productRequests = append(stub.productRequests, request)
	if stub.products == nil {
		return domainqianchuan.ProductPage{}, errors.New("unexpected product request")
	}
	return stub.products(request)
}

func (stub *readStub) FetchPlans(_ context.Context, request portqianchuan.PlanPageRequest) (domainqianchuan.PlanPage, error) {
	stub.planRequests = append(stub.planRequests, request)
	if stub.plans == nil {
		return domainqianchuan.PlanPage{}, errors.New("unexpected plan request")
	}
	return stub.plans(request)
}

func (stub *readStub) FetchPlanDetail(_ context.Context, request portqianchuan.PlanDetailRequest) (domainqianchuan.PlanDetail, error) {
	stub.detailRequests = append(stub.detailRequests, request)
	if stub.detail == nil {
		return domainqianchuan.PlanDetail{}, errors.New("unexpected detail request")
	}
	return stub.detail(request)
}

func (stub *readStub) FetchPlanMaterials(_ context.Context, request portqianchuan.MaterialPageRequest) (domainqianchuan.MaterialPage, error) {
	stub.materialRequests = append(stub.materialRequests, request)
	if stub.materials == nil {
		return domainqianchuan.MaterialPage{}, errors.New("unexpected material request")
	}
	return stub.materials(request)
}

func (stub *readStub) FetchAuthorizedCreators(_ context.Context, request portqianchuan.AuthorizedCreatorPageRequest) (domainqianchuan.AuthorizedCreatorPage, error) {
	stub.creatorRequests = append(stub.creatorRequests, request)
	if stub.creators == nil {
		return domainqianchuan.AuthorizedCreatorPage{}, errors.New("unexpected authorized creator request")
	}
	return stub.creators(request)
}

func (stub *readStub) FetchCreatorVideos(_ context.Context, request portqianchuan.CreatorVideoPageRequest) (domainqianchuan.CreatorVideoPage, error) {
	stub.videoRequests = append(stub.videoRequests, request)
	if stub.videos == nil {
		return domainqianchuan.CreatorVideoPage{}, errors.New("unexpected creator video request")
	}
	return stub.videos(request)
}

func TestQianchuanReadValidationStopsBeforeTokenResolution(t *testing.T) {
	tests := []struct {
		name string
		run  func(Service) error
	}{
		{name: "invalid-advertiser", run: func(service Service) error {
			_, err := service.QueryProducts(context.Background(), ProductQuery{CredentialScope: CredentialScope{AdvertiserID: "not-an-id"}}, "qianchuan_product_list")
			return err
		}},
		{name: "invalid-product", run: func(service Service) error {
			_, err := service.QueryProducts(context.Background(), ProductQuery{CredentialScope: CredentialScope{AdvertiserID: "1000000000000001"}, ProductIDs: []string{"0"}}, "qianchuan_product_list")
			return err
		}},
		{name: "invalid-date", run: func(service Service) error {
			_, err := service.ListPlans(context.Background(), PlanListQuery{CredentialScope: CredentialScope{AdvertiserID: "1000000000000001"}, StartDate: "2026-07-26", EndDate: "2026-07-25"})
			return err
		}},
		{name: "invalid-top", run: func(service Service) error {
			_, err := service.ListPlans(context.Background(), PlanListQuery{CredentialScope: CredentialScope{AdvertiserID: "1000000000000001"}, Top: -1})
			return err
		}},
		{name: "invalid-plan", run: func(service Service) error {
			_, err := service.ShowPlan(context.Background(), CredentialScope{AdvertiserID: "1000000000000001"}, "bad")
			return err
		}},
		{name: "invalid-page-size", run: func(service Service) error {
			_, err := service.ListPlanMaterials(context.Background(), PlanMaterialsQuery{CredentialScope: CredentialScope{AdvertiserID: "1000000000000001"}, AdID: "2000000000000001", PageSize: 11})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tokens := &tokenSpy{}
			reader := &readStub{}
			if err := test.run(Service{Tokens: tokens, Reader: reader}); err == nil {
				t.Fatal("invalid query was accepted")
			}
			if len(tokens.queries) != 0 || len(reader.productRequests)+len(reader.planRequests)+len(reader.detailRequests)+len(reader.materialRequests) != 0 {
				t.Fatalf("invalid query crossed credential or adapter boundary: tokens=%d reader=%#v", len(tokens.queries), reader)
			}
		})
	}
}

func TestQianchuanReadPaginationFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		page func(int) domainqianchuan.ProductPage
	}{
		{name: "duplicate-id", page: func(page int) domainqianchuan.ProductPage {
			return domainqianchuan.ProductPage{Rows: []domainqianchuan.Product{{ProductID: "3000000000000001"}}, PageInfo: domainqianchuan.PageInfo{Page: page, TotalPages: 2, TotalNumber: 2}}
		}},
		{name: "changed-total-pages", page: func(page int) domainqianchuan.ProductPage {
			totalPages := 2
			if page == 2 {
				totalPages = 3
			}
			return domainqianchuan.ProductPage{Rows: []domainqianchuan.Product{{ProductID: "300000000000000" + string(rune('0'+page))}}, PageInfo: domainqianchuan.PageInfo{Page: page, TotalPages: totalPages, TotalNumber: 2}}
		}},
		{name: "contradictory-total", page: func(page int) domainqianchuan.ProductPage {
			return domainqianchuan.ProductPage{Rows: []domainqianchuan.Product{{ProductID: "3000000000000001"}}, PageInfo: domainqianchuan.PageInfo{Page: page, TotalPages: 1, TotalNumber: 2}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tokens := &tokenSpy{}
			reader := &readStub{products: func(request portqianchuan.ProductPageRequest) (domainqianchuan.ProductPage, error) {
				return test.page(request.Page), nil
			}}
			result, err := (Service{Tokens: tokens, Reader: reader}).QueryProducts(context.Background(), ProductQuery{
				CredentialScope: CredentialScope{AdvertiserID: "1000000000000001"}, MaxPages: 3,
			}, "qianchuan_product_list")
			if err == nil || result.Products != nil {
				t.Fatalf("invalid pagination returned result=%#v err=%v", result, err)
			}
		})
	}
}

func TestQianchuanPlanListUsesCurrentDayAndDoesNotExposeStatsInfo(t *testing.T) {
	tokens := &tokenSpy{}
	reader := &readStub{plans: func(request portqianchuan.PlanPageRequest) (domainqianchuan.PlanPage, error) {
		return domainqianchuan.PlanPage{
			Rows:     []domainqianchuan.Plan{{AdID: "2000000000000001", Name: "Fixture plan"}},
			PageInfo: domainqianchuan.PageInfo{Page: request.Page, TotalPages: 1, TotalNumber: 1},
		}, nil
	}}
	result, err := (Service{
		Tokens: tokens, Reader: reader,
		Now: func() time.Time { return time.Date(2026, 7, 24, 16, 30, 0, 0, time.UTC) },
	}).ListPlans(context.Background(), PlanListQuery{CredentialScope: CredentialScope{AdvertiserID: "1000000000000001"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.planRequests) != 1 {
		t.Fatalf("plan request count = %d", len(reader.planRequests))
	}
	request := reader.planRequests[0]
	if request.StartTime != "2026-07-25 00:00:00" || request.EndTime != "2026-07-25 23:59:59" ||
		request.MarketingGoal != "VIDEO_PROM_GOODS" || request.AdlabScene != "UNI_PROJECT" ||
		request.Status != "ALL" || !request.NeedCompensateInfo {
		t.Fatalf("plan list request changed: %#v", request)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "stats_info") || strings.Contains(string(encoded), "stat_cost") {
		t.Fatalf("plan-list finance escaped the application boundary: %s", encoded)
	}
	wantTokenQuery := []authapplication.TokenQuery{{Channel: "qianchuan", AdvertiserID: "1000000000000001"}}
	if !reflect.DeepEqual(tokens.queries, wantTokenQuery) {
		t.Fatalf("token queries = %#v", tokens.queries)
	}
}

func TestAuthorizedCreatorPaginationRejectsChangedOrIncompleteTotals(t *testing.T) {
	tests := []struct {
		name string
		page func(int) domainqianchuan.AuthorizedCreatorPage
	}{
		{
			name: "changed-total-number",
			page: func(page int) domainqianchuan.AuthorizedCreatorPage {
				total := 2
				if page == 2 {
					total = 3
				}
				return domainqianchuan.AuthorizedCreatorPage{
					Rows:     []domainqianchuan.AuthorizedCreator{{AwemeID: "400000000000000" + strconv.Itoa(page)}},
					PageInfo: domainqianchuan.PageInfo{Page: page, TotalPages: 2, TotalNumber: total},
				}
			},
		},
		{
			name: "incomplete-final-page",
			page: func(page int) domainqianchuan.AuthorizedCreatorPage {
				return domainqianchuan.AuthorizedCreatorPage{
					Rows:     []domainqianchuan.AuthorizedCreator{{AwemeID: "400000000000000" + strconv.Itoa(page)}},
					PageInfo: domainqianchuan.PageInfo{Page: page, TotalPages: 2, TotalNumber: 3},
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &readStub{creators: func(request portqianchuan.AuthorizedCreatorPageRequest) (domainqianchuan.AuthorizedCreatorPage, error) {
				return test.page(request.Page), nil
			}}
			result, err := (Service{Tokens: &tokenSpy{}, Reader: reader}).ListAuthorizedCreators(
				context.Background(),
				AuthorizedCreatorQuery{CredentialScope: CredentialScope{AdvertiserID: "1000000000000001"}, MaxPages: 2},
			)
			if err == nil || result.Creators != nil {
				t.Fatalf("invalid authorized creator pagination returned result=%#v err=%v", result, err)
			}
		})
	}
}

func TestCreatorVideoPaginationDeduplicatesWorksAndFailsClosedOnCursor(t *testing.T) {
	t.Run("deduplicate", func(t *testing.T) {
		next := int64(20)
		reader := &readStub{videos: func(request portqianchuan.CreatorVideoPageRequest) (domainqianchuan.CreatorVideoPage, error) {
			if request.Cursor == nil {
				return domainqianchuan.CreatorVideoPage{
					Rows:       []domainqianchuan.CreatorVideo{{AwemeItemID: "5000000000000001"}},
					NextCursor: &next, HasMore: true,
				}, nil
			}
			return domainqianchuan.CreatorVideoPage{
				Rows: []domainqianchuan.CreatorVideo{
					{AwemeItemID: "5000000000000001"},
					{AwemeItemID: "5000000000000002"},
				},
			}, nil
		}}
		rows, pages, _, err := (Service{Reader: reader}).collectCreatorVideos(
			context.Background(), CredentialScope{AdvertiserID: "1000000000000001"},
			"TEST_ACCESS_TOKEN_DO_NOT_USE", "4000000000000001", "3000000000000001", 50, 2,
		)
		if err != nil {
			t.Fatal(err)
		}
		if pages != 2 || len(rows) != 2 || rows[0].AwemeItemID != "5000000000000001" || rows[1].AwemeItemID != "5000000000000002" {
			t.Fatalf("deduplicated creator videos = %#v, pages=%d", rows, pages)
		}
	})

	t.Run("stalled-cursor", func(t *testing.T) {
		stalled := int64(0)
		reader := &readStub{videos: func(portqianchuan.CreatorVideoPageRequest) (domainqianchuan.CreatorVideoPage, error) {
			return domainqianchuan.CreatorVideoPage{NextCursor: &stalled, HasMore: true}, nil
		}}
		rows, _, _, err := (Service{Reader: reader}).collectCreatorVideos(
			context.Background(), CredentialScope{AdvertiserID: "1000000000000001"},
			"TEST_ACCESS_TOKEN_DO_NOT_USE", "4000000000000001", "3000000000000001", 50, 2,
		)
		if err == nil || rows != nil {
			t.Fatalf("stalled creator cursor returned rows=%#v err=%v", rows, err)
		}
	})
}
