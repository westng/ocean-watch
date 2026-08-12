package marketing

import (
	"context"
	"errors"
	"reflect"
	"testing"

	applicationdiscovery "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/discovery"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

func TestRuntimeAssetResolverUsesConfiguredEventAndValidDPAFields(t *testing.T) {
	config := marketingPayloadFixture()
	original := configuration.CloneMap(config)
	discovery := &runtimeDiscoveryStub{dpa: applicationdiscovery.Result{
		RequestID: "dpa-request",
		Response: responseData(map[string]any{
			"asset_list": []any{map[string]any{"images_url": []any{"https://image.test/a"}}},
		}),
	}}
	result, err := (AssetResolver{Discovery: discovery}).Resolve(context.Background(), RuntimeAssetRequest{
		AdvertiserID: "1234567890", Config: config,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(discovery.calls, []string{"dpa"}) {
		t.Fatalf("configured runtime assets performed unexpected queries: %#v", discovery.calls)
	}
	if !reflect.DeepEqual(config, original) {
		t.Fatal("runtime asset resolution mutated the source config")
	}
	if configuration.Value(result.Config, "defaults.product_info.product_image_type") != "DPA" {
		t.Fatalf("valid DPA source was replaced: %#v", result.Config)
	}
	event := result.Evidence["event_asset"].(map[string]any)
	product := result.Evidence["product_creative"].(map[string]any)
	if event["source"] != "template" || product["source"] != "dpa_product_fields" {
		t.Fatalf("unexpected runtime evidence: %#v", result.Evidence)
	}
}

func TestRuntimeAssetResolverPaginatesProjectsAndReusesMatchingPromotion(t *testing.T) {
	config := marketingPayloadFixture()
	configuration.Object(config["resolved_ids"])["event_asset_ids"] = []any{}
	discovery := &runtimeDiscoveryStub{
		dpa: applicationdiscovery.Result{RequestID: "dpa-empty", Response: responseData(map[string]any{
			"asset_list": []any{},
		})},
		projects: map[int]applicationdiscovery.Result{
			1: discoveryPage("projects-1", "list", []any{map[string]any{
				"project_id": "1001", "advertiser_id": "1234567890",
				"related_product": map[string]any{"unique_product_id": "other-product"},
			}}, 1, 2),
			2: discoveryPage("projects-2", "list", []any{map[string]any{
				"project_id": "1002", "advertiser_id": "1234567890",
				"landing_type": "SHOP", "marketing_goal": "VIDEO_AND_IMAGE",
				"delivery_mode": "PROCEDURAL", "ad_type": "ALL", "asset_type": "THIRDPARTY",
				"related_product": map[string]any{"unique_product_id": "9007199254740993"},
				"optimize_goal": map[string]any{
					"external_action": "AD_CONVERT_TYPE_APP_ORDER", "asset_ids": []any{"3001"},
				},
			}}, 2, 2),
		},
		goals: map[string]applicationdiscovery.Result{
			"3001": {RequestID: "goal-3001", Response: responseData(map[string]any{
				"goals": []any{map[string]any{"external_action": "AD_CONVERT_TYPE_APP_ORDER"}},
			})},
		},
		promotions: map[string]map[int]applicationdiscovery.Result{
			"1002": {
				1: discoveryPage("promotions-1", "list", []any{map[string]any{
					"promotion_id": "2002",
					"promotion_materials": map[string]any{"product_info": map[string]any{
						"image_ids": []any{"image-1", "image-1", "image-2"},
					}},
					"brand_info": map[string]any{"brand_name_id": "brand-1", "cdp_brand_id": ""},
				}}, 1, 1),
			},
		},
	}
	result, err := (AssetResolver{Discovery: discovery}).Resolve(context.Background(), RuntimeAssetRequest{
		AdvertiserID: "1234567890", Config: config,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{"dpa", "projects:1", "projects:2", "goal:3001", "promotions:1002:1"}
	if !reflect.DeepEqual(discovery.calls, wantCalls) {
		t.Fatalf("runtime query sequence differs\ngot  %#v\nwant %#v", discovery.calls, wantCalls)
	}
	if got := configuration.Value(result.Config, "resolved_ids.event_asset_ids"); !reflect.DeepEqual(got, []any{"3001"}) {
		t.Fatalf("matching project event asset was not applied: %#v", got)
	}
	if got := configuration.Value(result.Config, "defaults.product_info.product_image_type"); got != "CUSTOM" {
		t.Fatalf("DPA fallback did not switch to CUSTOM: %#v", got)
	}
	if got := configuration.Value(result.Config, "resolved_ids.product_image_ids"); !reflect.DeepEqual(got, []any{"image-1", "image-2"}) {
		t.Fatalf("promotion image fallback differs: %#v", got)
	}
	if got := configuration.Value(result.Config, "resolved_ids.brand_info"); !reflect.DeepEqual(got, map[string]any{"brand_name_id": "brand-1"}) {
		t.Fatalf("promotion brand fallback differs: %#v", got)
	}
	if result.Evidence["reference_project_count"] != 1 {
		t.Fatalf("unexpected reference project evidence: %#v", result.Evidence)
	}
}

func TestRuntimeAssetResolverBlocksAmbiguousEventAssets(t *testing.T) {
	config := marketingPayloadFixture()
	configuration.Object(config["resolved_ids"])["event_asset_ids"] = []any{}
	discovery := &runtimeDiscoveryStub{
		dpa: applicationdiscovery.Result{Response: responseData(map[string]any{
			"asset_list": []any{map[string]any{"images_url": []any{"available"}}},
		})},
		projects: map[int]applicationdiscovery.Result{
			1: discoveryPage("projects", "list", []any{}, 1, 1),
		},
		events: map[int]applicationdiscovery.Result{
			1: discoveryPage("events", "asset_list", []any{
				map[string]any{"asset_id": "4001", "asset_name": "asset one"},
				map[string]any{"asset_id": "4002", "asset_name": "asset two"},
			}, 1, 1),
		},
		goals: map[string]applicationdiscovery.Result{
			"4001": goalResult("goal-4001"),
			"4002": goalResult("goal-4002"),
		},
	}
	_, err := (AssetResolver{Discovery: discovery}).Resolve(context.Background(), RuntimeAssetRequest{
		AdvertiserID: "1234567890", Config: config,
	})
	var assetErr *RuntimeAssetError
	if !errors.As(err, &assetErr) || assetErr.Code != "event_asset_selection_required" {
		t.Fatalf("ambiguous event assets were not blocked: %T %v", err, err)
	}
	if got := assetErr.Details["candidate_event_assets"].([]any); len(got) != 2 {
		t.Fatalf("ambiguous candidates missing from error: %#v", assetErr.Details)
	}
}

type runtimeDiscoveryStub struct {
	dpa        applicationdiscovery.Result
	projects   map[int]applicationdiscovery.Result
	promotions map[string]map[int]applicationdiscovery.Result
	events     map[int]applicationdiscovery.Result
	goals      map[string]applicationdiscovery.Result
	calls      []string
}

func (stub *runtimeDiscoveryStub) QueryProjects(
	_ context.Context,
	query applicationdiscovery.ProjectQuery,
) (applicationdiscovery.Result, error) {
	stub.calls = append(stub.calls, "projects:"+integerText(query.Page))
	result, ok := stub.projects[query.Page]
	if !ok {
		return applicationdiscovery.Result{}, errors.New("unexpected project page")
	}
	return result, nil
}

func (stub *runtimeDiscoveryStub) QueryPromotions(
	_ context.Context,
	query applicationdiscovery.PromotionQuery,
) (applicationdiscovery.Result, error) {
	stub.calls = append(stub.calls, "promotions:"+query.ProjectID+":"+integerText(query.Page))
	result, ok := stub.promotions[query.ProjectID][query.Page]
	if !ok {
		return applicationdiscovery.Result{}, errors.New("unexpected promotion page")
	}
	return result, nil
}

func (stub *runtimeDiscoveryStub) QueryDPA(
	_ context.Context,
	_ applicationdiscovery.DPAQuery,
) (applicationdiscovery.Result, error) {
	stub.calls = append(stub.calls, "dpa")
	return stub.dpa, nil
}

func (stub *runtimeDiscoveryStub) QueryEvents(
	_ context.Context,
	query applicationdiscovery.EventQuery,
) (applicationdiscovery.Result, error) {
	stub.calls = append(stub.calls, "events:"+integerText(query.Page))
	result, ok := stub.events[query.Page]
	if !ok {
		return applicationdiscovery.Result{}, errors.New("unexpected event page")
	}
	return result, nil
}

func (stub *runtimeDiscoveryStub) QueryGoals(
	_ context.Context,
	query applicationdiscovery.GoalQuery,
) (applicationdiscovery.Result, error) {
	stub.calls = append(stub.calls, "goal:"+query.AssetID)
	result, ok := stub.goals[query.AssetID]
	if !ok {
		return applicationdiscovery.Result{}, errors.New("unexpected goal asset")
	}
	return result, nil
}

func responseData(data map[string]any) map[string]any {
	return map[string]any{"code": 0, "message": "OK", "data": data}
}

func discoveryPage(requestID, key string, rows []any, page, total int) applicationdiscovery.Result {
	return applicationdiscovery.Result{RequestID: requestID, Response: responseData(map[string]any{
		key: rows,
		"page_info": map[string]any{
			"page": page, "page_size": runtimeAssetPageSize, "total_page": total,
		},
	})}
}

func goalResult(requestID string) applicationdiscovery.Result {
	return applicationdiscovery.Result{RequestID: requestID, Response: responseData(map[string]any{
		"goals": []any{map[string]any{"external_action": "AD_CONVERT_TYPE_APP_ORDER"}},
	})}
}

func integerText(value int) string {
	if value == 0 {
		return "0"
	}
	result := ""
	for value > 0 {
		result = string(rune('0'+value%10)) + result
		value /= 10
	}
	return result
}
