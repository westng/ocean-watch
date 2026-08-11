package qianchuan

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	authapplication "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/auth"
	sharedplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/plans"
	applicationworkmetadata "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/workmetadata"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	domainplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/plans"
	domainqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/qianchuan"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/requestcontrol"
	portqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/qianchuan"
)

const commandConfigPath = "/fixture/config.json"

func TestQianchuanCommandBoundaries(t *testing.T) {
	t.Run("single template dry-run uses no token or writer", testCreateCommandDryRunBoundary)
	t.Run("single plan validation error has no partial result", testCreateCommandValidationError)
	t.Run("batch dry-run uses one scoped token and preserves presentation", testBatchCommandReadScope)
	t.Run("metadata product mismatch stops before credentials", testBatchMetadataProductBoundary)
	t.Run("metadata owner hint overrides cache and cache failure only warns", testBatchOwnerHintCacheBoundary)
	t.Run("delete requires both confirmations before links or credentials", testRemoveCommandDoubleConfirmation)
	t.Run("unknown create preserves reconciliation result without replay", testCreateCommandUnknownResult)
}

func TestParseBatchWorkEntries(t *testing.T) {
	entries := parseBatchWorkEntries([]string{
		"[https://v.douyin.com/bad/:code](https://v.douyin.com/abc/)\t真人口播营销\t刘岛",
		"4.87 口令 https://v.douyin.com/xyz/ 复制打开\t9386\t暖身,口播\t刘研",
		"https://v.douyin.com/only/",
	}, "", "")
	if len(entries) != 3 || entries[0].URL != "https://v.douyin.com/abc/" ||
		entries[0].PlanType != "真人口播营销" || entries[0].Business != "刘岛" ||
		entries[1].PlanType != "暖身,口播" || entries[1].Business != "刘研" ||
		entries[2].PlanType != "" || entries[2].Business != "" {
		t.Fatalf("batch work entry parsing changed: %#v", entries)
	}
}

func testCreateCommandValidationError(t *testing.T) {
	service := CommandService{Create: CreateExecutor{}}
	result, err := service.CreatePlan(context.Background(), CreatePlanCommand{
		Payload: json.RawMessage(`{"advertiser_id":1000000000000001,"marketing_goal":"UNSUPPORTED","delivery_setting":{"smart_bid_type":"SMART_BID_CUSTOM","roi2_goal":1.7,"budget":5000}}`),
	})
	if err == nil || !reflect.DeepEqual(result, CreateCommandResult{}) {
		t.Fatalf("validation error returned a partial execution result: result=%#v err=%v", result, err)
	}
}

