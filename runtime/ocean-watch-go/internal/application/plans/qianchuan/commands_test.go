package qianchuan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	applicationworkmetadata "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/workmetadata"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

const commandConfigPath = "/fixture/config.json"

func TestQianchuanCommandBoundaries(t *testing.T) {
	t.Run("single template dry-run uses no token or writer", testCreateCommandDryRunBoundary)
	t.Run("single plan validation error has no partial result", testCreateCommandValidationError)
	t.Run("batch dry-run uses one scoped token and preserves presentation", testBatchCommandReadScope)
	t.Run("metadata owner hint overrides cache and cache failure only warns", testBatchOwnerHintCacheBoundary)
	t.Run("staged resolver skips F2 only for hot owner hints", testBatchStagedOwnerHintCache)
	t.Run("read-only preflight does not acquire advertiser write lock", testBatchReadOnlyLockBoundary)
	t.Run("delete requires both confirmations before links or credentials", testRemoveCommandDoubleConfirmation)
	t.Run("unknown create preserves reconciliation result without replay", testCreateCommandUnknownResult)
}

func TestBatchPlanScanStartsBeforeSlowLinkResolutionFinishes(t *testing.T) {
	scanStarted := make(chan struct{})
	reconciler := &commandSnapshotReconciler{scanStarted: scanStarted}
	links := &blockingCommandLinkResolver{
		scanStarted: scanStarted,
		result: applicationworkmetadata.ResolveResult{Resolved: []domain.ResolvedWorkLink{{
			InputIndex: 0, InputURL: "https://www.douyin.com/video/6000000000000001",
			AwemeItemID: "6000000000000001", CreatorName: "F2 达人",
			OwnerHint: &domain.WorkOwnerHint{AwemeID: batchCreatorID, AwemeShowID: batchVisibleID},
		}}},
	}
	tokens := &commandTokenProvider{}
	service := CommandService{
		Config: commandConfigReader{config: commandProductConfig()}, Tokens: tokens, Links: links,
		Verifier: WorkVerifier{Reader: &commandReader{}},
		Batch:    BatchService{Reader: &commandReader{}, Reconciler: reconciler},
		Locks:    commandLocker{}, Journals: &commandJournalStore{},
		NewPreflightID: func(string, time.Time) (string, error) {
			return "qianchuan-preflight-overlap", nil
		},
		Now: func() time.Time { return time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC) },
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := service.BatchWorks(ctx, BatchWorksCommand{
		PlanTemplate: "qcpt_command", Items: batchCommandItems("https://www.douyin.com/video/6000000000000001"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if links.calls != 1 || reconciler.scanCalls != 1 || result.PreflightID != "qianchuan-preflight-overlap" {
		t.Fatalf("plan scan and slow link branch did not overlap: links=%d scans=%d result=%#v",
			links.calls, reconciler.scanCalls, result)
	}
}

func TestBatchComponentTimingUsesOperationCompletion(t *testing.T) {
	reader := &commandReader{authorizedDelay: 80 * time.Millisecond}
	service := CommandService{
		Config: commandConfigReader{config: commandProductConfig()}, Tokens: &commandTokenProvider{},
		Links: &commandLinkResolver{result: applicationworkmetadata.ResolveResult{Resolved: []domain.ResolvedWorkLink{{
			InputIndex: 0, InputURL: "https://www.douyin.com/video/6000000000000001",
			AwemeItemID: "6000000000000001", CreatorName: "F2 达人",
			OwnerHint: &domain.WorkOwnerHint{AwemeID: batchCreatorID, AwemeShowID: batchVisibleID},
		}}}},
		Verifier: WorkVerifier{Reader: reader},
		Batch:    BatchService{Reader: reader, Reconciler: &commandSnapshotReconciler{}},
		Locks:    commandLocker{}, Journals: &commandJournalStore{},
		NewPreflightID: func(string, time.Time) (string, error) {
			return "qianchuan-preflight-component-timing", nil
		},
	}
	result, err := service.BatchWorks(context.Background(), BatchWorksCommand{
		PlanTemplate: "qcpt_command", Items: batchCommandItems("https://www.douyin.com/video/6000000000000001"),
	})
	if err != nil {
		t.Fatal(err)
	}
	stages := result.Performance.Stages
	if stages.OfficialVerificationSeconds < 0.07 ||
		stages.PlanInventorySeconds >= stages.OfficialVerificationSeconds/2 {
		t.Fatalf("component timing included wait after operation completion: %#v", stages)
	}
}

func TestBatchEmptyLinkResultSkipsCredentialsLockAndPlanScan(t *testing.T) {
	tokens := &blockingCommandTokenProvider{started: make(chan struct{})}
	links := &commandLinkResolver{result: applicationworkmetadata.ResolveResult{
		Skipped: []domain.SkippedWorkLink{{InputIndex: 0, Reason: "invalid_url", Message: "fixture"}},
	}}
	reconciler := &commandSnapshotReconciler{}
	service := CommandService{
		Config: commandConfigReader{config: commandProductConfig()}, Tokens: tokens, Links: links,
		Batch: BatchService{Reconciler: reconciler},
	}
	result, err := service.BatchWorks(context.Background(), BatchWorksCommand{
		PlanTemplate: "qcpt_command", Items: batchCommandItems("invalid"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != "dry_run" || len(result.Skipped) != 1 || reconciler.scanCalls != 0 ||
		result.PreflightID != "" {
		t.Fatalf("empty link result crossed the official-read boundary: %#v scans=%d", result, reconciler.scanCalls)
	}
	select {
	case <-tokens.started:
	case <-time.After(time.Second):
		t.Fatal("parallel credential preparation did not start")
	}
}

func TestBatchBadLinkDoesNotBlockVerifiedGroupsOrCallWriter(t *testing.T) {
	links := &commandLinkResolver{result: applicationworkmetadata.ResolveResult{
		Resolved: []domain.ResolvedWorkLink{{
			InputIndex: 1, InputURL: "https://www.douyin.com/video/6000000000000001",
			AwemeItemID: "6000000000000001", CreatorName: "F2 达人",
			OwnerHint: &domain.WorkOwnerHint{AwemeID: batchCreatorID, AwemeShowID: batchVisibleID},
		}},
		Skipped: []domain.SkippedWorkLink{{InputIndex: 0, Reason: "invalid_url", Message: "fixture"}},
	}}
	writer := &commandWriter{}
	service := CommandService{
		Config: commandConfigReader{config: commandProductConfig()}, Tokens: &commandTokenProvider{}, Links: links,
		Verifier: WorkVerifier{Reader: &commandReader{}},
		Batch: BatchService{
			Reader: &commandReader{}, Writer: writer, Reconciler: commandNoPlanFinder{},
		},
		Locks: commandLocker{}, Journals: &commandJournalStore{},
		NewPreflightID: func(string, time.Time) (string, error) {
			return "qianchuan-preflight-partial-links", nil
		},
		Now: func() time.Time { return time.Date(2026, 8, 18, 4, 0, 0, 0, time.UTC) },
	}
	result, err := service.BatchWorks(context.Background(), BatchWorksCommand{
		PlanTemplate: "qcpt_command",
		Items: []domainqianchuan.BatchItem{
			{InputIndex: 0, WorkURL: "bad", PlanType: "随手po", Business: "刘岛"},
			{InputIndex: 1, WorkURL: "https://www.douyin.com/video/6000000000000001", PlanType: "真人口播营销", Business: "刘岛"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Skipped) != 1 || len(result.Results) != 1 ||
		result.Results[0].PlanType != "真人口播营销" || result.Results[0].Status != "would_create" ||
		writer.createCalls != 0 {
		t.Fatalf("bad-link isolation or preflight write boundary changed: result=%#v writes=%d", result, writer.createCalls)
	}
}

func TestBatchGlobalFailuresBlockTheWholeBatchBeforeWrites(t *testing.T) {
	items := []domainqianchuan.BatchItem{
		{InputIndex: 0, WorkURL: "https://www.douyin.com/video/6000000000000001", PlanType: "随手po", Business: "刘岛"},
		{InputIndex: 1, WorkURL: "https://www.douyin.com/video/6000000000000002", PlanType: "真人口播营销", Business: "刘岛"},
	}
	resolved := applicationworkmetadata.ResolveResult{Resolved: []domain.ResolvedWorkLink{
		{
			InputIndex: 0, InputURL: items[0].WorkURL, AwemeItemID: "6000000000000001", CreatorName: "F2 达人",
			OwnerHint: &domain.WorkOwnerHint{AwemeID: batchCreatorID, AwemeShowID: batchVisibleID},
		},
		{
			InputIndex: 1, InputURL: items[1].WorkURL, AwemeItemID: "6000000000000002", CreatorName: "F2 达人",
			OwnerHint: &domain.WorkOwnerHint{AwemeID: batchCreatorID, AwemeShowID: batchVisibleID},
		},
	}}
	for _, test := range []struct {
		name  string
		stage string
		build func(*commandWriter) CommandService
	}{
		{
			name: "configuration", stage: "configuration",
			build: func(writer *commandWriter) CommandService {
				return CommandService{Config: commandConfigReader{err: errors.New("fixture config failure")}, Batch: BatchService{Writer: writer}}
			},
		},
		{
			name: "template", stage: "template",
			build: func(writer *commandWriter) CommandService {
				return CommandService{Config: commandConfigReader{config: commandProductConfig()}, Batch: BatchService{Writer: writer}}
			},
		},
		{
			name: "authorization", stage: "authorization",
			build: func(writer *commandWriter) CommandService {
				return CommandService{
					Config: commandConfigReader{config: commandProductConfig()},
					Tokens: &commandTokenProvider{err: errors.New("fixture authorization failure")},
					Links:  &commandLinkResolver{result: resolved}, Batch: BatchService{Writer: writer},
				}
			},
		},
		{
			name: "plan inventory", stage: "plan_inventory",
			build: func(writer *commandWriter) CommandService {
				reader := &commandReader{}
				return CommandService{
					Config: commandConfigReader{config: commandProductConfig()}, Tokens: &commandTokenProvider{},
					Links: &commandLinkResolver{result: resolved}, Verifier: WorkVerifier{Reader: reader},
					Batch: BatchService{
						Reader: reader, Writer: writer,
						Reconciler: &commandSnapshotReconciler{scanErr: errors.New("fixture inventory failure")},
					},
					Locks: commandLocker{},
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			writer := &commandWriter{}
			service := test.build(writer)
			templateID := "qcpt_command"
			if test.name == "template" {
				templateID = "missing-template"
			}
			result, err := service.BatchWorks(context.Background(), BatchWorksCommand{
				PlanTemplate: templateID, Items: append([]domainqianchuan.BatchItem(nil), items...),
			})
			var stageErr *PreflightStageError
			if !errors.As(err, &stageErr) || stageErr.Stage != test.stage || result.PreflightID != "" || writer.createCalls != 0 {
				t.Fatalf("global failure escaped fail-closed boundary: result=%#v err=%v writes=%d", result, err, writer.createCalls)
			}
		})
	}
}

func TestBatchPreflightSubmitSkipsLinksAndWorkVerification(t *testing.T) {
	reader := &commandReader{}
	reconciler := &commandSnapshotReconciler{}
	links := &commandLinkResolver{result: applicationworkmetadata.ResolveResult{
		Resolved: []domain.ResolvedWorkLink{{
			InputIndex: 0, InputURL: "https://www.douyin.com/video/6000000000000001",
			AwemeItemID: "6000000000000001", CreatorName: "F2 达人",
			OwnerHint: &domain.WorkOwnerHint{AwemeID: batchCreatorID, AwemeShowID: batchVisibleID},
		}},
	}}
	writer := &commandSuccessfulWriter{}
	bindings := &memoryPlanBindingStore{}
	journals := &commandJournalStore{}
	tokens := &commandTokenProvider{}
	now := time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC)
	reader.planDetailName = "测试商品-F2 达人-20260816040102"
	service := CommandService{
		Config: commandConfigReader{config: commandProductConfig()}, Tokens: tokens, Links: links,
		Verifier: WorkVerifier{Reader: reader},
		Batch: BatchService{
			Guard:  sharedplans.GuardedExecutor{Locks: commandLocker{}},
			Reader: reader, Writer: writer, Reconciler: reconciler, Bindings: bindings,
			Now: func() time.Time { return now },
		},
		Locks: commandLocker{}, Journals: journals,
		NewPreflightID: func(string, time.Time) (string, error) {
			return "qianchuan-preflight-fast-submit", nil
		},
		Now: func() time.Time { return now },
	}
	preview, err := service.BatchWorks(context.Background(), BatchWorksCommand{
		PlanTemplate: "qcpt_command", Items: batchCommandItems("https://www.douyin.com/video/6000000000000001"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.PreflightID != "qianchuan-preflight-fast-submit" || links.calls != 1 || reconciler.scanCalls != 1 {
		t.Fatalf("preflight snapshot was not prepared: %#v links=%d scans=%d", preview, links.calls, reconciler.scanCalls)
	}
	authorizedCalls, videoCalls := reader.authorizedCalls, reader.videoCalls
	now = now.Add(time.Minute)
	tokens.onEnsure = func() { now = now.Add(2 * time.Second) }
	submitted, err := service.BatchWorks(context.Background(), BatchWorksCommand{
		PreflightID: preview.PreflightID, Submit: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if submitted.Mode != "submit" || len(submitted.Results) != 1 || submitted.Results[0].Status != "created" ||
		links.calls != 1 || reader.authorizedCalls != authorizedCalls || reader.videoCalls != videoCalls ||
		reconciler.scanCalls != 2 || writer.createCalls != 1 ||
		len(bindings.bindings) != 1 ||
		submitted.Performance.Stages.CredentialResolutionSeconds != 2 ||
		submitted.Performance.LinkMetadata.Provider != "preflight_snapshot" ||
		submitted.Performance.LinkMetadata.Enabled {
		t.Fatalf("fast submit repeated preparation or missed write: result=%#v links=%d auth=%d/%d videos=%d/%d scans=%d writes=%d",
			submitted, links.calls, reader.authorizedCalls, authorizedCalls, reader.videoCalls, videoCalls,
			reconciler.scanCalls, writer.createCalls)
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
			AwemeItemID: "6000000000000001", CreatorName: "F2 达人",
			OwnerHint: &domain.WorkOwnerHint{
				AwemeID: batchCreatorID, AwemeShowID: batchVisibleID,
			},
		}},
	}}
	now := time.Date(2026, time.July, 26, 12, 0, 0, 0, time.UTC)
	journals := &commandJournalStore{onSave: func() { now = now.Add(3 * time.Second) }}
	service := CommandService{
		Config: commandConfigReader{config: commandProductConfig()}, Tokens: tokens, Links: links,
		Verifier: WorkVerifier{Reader: reader},
		Batch:    BatchService{Reader: reader, Reconciler: commandNoPlanFinder{}},
		Locks:    commandLocker{},
		Journals: journals,
		NewPreflightID: func(string, time.Time) (string, error) {
			return "qianchuan-preflight-fixture", nil
		},
		Now: func() time.Time { return now },
	}
	result, err := service.BatchWorks(context.Background(), BatchWorksCommand{
		PlanTemplate: "qcpt_command", Items: batchCommandItems("https://www.douyin.com/video/6000000000000001"),
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
		len(result.Results[0].Writes[0].Payload) == 0 || result.PreflightID != "qianchuan-preflight-fixture" ||
		result.ExpiresAt != "2026-07-26T12:30:00Z" || result.Performance.Stages.SnapshotPersistenceSeconds != 3 ||
		result.Performance.Stages.TotalRuntimeSeconds != 3 {
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
	for _, legacy := range []string{"material_resolution_seconds", "plan_reconciliation_seconds", "total_seconds"} {
		if strings.Contains(string(encoded), `"`+legacy+`"`) {
			t.Fatalf("batch result exposed legacy cumulative timing %q: %s", legacy, encoded)
		}
	}

	resultWithoutPayload, err := service.BatchWorks(context.Background(), BatchWorksCommand{
		PlanTemplate: "qcpt_command", Items: batchCommandItems("https://www.douyin.com/video/6000000000000001"),
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
		Locks: commandLocker{}, Journals: &commandJournalStore{},
		NewPreflightID: func(string, time.Time) (string, error) {
			return "qianchuan-preflight-owner-hint", nil
		},
	}
	result, err := service.BatchWorks(context.Background(), BatchWorksCommand{
		PlanTemplate: "qcpt_command", Items: batchCommandItems("https://www.douyin.com/video/6000000000000001"),
	})
	if err != nil {
		t.Fatal(err)
	}
	performance := result.Performance.OwnerHintCache
	warning, _ := performance.Warning.(map[string]string)
	if tokens.calls != 1 || cache.loadCalls != 1 || cache.storeCalls != 1 ||
		performance.Loaded != 1 || performance.LoadedFromCache != 1 ||
		performance.LoadedFromLinkMetadata != 1 || performance.Verified != 1 ||
		performance.Stored != 0 ||
		warning["code"] != "owner_hint_cache_write_failed" {
		t.Fatalf("owner hint precedence/cache warning contract changed: performance=%#v cache=%#v", performance, cache)
	}
	stored := cache.stored["6000000000000001"]
	if stored.AwemeID != batchCreatorID || stored.AwemeShowID != batchVisibleID || reader.broadCalls != 0 {
		t.Fatalf("metadata hint did not override stale cache before official verification: stored=%#v reader=%#v", stored, reader)
	}
}

func testBatchStagedOwnerHintCache(t *testing.T) {
	reader := &hintVerificationReader{
		targetedCreator: domainqianchuan.AuthorizedCreator{
			AwemeID: batchCreatorID, VisibleID: batchVisibleID, Name: "fixture-creator",
		},
		actualCreatorID: batchCreatorID,
	}
	resolver := &stagedCommandLinkResolver{result: applicationworkmetadata.ResolveResult{
		Resolved: []domain.ResolvedWorkLink{
			{InputIndex: 0, InputURL: "https://www.douyin.com/video/6000000000000001", AwemeItemID: "6000000000000001"},
			{InputIndex: 1, InputURL: "https://www.douyin.com/video/6000000000000002", AwemeItemID: "6000000000000002"},
		},
	}}
	cache := &commandOwnerHintCache{loaded: map[string]OwnerHint{
		"6000000000000001": {AwemeID: batchCreatorID, AwemeShowID: batchVisibleID},
	}}
	locks := &countingCommandLocker{}
	service := CommandService{
		Config: commandConfigReader{config: commandProductConfig()}, Tokens: &commandTokenProvider{}, Links: resolver,
		OwnerHints: cache, Verifier: WorkVerifier{Reader: reader},
		Batch: BatchService{Reader: reader, Reconciler: commandNoPlanFinder{}}, Locks: locks,
		Journals: &commandJournalStore{}, NewPreflightID: func(string, time.Time) (string, error) {
			return "qianchuan-preflight-staged-cache", nil
		},
	}
	result, err := service.BatchWorks(context.Background(), BatchWorksCommand{
		PlanTemplate: "qcpt_command",
		Items: []domainqianchuan.BatchItem{
			{InputIndex: 0, WorkURL: "https://www.douyin.com/video/6000000000000001"},
			{InputIndex: 1, WorkURL: "https://www.douyin.com/video/6000000000000002"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolver.metadataCalls != 1 || !reflect.DeepEqual(resolver.metadataIDs, []string{"6000000000000002"}) {
		t.Fatalf("staged resolver did not restrict F2 to cache misses: calls=%d ids=%v", resolver.metadataCalls, resolver.metadataIDs)
	}
	if cache.loadCalls != 1 || result.Performance.OwnerHintCache.LoadedFromCache != 1 || locks.acquires != 0 {
		t.Fatalf("cache/lock boundary changed: cache=%#v locks=%d", result.Performance.OwnerHintCache, locks.acquires)
	}
	if len(result.Results) != 1 || result.Results[0].Status != "would_create" {
		t.Fatalf("hot/cold staged preflight did not preserve business result: %#v", result.Results)
	}
}

func testBatchReadOnlyLockBoundary(t *testing.T) {
	links := &commandLinkResolver{result: applicationworkmetadata.ResolveResult{Resolved: []domain.ResolvedWorkLink{{
		InputIndex: 0, InputURL: "https://www.douyin.com/video/6000000000000001", AwemeItemID: "6000000000000001",
		OwnerHint: &domain.WorkOwnerHint{AwemeID: batchCreatorID, AwemeShowID: batchVisibleID},
	}}}}
	locks := &countingCommandLocker{}
	service := CommandService{
		Config: commandConfigReader{config: commandProductConfig()}, Tokens: &commandTokenProvider{}, Links: links,
		Verifier: WorkVerifier{Reader: &commandReader{}}, Batch: BatchService{Reader: &commandReader{}, Reconciler: commandNoPlanFinder{}},
		Locks: locks, Journals: &commandJournalStore{}, NewPreflightID: func(string, time.Time) (string, error) {
			return "qianchuan-preflight-read-only-lock", nil
		},
	}
	if _, err := service.BatchWorks(context.Background(), BatchWorksCommand{
		PlanTemplate: "qcpt_command", Items: batchCommandItems(links.result.Resolved[0].InputURL),
	}); err != nil {
		t.Fatal(err)
	}
	if locks.acquires != 0 {
		t.Fatalf("read-only preflight acquired advertiser write lock %d times", locks.acquires)
	}
}

func batchCommandItems(workURLs ...string) []domainqianchuan.BatchItem {
	result := make([]domainqianchuan.BatchItem, len(workURLs))
	for index, workURL := range workURLs {
		result[index] = domainqianchuan.BatchItem{InputIndex: index, WorkURL: workURL}
	}
	return result
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

type commandConfigReader struct {
	config map[string]any
	err    error
}

func (reader commandConfigReader) Read(context.Context) (map[string]any, error) {
	return reader.config, reader.err
}

type commandJournalStore struct {
	journals map[string]domainplans.Journal
	onSave   func()
}

func (store *commandJournalStore) Load(_ context.Context, runID string) (domainplans.Journal, error) {
	if store == nil || store.journals == nil {
		return domainplans.Journal{}, errors.New("fixture journal not found")
	}
	journal, exists := store.journals[runID]
	if !exists {
		return domainplans.Journal{}, fmt.Errorf("fixture journal %s not found", runID)
	}
	return journal, nil
}

func (store *commandJournalStore) Save(_ context.Context, runID string, journal domainplans.Journal) error {
	if store.onSave != nil {
		store.onSave()
	}
	if store.journals == nil {
		store.journals = map[string]domainplans.Journal{}
	}
	store.journals[runID] = journal
	return nil
}

type commandTokenProvider struct {
	calls    int
	last     authapplication.TokenQuery
	onEnsure func()
	err      error
}

type blockingCommandTokenProvider struct {
	started chan struct{}
	once    sync.Once
}

func (provider *blockingCommandTokenProvider) Ensure(
	ctx context.Context,
	_ authapplication.TokenQuery,
) (authapplication.TokenLease, error) {
	provider.once.Do(func() { close(provider.started) })
	<-ctx.Done()
	return authapplication.TokenLease{}, ctx.Err()
}

const commandToken = "TEST_QIANCHUAN_COMMAND_TOKEN_DO_NOT_USE"

func (provider *commandTokenProvider) Ensure(_ context.Context, query authapplication.TokenQuery) (authapplication.TokenLease, error) {
	provider.calls++
	provider.last = query
	if provider.onEnsure != nil {
		provider.onEnsure()
	}
	if provider.err != nil {
		return authapplication.TokenLease{}, provider.err
	}
	return authapplication.TokenLease{
		Channel: "qianchuan", AuthorizationID: "fixture-command-authorization", AccessToken: commandToken,
	}, nil
}

type commandLinkResolver struct {
	calls  int
	result applicationworkmetadata.ResolveResult
}

type stagedCommandLinkResolver struct {
	result        applicationworkmetadata.ResolveResult
	metadataIDs   []string
	metadataCalls int
}

func (resolver *stagedCommandLinkResolver) Resolve(context.Context, applicationworkmetadata.ResolveRequest) (applicationworkmetadata.ResolveResult, error) {
	return resolver.result, nil
}

func (resolver *stagedCommandLinkResolver) ResolveLinks(context.Context, applicationworkmetadata.ResolveRequest) (applicationworkmetadata.ResolveResult, error) {
	return resolver.result, nil
}

func (resolver *stagedCommandLinkResolver) ResolveMetadata(_ context.Context, result applicationworkmetadata.ResolveResult, _ int) (applicationworkmetadata.ResolveResult, error) {
	resolver.metadataCalls++
	for _, row := range result.Resolved {
		resolver.metadataIDs = append(resolver.metadataIDs, row.AwemeItemID)
		row.CreatorName = "F2 达人"
		row.OwnerHint = &domain.WorkOwnerHint{AwemeID: batchCreatorID, AwemeShowID: batchVisibleID}
		row.ProductHint = &domain.WorkProductHint{ProductID: batchProductID, ProductName: "测试商品"}
		result.Resolved = []domain.ResolvedWorkLink{row}
		break
	}
	return result, nil
}

type blockingCommandLinkResolver struct {
	calls       int
	scanStarted <-chan struct{}
	result      applicationworkmetadata.ResolveResult
}

func (resolver *blockingCommandLinkResolver) Resolve(
	ctx context.Context,
	_ applicationworkmetadata.ResolveRequest,
) (applicationworkmetadata.ResolveResult, error) {
	resolver.calls++
	select {
	case <-resolver.scanStarted:
		return resolver.result, nil
	case <-ctx.Done():
		return applicationworkmetadata.ResolveResult{}, ctx.Err()
	}
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

type commandReader struct {
	unscopedCalls   int
	authorizedCalls int
	videoCalls      int
	planDetailName  string
	authorizedDelay time.Duration
}

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

func (reader *commandReader) FetchPlanDetail(_ context.Context, request portqianchuan.PlanDetailRequest) (domainqianchuan.PlanDetail, error) {
	return domainqianchuan.PlanDetail{
		AdID: request.AdID, AwemeID: batchCreatorID, Name: reader.planDetailName,
		Status: "ENABLE", Products: []domainqianchuan.PlanProduct{{ProductID: batchProductID}},
	}, nil
}

func (*commandReader) FetchPlanMaterials(context.Context, portqianchuan.MaterialPageRequest) (domainqianchuan.MaterialPage, error) {
	return domainqianchuan.MaterialPage{}, errors.New("unexpected material query")
}

func (reader *commandReader) FetchAuthorizedCreators(ctx context.Context, _ portqianchuan.AuthorizedCreatorPageRequest) (domainqianchuan.AuthorizedCreatorPage, error) {
	reader.checkScope(ctx)
	reader.authorizedCalls++
	if reader.authorizedDelay > 0 {
		time.Sleep(reader.authorizedDelay)
	}
	return domainqianchuan.AuthorizedCreatorPage{
		Rows: []domainqianchuan.AuthorizedCreator{{
			AwemeID: batchCreatorID, VisibleID: batchVisibleID, Name: "fixture-creator",
		}},
		PageInfo: domainqianchuan.PageInfo{Page: 1, TotalPages: 1, TotalNumber: 1},
	}, nil
}

func (reader *commandReader) FetchCreatorVideos(ctx context.Context, request portqianchuan.CreatorVideoPageRequest) (domainqianchuan.CreatorVideoPage, error) {
	reader.checkScope(ctx)
	reader.videoCalls++
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
	result := CurrentPlanResult{Matches: map[string][]ExistingPlan{}, Policies: map[string]PlanMatchPolicy{}}
	bindingDigest, _ := PlanBindingDigest(nil)
	for _, target := range request.Targets {
		key := target.GroupID
		if key == "" {
			key = target.AwemeID
		}
		result.Matches[key] = []ExistingPlan{}
		if target.GroupID != "" {
			result.Policies[key] = PlanMatchPolicy{Status: "would_create", BindingDigest: bindingDigest}
		}
	}
	return result, nil
}

type commandSnapshotReconciler struct {
	scanStarted chan struct{}
	scanOnce    sync.Once
	scanCalls   int
	scanErr     error
}

func (reconciler *commandSnapshotReconciler) ScanCurrentPlans(
	context.Context,
	CurrentPlanScanRequest,
) (CurrentPlanInventory, error) {
	reconciler.scanCalls++
	if reconciler.scanStarted != nil {
		reconciler.scanOnce.Do(func() { close(reconciler.scanStarted) })
	}
	if reconciler.scanErr != nil {
		return CurrentPlanInventory{}, reconciler.scanErr
	}
	return CurrentPlanInventory{
		StartTime: "2026-08-16 00:00:00", EndTime: "2026-08-16 23:59:59", PageCount: 1,
		Plans: []domainqianchuan.Plan{},
	}, nil
}

func (*commandSnapshotReconciler) FindCurrentPlans(
	_ context.Context,
	request CurrentPlanRequest,
) (CurrentPlanResult, error) {
	if request.Inventory == nil {
		return CurrentPlanResult{}, errors.New("expected pre-scanned inventory")
	}
	return commandNoPlanFinder{}.FindCurrentPlans(context.Background(), request)
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

type commandSuccessfulWriter struct{ createCalls int }

func (writer *commandSuccessfulWriter) CreatePlan(context.Context, portqianchuan.CreatePlanRequest) (portqianchuan.WriteResult, error) {
	writer.createCalls++
	return portqianchuan.WriteResult{ObjectID: batchPlanID, RequestID: "fixture-create-request"}, nil
}

func (*commandSuccessfulWriter) AddMaterials(context.Context, portqianchuan.MaterialWriteRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected material add")
}

func (*commandSuccessfulWriter) DeleteMaterials(context.Context, portqianchuan.DeleteMaterialsRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected material delete")
}

func (*commandSuccessfulWriter) UpdatePlan(context.Context, portqianchuan.MutationRequest) (portqianchuan.WriteResult, error) {
	return portqianchuan.WriteResult{}, errors.New("unexpected plan mutation")
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

type countingCommandLocker struct{ acquires int }

func (locker *countingCommandLocker) Acquire(context.Context, domainplans.WriteScope) (func() error, error) {
	locker.acquires++
	return func() error { return nil }, nil
}

func commandProductConfig() map[string]any {
	return map[string]any{
		"qianchuan_product_template_schema_version": 8,
		"qianchuan_product_templates": map[string]any{
			"qcpt_command": map[string]any{
				"template_id":   "qcpt_command",
				"display_name":  "巨量千川-1000000000000001-测试商品-5000000000000001-商品全域",
				"template_type": "QIANCHUAN_PRODUCT_ALL_DOMAIN", "status": "active",
				"bindings": map[string]any{
					"channel": "qianchuan", "advertiser_id": batchAdvertiserID,
					"product_name": "测试商品", "product_short_name": "测试商品", "product_ids": []any{batchProductID},
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
