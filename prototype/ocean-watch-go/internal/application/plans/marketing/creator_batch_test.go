package marketing

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	domainmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/marketing"
	domainplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/plans"
)

func TestMarketingBatchCreatorManifestContracts(t *testing.T) {
	payload := creatorBatchManifestPayload(t, []map[string]any{
		{
			"aweme_id": "8001", "item_ids": []any{"9002", "9001", "9001"},
			"product_match": map[string]any{"status": "matched", "evidence": "fixture match"},
		},
	})
	manifest, err := ParseCreatorBatchManifest(payload, "")
	if err != nil {
		t.Fatal(err)
	}
	job := manifest.Jobs[0]
	if manifest.Channel != "marketing" || job.AdvertiserID != "1234567890" ||
		job.PlanTemplate != "creator-template" || !reflect.DeepEqual(job.ItemIDs, []string{"9002", "9001"}) ||
		job.ProductMatch.Status != "MATCHED" || !job.budgetSet {
		t.Fatalf("creator manifest normalization changed: %#v", manifest)
	}

	invalid := creatorBatchManifestValue([]map[string]any{{
		"aweme_id": "8001", "item_ids": []any{"9001"},
	}})
	invalidPayload, err := json.Marshal(invalid)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ParseCreatorBatchManifest(invalidPayload, "")
	var inputErr *BatchInputError
	if !errors.As(err, &inputErr) || inputErr.Code != "product_match_confirmation_required" {
		t.Fatalf("missing product evidence was accepted: %v", err)
	}
}