func TestCreatePayloadNormalizesOfficialNumericIDsBeforeDryRun(t *testing.T) {
	service := CommandService{Create: CreateExecutor{}}
	payload := json.RawMessage(`{
		"advertiser_id":"1000000000000001",
		"aweme_id":"4000000000000001",
		"name":"达人🧀计划",
		"marketing_goal":"VIDEO_PROM_GOODS",
		"product_ids":["5000000000000001"],
		"delivery_setting":{"smart_bid_type":"SMART_BID_CUSTOM","roi2_goal":1.75,"budget":5000},
		"multi_product_creative_list":[{
			"product_id":"5000000000000001",
			"video_material":[{"aweme_item_id":"6000000000000001","image_mode":"VIDEO_VERTICAL"}],
			"block_video_material":[{"aweme_item_id":"6000000000000002"}]
		}]
	}`)

	result, err := service.CreatePlan(context.Background(), CreatePlanCommand{Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	creative := result.Payload["multi_product_creative_list"].([]any)[0].(map[string]any)
	video := creative["video_material"].([]any)[0].(map[string]any)
	blocked := creative["block_video_material"].([]any)[0].(map[string]any)
	if result.Payload["name"] != "达人计划" ||
		result.Payload["aweme_id"] != json.Number("4000000000000001") ||
		creative["product_id"] != json.Number("5000000000000001") ||
		video["aweme_item_id"] != json.Number("6000000000000001") ||
		blocked["aweme_item_id"] != json.Number("6000000000000002") {
		t.Fatalf("Qianchuan numeric IDs were not normalized before preview: %#v", result.Payload)
	}
}

func TestCreateLivePayloadNormalizesProgrammaticAwemeItemIDs(t *testing.T) {
	service := CommandService{Create: CreateExecutor{}}
	payload := json.RawMessage(`{
		"advertiser_id":"1000000000000001",
		"aweme_id":"4000000000000001",
		"marketing_goal":"LIVE_PROM_GOODS",
		"delivery_setting":{"smart_bid_type":"SMART_BID_CONSERVATIVE","budget":5000},
		"programmatic_creative_media_list":{
			"video_material":[{"aweme_item_id":"6000000000000001","image_mode":"VIDEO_VERTICAL"}],
			"block_video_material":[{"aweme_item_id":"6000000000000002"}]
		}
	}`)

	result, err := service.CreatePlan(context.Background(), CreatePlanCommand{Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	programmatic := result.Payload["programmatic_creative_media_list"].(map[string]any)
	video := programmatic["video_material"].([]any)[0].(map[string]any)
	blocked := programmatic["block_video_material"].([]any)[0].(map[string]any)
	if result.Payload["aweme_id"] != json.Number("4000000000000001") ||
		video["aweme_item_id"] != json.Number("6000000000000001") ||
		blocked["aweme_item_id"] != json.Number("6000000000000002") {
		t.Fatalf("Qianchuan live numeric IDs were not normalized before preview: %#v", result.Payload)
	}
}

func testCreateCommandDryRunBoundary(t *testing.T) {
	tokens := &commandTokenProvider{}
	writer := &commandWriter{}
	service := CommandService{
		Config: commandConfigReader{config: commandProductConfig()}, Tokens: tokens,
		Create: CreateExecutor{Writer: writer},
	}
	result, err := service.CreatePlan(context.Background(), CreatePlanCommand{
		ConfigPath: commandConfigPath, PlanTemplate: "qcpt_command", Name: "fixture-plan",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != "dry_run" || result.Status != "ready" || result.Config != commandConfigPath ||
		!reflect.DeepEqual(result.BlockingFields, []string{"runtime_creator_materials"}) {
		t.Fatalf("single-plan dry-run contract changed: %#v", result)
	}
	if tokens.calls != 0 || writer.createCalls != 0 {
		t.Fatalf("single-plan dry-run crossed credential/write boundary: tokens=%d writes=%d", tokens.calls, writer.createCalls)
	}
}

func testBatchCommandReadScope(t *testing.T) {
	reader := &commandReader{}
	tokens := &commandTokenProvider{}
	links := &commandLinkResolver{result: applicationworkmetadata.ResolveResult{
		Resolved: []domain.ResolvedWorkLink{{
			InputIndex: 0, InputURL: "https://www.douyin.com/video/6000000000000001",
			AwemeItemID: "6000000000000001", CreatorName: "第三方达人",
			OwnerHint: &domain.WorkOwnerHint{
				AwemeID: batchCreatorID, AwemeShowID: batchVisibleID,
			},
		}},
	}}
	service := CommandService{
		Config: commandConfigReader{config: commandProductConfig()}, Tokens: tokens, Links: links,
		Verifier: WorkVerifier{Reader: reader},
		Batch:    BatchService{Reader: reader, Reconciler: commandNoPlanFinder{}},
		Locks:    commandLocker{},
		Now:      func() time.Time { return time.Date(2026, time.July, 26, 12, 0, 0, 0, time.UTC) },
	}
	result, err := service.BatchWorks(context.Background(), BatchWorksCommand{
		PlanTemplate: "qcpt_command", WorkURLs: []string{"https://www.douyin.com/video/6000000000000001"},
		IncludePayloads: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tokens.calls != 1 || tokens.last.Channel != "qianchuan" ||
		tokens.last.AdvertiserID != batchAdvertiserID || reader.unscopedCalls != 0 {
		t.Fatalf("batch read scope changed: tokens=%d query=%#v unscoped=%d", tokens.calls, tokens.last, reader.unscopedCalls)
	}
	if len(result.Results) != 1 || len(result.Results[0].Writes) != 1 ||
		len(result.Results[0].Writes[0].Payload) == 0 {
		t.Fatalf("include-payloads did not reach batch writes: %#v", result.Results)
	}
	wantColumns := []domain.PresentationColumn{
		{Field: "plan_id", Label: "计划ID"},
		{Field: "creator_nickname", Label: "达人昵称"},
		{Field: "product_id", Label: "商品ID"},
		{Field: "material_id", Label: "素材ID"},
		{Field: "material_title", Label: "素材标题"},
	}
	if !result.Presentation.Required || !reflect.DeepEqual(result.Presentation.Columns, wantColumns) ||
		!strings.HasPrefix(result.Presentation.RenderedMarkdown, "| 计划ID | 达人昵称 | 商品ID | 素材ID | 素材标题 |") {
		t.Fatalf("mandatory five-column presentation changed: %#v", result.Presentation)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), commandToken) {
		t.Fatal("batch result exposed the token lease")
	}

	resultWithoutPayload, err := service.BatchWorks(context.Background(), BatchWorksCommand{
		PlanTemplate: "qcpt_command", WorkURLs: []string{"https://www.douyin.com/video/6000000000000001"},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = json.Marshal(resultWithoutPayload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"payload"`) {
		t.Fatalf("batch payload appeared without explicit opt-in: %s", encoded)
	}
}

func testBatchMetadataProductBoundary(t *testing.T) {
	for _, test := range []struct {
		name               string
		endpoint           string
		disabled           bool
		wantMetadataCalls  int
		wantMetadataEnable bool
	}{
		{name: "enabled metadata", endpoint: "https://metadata.example.test/private-path", wantMetadataCalls: 1, wantMetadataEnable: true},
		{name: "disabled invalid metadata", endpoint: "http://invalid.example.test/private-path", disabled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := commandProductConfig()
			config["integrations"] = map[string]any{
				"qianchuan_work_metadata": map[string]any{"endpoint": test.endpoint},
			}
			defaultLinks := &commandLinkResolver{}
			metadataLinks := &commandLinkResolver{result: applicationworkmetadata.ResolveResult{
				Resolved: []domain.ResolvedWorkLink{{
					InputIndex: 0, InputURL: "https://www.douyin.com/video/6000000000000001",
					AwemeItemID: "6000000000000001",
					ProductHint: &domain.WorkProductHint{ProductID: "5000000000000099"},
				}},
			}}
			if test.disabled {
				defaultLinks = metadataLinks
				metadataLinks = &commandLinkResolver{}
			}
			tokens := &commandTokenProvider{}
			metadataFactoryCalls := 0
			service := CommandService{
				Config: commandConfigReader{config: config}, Links: defaultLinks, Tokens: tokens,
				MetadataLinks: func(endpoint string) (WorkLinkResolver, error) {
					metadataFactoryCalls++
					if endpoint != test.endpoint {
						t.Fatalf("metadata endpoint changed: %q", endpoint)
					}
					return metadataLinks, nil
				},
				Batch: BatchService{},
			}
			result, err := service.BatchWorks(context.Background(), BatchWorksCommand{
				PlanTemplate: "qcpt_command", WorkURLs: []string{"https://v.douyin.com/fixture"},
				NoLinkMetadataAPI: test.disabled,
			})
			if err != nil {
				t.Fatal(err)
			}
			if tokens.calls != 0 || metadataFactoryCalls != test.wantMetadataCalls ||
				result.Performance.LinkMetadata.Configured != true ||
				result.Performance.LinkMetadata.Enabled != test.wantMetadataEnable {
				t.Fatalf("metadata product mismatch crossed credential/config boundary: tokens=%d factory=%d performance=%#v",
					tokens.calls, metadataFactoryCalls, result.Performance)
			}
			if len(result.Skipped) != 1 || result.Skipped[0].Reason != "link_metadata_product_mismatch" ||
				result.Skipped[0].HintedProductID != "5000000000000099" ||
				!strings.HasPrefix(result.Presentation.RenderedMarkdown, "| 计划ID | 达人昵称 | 商品ID | 素材ID | 素材标题 |") {
				t.Fatalf("metadata product mismatch output changed: %#v", result)
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "private-path") || strings.Contains(string(encoded), "metadata.example.test") {
				t.Fatalf("metadata endpoint leaked in output: %s", encoded)
			}
		})
	}
}

func testBatchOwnerHintCacheBoundary(t *testing.T) {
	reader := &hintVerificationReader{
		targetedCreator: domainqianchuan.AuthorizedCreator{
			AwemeID: batchCreatorID, VisibleID: batchVisibleID, Name: "fixture-creator",
		},
		actualCreatorID: batchCreatorID,
	}
	tokens := &commandTokenProvider{}
	cache := &commandOwnerHintCache{
		loaded: map[string]OwnerHint{
			"6000000000000001": {AwemeID: "4000000000000099", AwemeShowID: batchVisibleID},
		},
		storeErr: errors.New("synthetic cache write failure"),
	}
	links := &commandLinkResolver{result: applicationworkmetadata.ResolveResult{
		Resolved: []domain.ResolvedWorkLink{{
			InputIndex: 0, InputURL: "https://www.douyin.com/video/6000000000000001",
			AwemeItemID: "6000000000000001",
			OwnerHint:   &domain.WorkOwnerHint{AwemeID: batchCreatorID, AwemeShowID: batchVisibleID},
		}},
	}}
	service := CommandService{
		Config: commandConfigReader{config: commandProductConfig()}, Tokens: tokens, Links: links,
		OwnerHints: cache, Verifier: WorkVerifier{Reader: reader},
		Batch: BatchService{Reader: reader, Reconciler: commandNoPlanFinder{}},
		Locks: commandLocker{},
	}
	result, err := service.BatchWorks(context.Background(), BatchWorksCommand{
		PlanTemplate: "qcpt_command", WorkURLs: []string{"https://www.douyin.com/video/6000000000000001"},
	})
	if err != nil {
		t.Fatal(err)
	}
	performance := result.Performance.OwnerHintCache
	warning, _ := performance.Warning.(map[string]string)
	if tokens.calls != 1 || cache.loadCalls != 1 || cache.storeCalls != 1 ||
		performance.Loaded != 1 || performance.LoadedFromCache != 1 ||
		performance.LoadedFromLinkMetadata != 1 || performance.Verified != 1 ||
		performance.BroadScanWorkCount != 0 || performance.Stored != 0 ||
		warning["code"] != "owner_hint_cache_write_failed" {
		t.Fatalf("owner hint precedence/cache warning contract changed: performance=%#v cache=%#v", performance, cache)
	}
	stored := cache.stored["6000000000000001"]
	if stored.AwemeID != batchCreatorID || stored.AwemeShowID != batchVisibleID || reader.broadCalls != 0 {
		t.Fatalf("metadata hint did not override stale cache before official verification: stored=%#v reader=%#v", stored, reader)
	}
}

func testRemoveCommandDoubleConfirmation(t *testing.T) {
	tokens := &commandTokenProvider{}
	links := &commandLinkResolver{}
	service := CommandService{Tokens: tokens, Links: links}
	_, err := service.RemoveWorks(context.Background(), RemoveWorksCommand{
		AdvertiserID: batchAdvertiserID, AdID: batchPlanID, Submit: true,
		WorkURLs: []string{"https://www.douyin.com/video/6000000000000001"},
	})
	if err == nil || !strings.Contains(err.Error(), "confirm-delete") {
		t.Fatalf("missing delete confirmation was accepted: %v", err)
	}
	if links.calls != 0 || tokens.calls != 0 {
		t.Fatalf("delete confirmation happened after side effects: links=%d tokens=%d", links.calls, tokens.calls)
	}
}

func testCreateCommandUnknownResult(t *testing.T) {
	writer := &commandWriter{createErr: &domainplans.DispatchFailure{
		State: domainplans.DispatchUnknown, Cause: errors.New("synthetic response loss"),
	}}
	finder := &commandNoMatchFinder{}
	credentials := &commandCredentialProvider{}
	service := CommandService{Create: CreateExecutor{
		Guard:  sharedplans.GuardedExecutor{Credentials: credentials, Locks: commandLocker{}},
		Writer: writer, Reconciler: finder,
	}}
	result, err := service.CreatePlan(context.Background(), CreatePlanCommand{
		ConfigPath: commandConfigPath, Payload: commandCreatePayload(),
		AdvertiserID: batchAdvertiserID, Submit: true,
	})
	if err == nil || result.Status != "unknown" || result.DispatchState != domainplans.DispatchUnknown ||
		result.FailureStage != "plan_reconciliation" || result.ExitCode != 1 {
		t.Fatalf("unknown result was discarded: result=%#v err=%v", result, err)
	}
	if writer.createCalls != 1 || finder.calls != 1 || credentials.calls != 1 {
		t.Fatalf("unknown write was replayed or not reconciled: writer=%d finder=%d credentials=%d", writer.createCalls, finder.calls, credentials.calls)
	}
}

type commandConfigReader struct{ config map[string]any }

func (reader commandConfigReader) Read(context.Context) (map[string]any, error) {
	return reader.config, nil
}

type commandTokenProvider struct {
	calls int
	last  authapplication.TokenQuery
}

const commandToken = "TEST_QIANCHUAN_COMMAND_TOKEN_DO_NOT_USE"

func (provider *commandTokenProvider) Ensure(_ context.Context, query authapplication.TokenQuery) (authapplication.TokenLease, error) {
	provider.calls++
	provider.last = query
	return authapplication.TokenLease{
		Channel: "qianchuan", AuthorizationID: "fixture-command-authorization", AccessToken: commandToken,
	}, nil
}

type commandLinkResolver struct {
	calls  int
	result applicationworkmetadata.ResolveResult
}

type commandOwnerHintCache struct {
	loaded     map[string]OwnerHint
	stored     map[string]OwnerHint
	loadErr    error
	storeErr   error
	loadCalls  int
	storeCalls int
}

func (cache *commandOwnerHintCache) Load(context.Context, string, []string) (map[string]OwnerHint, error) {
	cache.loadCalls++
	result := map[string]OwnerHint{}
	for itemID, hint := range cache.loaded {
		result[itemID] = hint
	}
	return result, cache.loadErr
}

func (cache *commandOwnerHintCache) Store(_ context.Context, _ string, hints map[string]OwnerHint) (int, error) {
	cache.storeCalls++
	cache.stored = map[string]OwnerHint{}
	for itemID, hint := range hints {
		cache.stored[itemID] = hint
	}
	if cache.storeErr != nil {
		return 0, cache.storeErr
	}
	return len(hints), nil
}

func (resolver *commandLinkResolver) Resolve(context.Context, applicationworkmetadata.ResolveRequest) (applicationworkmetadata.ResolveResult, error) {
	resolver.calls++
	return resolver.result, nil
}

type commandReader struct{ unscopedCalls int }

func (reader *commandReader) checkScope(ctx context.Context) {
	scope, ok := requestcontrol.Authorization(ctx)
	if !ok || scope.Channel != "qianchuan" || scope.AuthorizationID != "fixture-command-authorization" {
		reader.unscopedCalls++
	}
}

func (*commandReader) FetchProducts(context.Context, portqianchuan.ProductPageRequest) (domainqianchuan.ProductPage, error) {
	return domainqianchuan.ProductPage{}, errors.New("unexpected product query")
}

func (*commandReader) FetchPlans(context.Context, portqianchuan.PlanPageRequest) (domainqianchuan.PlanPage, error) {
	return domainqianchuan.PlanPage{}, errors.New("unexpected plan query")
}

func (*commandReader) FetchPlanDetail(context.Context, portqianchuan.PlanDetailRequest) (domainqianchuan.PlanDetail, error) {
	return domainqianchuan.PlanDetail{}, errors.New("unexpected plan detail query")
}

func (*commandReader) FetchPlanMaterials(context.Context, portqianchuan.MaterialPageRequest) (domainqianchuan.MaterialPage, error) {
	return domainqianchuan.MaterialPage{}, errors.New("unexpected material query")
}

func (reader *commandReader) FetchAuthorizedCreators(ctx context.Context, _ portqianchuan.AuthorizedCreatorPageRequest) (domainqianchuan.AuthorizedCreatorPage, error) {
	reader.checkScope(ctx)
	return domainqianchuan.AuthorizedCreatorPage{
		Rows: []domainqianchuan.AuthorizedCreator{{
			AwemeID: batchCreatorID, VisibleID: batchVisibleID, Name: "fixture-creator",
		}},
		PageInfo: domainqianchuan.PageInfo{Page: 1, TotalPages: 1, TotalNumber: 1},
	}, nil
}

func (reader *commandReader) FetchCreatorVideos(ctx context.Context, request portqianchuan.CreatorVideoPageRequest) (domainqianchuan.CreatorVideoPage, error) {
	reader.checkScope(ctx)
	rows := make([]domainqianchuan.CreatorVideo, 0, len(request.AwemeItemIDs))
	for _, itemID := range request.AwemeItemIDs {
		rows = append(rows, domainqianchuan.CreatorVideo{
			AwemeItemID: itemID, ImageMode: "VIDEO_LARGE", MaterialID: "material-" + itemID,
			Title: "fixture-title",
		})
	}
	return domainqianchuan.CreatorVideoPage{Rows: rows}, nil
}

type commandNoPlanFinder struct{}

func (commandNoPlanFinder) FindCurrentPlans(_ context.Context, request CurrentPlanRequest) (CurrentPlanResult, error) {
	result := CurrentPlanResult{Matches: map[string][]ExistingPlan{}}
	for _, target := range request.Targets {
		result.Matches[target.AwemeID] = []ExistingPlan{}
	}
	return result, nil
}

type commandNoMatchFinder struct{ calls int }

func (finder *commandNoMatchFinder) FindCurrentPlans(_ context.Context, request CurrentPlanRequest) (CurrentPlanResult, error) {
	finder.calls++
	return commandNoPlanFinder{}.FindCurrentPlans(context.Background(), request)
}

type commandWriter struct {
	createCalls int
	createErr   error
}

func (writer *commandWriter) CreatePlan(context.Context, portqianchuan.CreatePlanRequest) (portqianchuan.WriteResult, error) {
	writer.createCalls++
	return portqianchuan.WriteResult{}, writer.createErr
}

func (*commandWriter) AddMaterials(context.Context, portqianchuan.MaterialWriteRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected material add")
}

func (*commandWriter) DeleteMaterials(context.Context, portqianchuan.DeleteMaterialsRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected material delete")
}

func (*commandWriter) UpdatePlan(context.Context, portqianchuan.MutationRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected plan mutation")
}

type commandCredentialProvider struct{ calls int }

func (provider *commandCredentialProvider) AccessToken(context.Context, domainplans.Channel, string, string) (sharedplans.CredentialLease, error) {
	provider.calls++
	return sharedplans.CredentialLease{AuthorizationID: "fixture-command-authorization", AccessToken: commandToken}, nil
}

type commandLocker struct{}

func (commandLocker) Acquire(context.Context, domainplans.WriteScope) (func() error, error) {
	return func() error { return nil }, nil
}

func commandProductConfig() map[string]any {
	return map[string]any{
		"qianchuan_product_template_schema_version": 5,
		"qianchuan_product_templates": map[string]any{
			"qcpt_command": map[string]any{
				"template_id":   "qcpt_command",
				"display_name":  "巨量千川-1000000000000001-测试商品-5000000000000001-商品全域",
				"template_type": "QIANCHUAN_PRODUCT_ALL_DOMAIN", "status": "active",
				"bindings": map[string]any{
					"channel": "qianchuan", "advertiser_id": batchAdvertiserID,
					"product_name": "测试商品", "product_ids": []any{batchProductID},
				},
				"delivery_setting": map[string]any{
					"smart_bid_type": "SMART_BID_CUSTOM", "roi2_goal": 1.7,
					"qcpx_mode": "QCPX_MODE_ON", "budget": 5000,
					"video_schedule_type":  "SCHEDULE_FROM_NOW",
					"deep_external_action": "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI",
				},
				"plan_name_template": "{product_name}-{creator_name}-{datetime}",
				"material_strategy": map[string]any{
					"source_type": "CREATOR_RUNTIME_QUERY", "persist_material_ids": false,
				},
			},
		},
	}
}

func commandCreatePayload() json.RawMessage {
	return json.RawMessage(`{"advertiser_id":1000000000000001,"aweme_id":4000000000000001,"name":"stable-plan","marketing_goal":"VIDEO_PROM_GOODS","product_ids":[5000000000000001],"delivery_setting":{"smart_bid_type":"SMART_BID_CUSTOM","roi2_goal":1.75,"budget":5000,"video_schedule_type":"SCHEDULE_FROM_NOW"}}`)
}
