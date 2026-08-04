package marketing

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	applicationmaterials "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/materials"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/configuration"
	domainmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/marketing"
)

func TestPrepareMarketingUploadDryRunNeedsNoMaterialDependencies(t *testing.T) {
	config := marketingPrepareFixture(AccountUploadSource)
	original := configuration.CloneMap(config)
	prepared, err := (Preparer{Config: staticMarketingConfig{value: config}}).Prepare(
		context.Background(),
		PrepareRequest{
			Kind: PrepareUpload, AdvertiserID: "1234567890", PlanTemplate: "upload-template",
			VideoIDs: []string{"runtime-video"}, MaterialDate: "7.26",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.SourceType != AccountUploadSource || prepared.PreflightSummary["online_material_snapshot"] != false {
		t.Fatalf("unexpected local preview: %#v", prepared)
	}
	materials := prepared.PromotionPayload["promotion_materials"].(map[string]any)["video_material_list"].([]any)
	if len(materials) != 1 || materials[0].(map[string]any)["video_id"] != "runtime-video" {
		t.Fatalf("runtime video was not applied: %#v", materials)
	}
	if !reflect.DeepEqual(config, original) {
		t.Fatal("Marketing preparation mutated source config")
	}
}

func TestPrepareMarketingUploadOnlineValidatesAvailabilityAndCovers(t *testing.T) {
	materials := &marketingMaterialStub{
		adVideos: []domainmarketing.VideoAsset{
			{ID: "video-2", Filename: "second.mp4"},
			{ID: "video-1", Filename: "first.mp4"},
		},
		covers: map[string]string{"video-1": "cover-1", "video-2": "cover-2"},
	}
	prepared, err := (Preparer{
		Config:        staticMarketingConfig{value: marketingPrepareFixture(AccountUploadSource)},
		Materials:     materials,
		RuntimeAssets: passthroughRuntimeAssets{},
	}).Prepare(context.Background(), PrepareRequest{
		Kind: PrepareUpload, AdvertiserID: "1234567890", PlanTemplate: "upload-template",
		VideoIDs: []string{"video-1", "video-2"}, MaterialDate: "7.26", Submit: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(materials.calls, []string{"ad:video-1,video-2", "cover:video-1", "cover:video-2"}) {
		t.Fatalf("online material validation changed: %#v", materials.calls)
	}
	want := []PreparedVideo{
		{VideoID: "video-1", CoverID: "cover-1", Title: "first.mp4"},
		{VideoID: "video-2", CoverID: "cover-2", Title: "second.mp4"},
	}
	if !reflect.DeepEqual(prepared.SelectedVideos, want) || len(prepared.MissingFields) != 0 {
		t.Fatalf("prepared upload differs: %#v", prepared)
	}
	payloadRows := prepared.PromotionPayload["promotion_materials"].(map[string]any)["video_material_list"].([]any)
	if payloadRows[0].(map[string]any)["video_cover_id"] != "cover-1" ||
		payloadRows[1].(map[string]any)["video_cover_id"] != "cover-2" {
		t.Fatalf("official covers were not injected: %#v", payloadRows)
	}
}

func TestPrepareMarketingCreatorUsesCurrentAuthorizationAndNativeIdentity(t *testing.T) {
	materials := &marketingMaterialStub{creatorCandidates: []domainmarketing.CreatorCandidate{
		{
			OwnerAdvertiserID: "1234567890", CreatorID: "8001", CreatorName: "达人甲",
			ItemID: "9007199254740993", VideoID: "creator-video", VideoCoverID: "creator-cover",
			ImageMode: "CREATIVE_IMAGE_MODE_VIDEO_VERTICAL", Title: "作品甲", Usable: true,
		},
	}}
	prepared, err := (Preparer{
		Config:        staticMarketingConfig{value: marketingPrepareFixture(CreatorAuthorizedSource)},
		Materials:     materials,
		RuntimeAssets: passthroughRuntimeAssets{},
	}).Prepare(context.Background(), PrepareRequest{
		Kind: PrepareCreator, AdvertiserID: "1234567890", PlanTemplate: "creator-template",
		ItemIDs: []string{"9007199254740993"}, ExpectedAwemeID: "8001",
		MaterialDate: "7.26", Submit: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(materials.calls, []string{"creator:9007199254740993"}) {
		t.Fatalf("creator snapshot query changed: %#v", materials.calls)
	}
	if prepared.SelectedCreator == nil || prepared.SelectedCreator.AwemeID != "8001" || len(prepared.MissingFields) != 0 {
		t.Fatalf("creator preparation differs: %#v", prepared)
	}
	promotionMaterials := prepared.PromotionPayload["promotion_materials"].(map[string]any)
	row := promotionMaterials["video_material_list"].([]any)[0].(map[string]any)
	if row["item_id"] != jsonNumber("9007199254740993") || row["video_cover_id"] != "creator-cover" {
		t.Fatalf("creator material payload differs: %#v", row)
	}
	if prepared.PromotionPayload["native_setting"].(map[string]any)["aweme_id"] != "8001" {
		t.Fatalf("creator identity missing: %#v", prepared.PromotionPayload)
	}
}

func TestPrepareMarketingBlocksStaleOrIncompleteOnlineMaterials(t *testing.T) {
	tests := []struct {
		name      string
		kind      PrepareKind
		template  string
		source    string
		request   PrepareRequest
		materials *marketingMaterialStub
	}{
		{
			name: "upload missing from ad-get", kind: PrepareUpload, template: "upload-template",
			source: AccountUploadSource, request: PrepareRequest{VideoIDs: []string{"missing"}},
			materials: &marketingMaterialStub{adVideos: []domainmarketing.VideoAsset{}},
		},
		{
			name: "creator expired", kind: PrepareCreator, template: "creator-template",
			source: CreatorAuthorizedSource, request: PrepareRequest{ItemIDs: []string{"9001"}},
			materials: &marketingMaterialStub{creatorCandidates: []domainmarketing.CreatorCandidate{{
				OwnerAdvertiserID: "1234567890", CreatorID: "8001", ItemID: "9001",
				VideoID: "video", VideoCoverID: "cover", Usable: false,
				UnusableReasons: []string{"authorization_expired"},
			}}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := test.request
			request.Kind = test.kind
			request.AdvertiserID = "1234567890"
			request.PlanTemplate = test.template
			request.Submit = true
			_, err := (Preparer{
				Config:        staticMarketingConfig{value: marketingPrepareFixture(test.source)},
				Materials:     test.materials,
				RuntimeAssets: passthroughRuntimeAssets{},
			}).Prepare(context.Background(), request)
			if err == nil {
				t.Fatal("unsafe online material was accepted")
			}
		})
	}
}

type staticMarketingConfig struct {
	value map[string]any
	err   error
}

type passthroughRuntimeAssets struct{}

func (passthroughRuntimeAssets) Resolve(
	_ context.Context,
	request RuntimeAssetRequest,
) (RuntimeAssetResult, error) {
	return RuntimeAssetResult{
		Config: configuration.CloneMap(request.Config),
		Evidence: map[string]any{
			"event_asset":      map[string]any{"status": "configured"},
			"product_creative": map[string]any{"status": "validated"},
		},
	}, nil
}

func (reader staticMarketingConfig) Read(context.Context) (map[string]any, error) {
	if reader.err != nil {
		return nil, reader.err
	}
	return configuration.CloneMap(reader.value), nil
}

type marketingMaterialStub struct {
	adVideos          []domainmarketing.VideoAsset
	covers            map[string]string
	creatorCandidates []domainmarketing.CreatorCandidate
	calls             []string
	err               error
}

func (stub *marketingMaterialStub) QueryVideos(
	_ context.Context,
	query applicationmaterials.VideoQuery,
) (applicationmaterials.VideoResult, error) {
	if stub.err != nil {
		return applicationmaterials.VideoResult{}, stub.err
	}
	switch query.Mode {
	case "ad-get":
		stub.calls = append(stub.calls, "ad:"+stringsJoin(query.VideoIDs))
		return applicationmaterials.VideoResult{MatchedList: append([]domainmarketing.VideoAsset(nil), stub.adVideos...)}, nil
	case "cover-suggest":
		stub.calls = append(stub.calls, "cover:"+query.VideoIDs[0])
		return applicationmaterials.VideoResult{SelectedCoverID: stub.covers[query.VideoIDs[0]]}, nil
	default:
		return applicationmaterials.VideoResult{}, errors.New("unexpected video query")
	}
}

func (stub *marketingMaterialStub) QueryCreator(
	_ context.Context,
	query applicationmaterials.CreatorQuery,
) (applicationmaterials.CreatorResult, error) {
	if stub.err != nil {
		return applicationmaterials.CreatorResult{}, stub.err
	}
	stub.calls = append(stub.calls, "creator:"+stringsJoin(query.ItemIDs))
	return applicationmaterials.CreatorResult{
		Candidates: append([]domainmarketing.CreatorCandidate(nil), stub.creatorCandidates...),
	}, nil
}

func marketingPrepareFixture(sourceType string) map[string]any {
	base := marketingPayloadFixture()
	base["config_schema_version"] = 2
	base["default_channel"] = "marketing"
	base["channels"] = map[string]any{
		"marketing": map[string]any{"api": map[string]any{"base_url": "https://api.oceanengine.com/open_api"}},
		"qianchuan": map[string]any{},
	}
	base["plan_template_schema_version"] = 6
	base["default_plan_template"] = map[string]any{
		"defaults":      configuration.Clone(base["defaults"]),
		"materials":     configuration.Clone(base["materials"]),
		"resolved_ids":  configuration.Clone(base["resolved_ids"]),
		"links":         configuration.Clone(base["links"]),
		"tracking_urls": configuration.Clone(base["tracking_urls"]),
		"titles":        configuration.Clone(base["titles"]),
	}
	name := "upload-template"
	if sourceType == CreatorAuthorizedSource {
		name = "creator-template"
	}
	strategy := map[string]any{
		"source_type": sourceType, "selection_mode": "MANUAL", "max_materials_per_unit": 5,
	}
	if sourceType == CreatorAuthorizedSource {
		strategy["creator_filters"] = map[string]any{
			"creator_ids": []any{}, "auth_types": []any{"VIDEO_ITEM"},
			"authorization_status": "VALID", "minimum_remaining_days": 1,
		}
	}
	base["plan_templates"] = map[string]any{name: map[string]any{
		"bindings": map[string]any{
			"channel": "marketing", "advertiser_id": "1234567890", "platform": "测试平台",
			"traffic_source": "CID", "product_id": "9007199254740993", "product_name": "test product",
		},
		"copy_materials":    map[string]any{"titles": configuration.Clone(base["titles"])},
		"material_strategy": strategy,
		"overrides":         map[string]any{},
	}}
	return base
}

func stringsJoin(values []string) string {
	result := ""
	for index, value := range values {
		if index != 0 {
			result += ","
		}
		result += value
	}
	return result
}

func jsonNumber(value string) any {
	return json.Number(value)
}