func TestMarketingBatchCreatorSubmitPreservesPreflightAndPublicStatus(t *testing.T) {
	store := &memoryJournalStore{}
	executor := &creatorBatchExecutorStub{}
	service := creatorBatchFixtureService(store, executor)
	result, err := service.Execute(context.Background(), CreatorBatchRequest{
		ManifestPayload: creatorBatchManifestPayload(t, nil), Submit: true, MaxConcurrency: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.Counts["created"] != 1 || result.Rows[0].Status != "created" {
		t.Fatalf("creator submit public status changed: %#v", result)
	}
	if result.Preflight.ReadyToSubmit != 1 || result.Preflight.AlreadyCompleted != 0 ||
		result.Preflight.Blocked != 0 || result.Preflight.ConfirmationRequired {
		t.Fatalf("submit replaced its preflight snapshot: %#v", result.Preflight)
	}
	if result.Rows[0].MissingFields == nil || len(*result.Rows[0].MissingFields) != 0 {
		t.Fatalf("ready submit must preserve an explicit empty missing_fields array: %#v", result.Rows[0])
	}
	journal := store.snapshot()
	for _, job := range journal.Jobs {
		if job.Status != "completed" || job.ProjectID != "2001" || job.PromotionID != "3001" {
			t.Fatalf("journal completion state changed: %#v", job)
		}
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), `"fingerprint"`) || !strings.Contains(string(payload), `"missing_fields":[]`) {
		t.Fatalf("creator batch public JSON changed: %s", payload)
	}
}

func TestMarketingBatchCreatorCompletedJobSkipsCurrentAuthorization(t *testing.T) {
	manifest, err := ParseCreatorBatchManifest(creatorBatchManifestPayload(t, nil), "")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := (Preparer{Config: staticMarketingConfig{value: marketingPrepareFixture(CreatorAuthorizedSource)}}).Prepare(
		context.Background(), creatorPrepareRequest(manifest.Jobs[0], CreatorBatchRequest{}, testTime(), false, ""),
	)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Jobs[0].MaterialDate = prepared.MaterialDate
	manifest.Jobs[0].ProjectName = textOrEmpty(prepared.ProjectPayload["name"])
	manifest.Jobs[0].PromotionName = textOrEmpty(prepared.PromotionPayload["name"])
	fingerprint, err := CreatorBatchFingerprint(manifest)
	if err != nil {
		t.Fatal(err)
	}
	jobKey := CreatorBatchJobKey(manifest.Channel, manifest.Jobs[0])
	journal, err := newCompletedCreatorBatchJournal(fingerprint, jobKey)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryJournalStore{journal: journal, exists: true}
	materials := &marketingMaterialStub{err: errors.New("completed job must not query authorization")}
	service := creatorBatchFixtureService(store, nil)
	service.Preparer.Materials = materials
	result, err := service.Execute(context.Background(), CreatorBatchRequest{
		ManifestPayload: creatorBatchManifestPayload(t, nil), Preflight: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(materials.calls) != 0 || result.Rows[0].Status != "skipped_completed" ||
		result.Rows[0].MissingFields != nil || result.Preflight.AlreadyCompleted != 1 {
		t.Fatalf("completed creator job was revalidated or exposed placeholder fields: %#v calls=%#v", result, materials.calls)
	}
}

func TestMarketingBatchCreatorSubmitRequiresExecutorOnlyForReadyJobs(t *testing.T) {
	service := creatorBatchFixtureService(&memoryJournalStore{}, nil)
	_, err := service.Execute(context.Background(), CreatorBatchRequest{
		ManifestPayload: creatorBatchManifestPayload(t, nil), Submit: true,
	})
	if err == nil || !strings.Contains(err.Error(), "transaction executor") {
		t.Fatalf("ready submit without an executor did not fail safely: %v", err)
	}

	blockedStore := &memoryJournalStore{}
	blocked := creatorBatchFixtureService(blockedStore, nil)
	blocked.Preparer.Materials = &marketingMaterialStub{err: errors.New("authorization snapshot unavailable")}
	result, err := blocked.Execute(context.Background(), CreatorBatchRequest{
		ManifestPayload: creatorBatchManifestPayload(t, nil), Submit: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 1 || result.Counts["blocked"] != 1 || result.Preflight.Blocked != 1 {
		t.Fatalf("blocked submit should not require an executor: %#v", result)
	}
}

func TestMarketingBatchCreatorBlocksHistoricalCoverAfterAuthorizationPeriodRejection(t *testing.T) {
	manifest, err := ParseCreatorBatchManifest(creatorBatchManifestPayload(t, nil), "")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := (Preparer{Config: staticMarketingConfig{value: marketingPrepareFixture(CreatorAuthorizedSource)}}).Prepare(
		context.Background(), creatorPrepareRequest(manifest.Jobs[0], CreatorBatchRequest{}, testTime(), false, ""),
	)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Jobs[0].MaterialDate = prepared.MaterialDate
	manifest.Jobs[0].ProjectName = textOrEmpty(prepared.ProjectPayload["name"])
	manifest.Jobs[0].PromotionName = textOrEmpty(prepared.PromotionPayload["name"])
	fingerprint, err := CreatorBatchFingerprint(manifest)
	if err != nil {
		t.Fatal(err)
	}
	jobKey := CreatorBatchJobKey(manifest.Channel, manifest.Jobs[0])
	code := int64(40000)
	journal, err := domainplans.NewJournal(fingerprint, map[string]domainplans.JournalJob{
		jobKey: {
			Status: "promotion_failed", AdvertiserID: "1234567890", ProjectID: "2001",
			FailureStage: "promotion_create", LastResponse: &domainplans.OfficialResponse{
				Code: &code, Message: "视频/图文[9001]不在授权期间，无法使用", RequestID: "request-old",
			},
		},
	}, testTime())
	if err != nil {
		t.Fatal(err)
	}
	service := creatorBatchFixtureService(&memoryJournalStore{journal: journal, exists: true}, nil)
	service.Preparer.Materials = &marketingMaterialStub{creatorCandidates: []domainmarketing.CreatorCandidate{{
		OwnerAdvertiserID: "1234567890", CreatorID: "8001", CreatorName: "达人甲",
		ItemID: "9001", MaterialID: "material-9001", VideoID: "video-9001",
		ImageMode: "CREATIVE_IMAGE_MODE_VIDEO_VERTICAL", Title: "作品甲", Usable: false,
		UnusableReasons: []string{"missing_video_cover_id"},
	}}}
	service.Preparer.CreatorCovers = creatorCoverResolverStub{}
	result, err := service.Execute(context.Background(), CreatorBatchRequest{
		ManifestPayload: creatorBatchManifestPayload(t, nil), Preflight: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	row := result.Rows[0]
	if result.ExitCode != 2 || result.Preflight.Blocked != 1 || row.Status != "blocked" ||
		row.ErrorCode != "creator_reauthorization_required" || row.ProjectID != "2001" ||
		row.PlannedOperation != "resume_promotion" {
		t.Fatalf("authorization-period rejection did not block historical-cover resume: %#v", result)
	}
	if result.Preflight.CreatorAuthorization["status"] != "CREATE_TIME_ONLY" ||
		result.Preflight.CreatorAuthorization["historical_cover_jobs"] != 1 ||
		row.CreatorCoverResolution["source"] != "matching_official_promotion" {
		t.Fatalf("historical-cover risk was not exposed: %#v", result)
	}
}

func creatorBatchFixtureService(store *memoryJournalStore, executor TransactionExecutor) CreatorBatchService {
	return CreatorBatchService{
		Preparer: Preparer{
			Config: staticMarketingConfig{value: marketingPrepareFixture(CreatorAuthorizedSource)},
			Materials: &marketingMaterialStub{creatorCandidates: []domainmarketing.CreatorCandidate{{
				OwnerAdvertiserID: "1234567890", CreatorID: "8001", CreatorName: "达人甲",
				ItemID: "9001", VideoID: "video-9001", VideoCoverID: "cover-9001",
				ImageMode: "CREATIVE_IMAGE_MODE_VIDEO_VERTICAL", Title: "作品甲", Usable: true,
			}}},
			RuntimeAssets: passthroughRuntimeAssets{},
		},
		Executor: executor, Journals: store, Now: testTime,
	}
}

func creatorBatchManifestPayload(t *testing.T, jobs []map[string]any) []byte {
	t.Helper()
	if jobs == nil {
		jobs = []map[string]any{{
			"aweme_id": "8001", "item_ids": []any{"9001"},
			"product_match": map[string]any{"status": "MATCHED", "evidence": "fixture match"},
		}}
	}
	payload, err := json.Marshal(creatorBatchManifestValue(jobs))
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func creatorBatchManifestValue(jobs []map[string]any) map[string]any {
	rows := make([]any, len(jobs))
	for index, job := range jobs {
		rows[index] = job
	}
	return map[string]any{
		"schema_version": 1, "channel": "marketing", "advertiser_id": "1234567890",
		"plan_template": "creator-template", "material_date": "7.26", "budget": 5000,
		"jobs": rows,
	}
}

func newCompletedCreatorBatchJournal(fingerprint, jobKey string) (domainplans.Journal, error) {
	return domainplans.NewJournal(fingerprint, map[string]domainplans.JournalJob{
		jobKey: {
			Status: "completed", AdvertiserID: "1234567890",
			ProjectID: "2001", PromotionID: "3001",
		},
	}, testTime())
}

type creatorBatchExecutorStub struct{}

type creatorCoverResolverStub struct{}

func (creatorCoverResolverStub) Resolve(
	_ context.Context,
	request CreatorCoverRequest,
) (CreatorCoverResult, error) {
	candidates := cloneCreatorCandidates(request.Candidates)
	for index := range candidates {
		candidates[index].VideoCoverID = "cover-history"
		candidates[index].UnusableReasons = withoutString(candidates[index].UnusableReasons, "missing_video_cover_id")
		candidates[index].Usable = len(candidates[index].UnusableReasons) == 0
	}
	return CreatorCoverResult{
		Candidates: candidates,
		Diagnostics: map[string]any{
			"status": "resolved", "source": "matching_official_promotion",
			"resolved": []any{map[string]any{"item_id": "9001", "video_cover_id": "cover-history"}},
		},
	}, nil
}

func (stub *creatorBatchExecutorStub) Execute(ctx context.Context, request Request) (Result, error) {
	if request.Checkpoint != nil {
		if err := request.Checkpoint(ctx, Checkpoint{Status: "project_created", ProjectID: "2001"}); err != nil {
			return Result{}, err
		}
		if err := request.Checkpoint(ctx, Checkpoint{
			Status: "completed", ProjectID: "2001", PromotionID: "3001",
		}); err != nil {
			return Result{}, err
		}
	}
	return Result{Status: "completed", ProjectID: "2001", PromotionID: "3001"}, nil
}

func testTime() time.Time {
	return time.Date(2026, time.July, 26, 12, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
}
