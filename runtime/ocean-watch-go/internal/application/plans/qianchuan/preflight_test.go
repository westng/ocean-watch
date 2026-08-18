package qianchuan

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
)

func TestGetBatchPreflightReturnsStableSanitizedSummary(t *testing.T) {
	createdAt := time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC)
	works := batchVerifiedWorks(2)
	works[1].Creator.AwemeID = "4000000000000002"
	works[1].MatchedProductIDs = []string{"5000000000000002", batchProductID}
	snapshot, eligible, err := prepareBatchSnapshot(BatchRequest{
		AdvertiserID: batchAdvertiserID, AuthAccountID: "private-auth-selector",
		TemplateID: "fixture-template", TemplateName: "fixture-template-name",
		ProductName: "fixture-product", ProductShortName: "fixture-short",
		TemplatePayload: json.RawMessage(`{"private":"payload"}`), Works: works,
		Skipped: []SkippedWork{{InputURL: "https://v.douyin.com/private/", Reason: "invalid"}},
	}, BatchResult{Results: []BatchGroupResult{
		{AwemeID: "4000000000000002", AdID: "2000000000000002", Status: "would_append"},
		{AwemeID: batchCreatorID, Status: "would_create"},
	}}, createdAt)
	if err != nil || !eligible {
		t.Fatalf("prepare snapshot: eligible=%t err=%v", eligible, err)
	}
	journal, err := batchPreflightJournal(snapshot, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	const preflightID = "qianchuan-preflight-20260816t040000-abcdef123456"
	tokens := &commandTokenProvider{}
	store := &commandJournalStore{journals: map[string]domainplans.Journal{preflightID: journal}}
	service := CommandService{
		Journals: store, Tokens: tokens,
		Now: func() time.Time { return createdAt.Add(time.Minute) },
	}
	summary, err := service.GetBatchPreflight(context.Background(), preflightID)
	if err != nil {
		t.Fatal(err)
	}
	if tokens.calls != 0 || summary.PreflightID != preflightID || !summary.ReadyForSubmit ||
		summary.EligibleWorks != 2 || summary.SkippedWorks != 1 ||
		!reflect.DeepEqual(summary.ProductIDs, []string{batchProductID, "5000000000000002"}) ||
		!reflect.DeepEqual(summary.Decisions, []BatchPreflightDecision{
			{CreatorID: "4000000000000001", Action: "create"},
			{CreatorID: "4000000000000002", Action: "append", ExistingPlanID: "2000000000000002"},
		}) {
		t.Fatalf("unexpected preflight summary: %#v token_calls=%d", summary, tokens.calls)
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private-auth-selector", "private", "v.douyin.com", "template_payload"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("preflight summary leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestGetBatchPreflightFailsClosed(t *testing.T) {
	createdAt := time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC)
	snapshot, _, err := prepareBatchSnapshot(BatchRequest{
		AdvertiserID: batchAdvertiserID, TemplateID: "fixture", TemplateName: "fixture",
		TemplatePayload: batchTemplatePayload(), Works: batchVerifiedWorks(1),
	}, BatchResult{Results: []BatchGroupResult{{AwemeID: batchCreatorID, Status: "would_create"}}}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := batchPreflightJournal(snapshot, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	const preflightID = "qianchuan-preflight-20260816t040000-abcdef123456"
	service := CommandService{
		Journals: &commandJournalStore{journals: map[string]domainplans.Journal{preflightID: journal}},
		Now:      func() time.Time { return createdAt.Add(31 * time.Minute) },
	}
	if _, err := service.GetBatchPreflight(context.Background(), preflightID); !errors.Is(err, ErrBatchPreflightExpired) {
		t.Fatalf("expired preflight error=%v", err)
	}
	service.Journals = missingPreflightStore{}
	if _, err := service.GetBatchPreflight(context.Background(), preflightID); !errors.Is(err, ErrBatchPreflightNotFound) {
		t.Fatalf("missing preflight error=%v", err)
	}
	if _, err := service.GetBatchPreflight(context.Background(), "../private"); !errors.Is(err, ErrBatchPreflightInvalid) {
		t.Fatalf("invalid preflight error=%v", err)
	}
	other, err := domainplans.NewJournal("fixture-fingerprint", map[string]domainplans.JournalJob{
		"fixture": {Status: "prepared"},
	}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	service.Journals = &commandJournalStore{journals: map[string]domainplans.Journal{preflightID: other}}
	service.Now = func() time.Time { return createdAt.Add(time.Minute) }
	if _, err := service.GetBatchPreflight(context.Background(), preflightID); !errors.Is(err, ErrBatchPreflightInvalid) {
		t.Fatalf("non-preflight journal error=%v", err)
	}
}

type missingPreflightStore struct{}

func (missingPreflightStore) Load(context.Context, string) (domainplans.Journal, error) {
	return domainplans.Journal{}, os.ErrNotExist
}

func (missingPreflightStore) Save(context.Context, string, domainplans.Journal) error { return nil }

func TestBatchPreflightSnapshotLifecycle(t *testing.T) {
	createdAt := time.Date(2026, 8, 16, 4, 0, 0, 0, time.UTC)
	request := BatchRequest{
		AdvertiserID: batchAdvertiserID, AuthAccountID: "fixture-auth-account",
		TemplateID: "fixture-template", TemplateName: "fixture-template-name",
		ProductName: "fixture-product", ProductShortName: "fixture-short",
		PlanNameTemplate: "{creator_name}-{product_short_name}",
		TemplatePayload:  batchTemplatePayload(), IncludePayloads: true,
		Works:         batchVerifiedWorks(1),
		Skipped:       []SkippedWork{{InputIndex: 2, InputURL: "https://v.douyin.com/private-input/", Reason: "invalid", Message: "fixture"}},
		QueryFailures: []WorkQueryFailure{{AwemeID: batchCreatorID, Message: "raw query failure"}},
	}
	request.Works[0].Material.URL = "https://example.invalid/video"
	request.Works[0].Material.VideoCoverURL = "https://example.invalid/cover"
	result := BatchResult{Results: []BatchGroupResult{{
		AwemeID: batchCreatorID, Status: "would_create",
	}}}

	snapshot, eligible, err := prepareBatchSnapshot(request, result, createdAt)
	if err != nil || !eligible {
		t.Fatalf("prepare snapshot: eligible=%t err=%v", eligible, err)
	}
	if snapshot.ExpiresAt != "2026-08-16T04:30:00Z" || snapshot.AuthAccountID != "fixture-auth-account" {
		t.Fatalf("snapshot expiry or authorization selector changed: %#v", snapshot)
	}
	if len(snapshot.Works) != 1 || snapshot.Works[0].InputURL != "" ||
		snapshot.Works[0].Material.URL != "" || snapshot.Works[0].Material.VideoCoverURL != "" ||
		len(snapshot.QueryFailures) != 0 || len(snapshot.Skipped) != 1 || snapshot.Skipped[0].InputURL != "" {
		t.Fatalf("snapshot retained unnecessary source or response data: %#v", snapshot)
	}
	journal, err := batchPreflightJournal(snapshot, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeBatchPreflight(journal, createdAt.Add(29*time.Minute))
	if err != nil || decoded.TemplateDigest != snapshot.TemplateDigest || len(decoded.Works) != 1 {
		t.Fatalf("decode snapshot: decoded=%#v err=%v", decoded, err)
	}
	if _, err := decodeBatchPreflight(journal, createdAt.Add(30*time.Minute)); err == nil ||
		!strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired snapshot was accepted: %v", err)
	}

	tampered := journal
	tampered.Jobs = map[string]domainplans.JournalJob{}
	if _, err := decodeBatchPreflight(tampered, createdAt); err == nil ||
		!strings.Contains(err.Error(), "jobs do not match") {
		t.Fatalf("tampered journal jobs were accepted: %v", err)
	}

	tampered = journal
	var extended preparedBatchSnapshot
	if err := json.Unmarshal(tampered.Extra["snapshot"], &extended); err != nil {
		t.Fatal(err)
	}
	extended.ExpiresAt = createdAt.Add(2 * time.Hour).Format(time.RFC3339Nano)
	tampered.Extra["snapshot"], err = json.Marshal(extended)
	if err != nil {
		t.Fatal(err)
	}
	tampered.Fingerprint, err = batchSnapshotFingerprint(extended)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeBatchPreflight(tampered, createdAt); err == nil ||
		!strings.Contains(err.Error(), "timestamps are invalid") {
		t.Fatalf("extended snapshot lifetime was accepted: %v", err)
	}
}

func TestBatchPreflightExpiresAtShanghaiBusinessDayEnd(t *testing.T) {
	createdAt := time.Date(2026, 8, 16, 15, 50, 0, 0, time.UTC)
	expiresAt, err := batchPreflightExpiry(createdAt)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 8, 16, 16, 0, 0, 0, time.UTC); !expiresAt.Equal(want) {
		t.Fatalf("cross-day expiry=%s want=%s", expiresAt, want)
	}
}

func TestBatchTemplateDigestUsesCanonicalJSONNumbers(t *testing.T) {
	first := BatchRequest{
		AdvertiserID: batchAdvertiserID, TemplateID: "fixture", TemplateName: "fixture",
		ProductName: "fixture", ProductShortName: "fixture", PlanNameTemplate: "{creator_name}",
		TemplatePayload: json.RawMessage(`{"advertiser_id":1000000000000001,"budget":5000}`),
	}
	second := first
	second.TemplatePayload = json.RawMessage("{\n  \"budget\": 5000,\n  \"advertiser_id\": 1000000000000001\n}")
	if batchTemplateDigest(first) != batchTemplateDigest(second) {
		t.Fatal("semantically identical template JSON produced different digests")
	}
	firstSnapshot := preparedBatchSnapshot{TemplatePayload: first.TemplatePayload}
	secondSnapshot := preparedBatchSnapshot{TemplatePayload: second.TemplatePayload}
	firstFingerprint, firstErr := batchSnapshotFingerprint(firstSnapshot)
	secondFingerprint, secondErr := batchSnapshotFingerprint(secondSnapshot)
	if firstErr != nil || secondErr != nil || firstFingerprint != secondFingerprint {
		t.Fatalf("semantically identical template JSON produced different snapshot fingerprints: %v %v", firstErr, secondErr)
	}
}

func TestBatchSubmitIsolatesChangedPreflightCreator(t *testing.T) {
	const changedCreatorID = "4000000000000002"
	works := batchVerifiedWorks(1)
	changedWork := batchVerifiedWorks(1)[0]
	changedWork.InputIndex = 1
	changedWork.AwemeItemID = "6000000000000002"
	changedWork.Creator = domainqianchuan.AuthorizedCreator{
		AwemeID: changedCreatorID, VisibleID: "changed-visible", Name: "changed-creator",
	}
	changedWork.CreatorName = "changed-creator"
	changedWork.Material.AwemeItemID = changedWork.AwemeItemID
	works = append(works, changedWork)
	reader := &batchStateReader{materials: map[string]domainqianchuan.PlanMaterial{
		works[0].AwemeItemID: {
			MaterialID: "material-" + works[0].AwemeItemID, AwemeItemID: works[0].AwemeItemID,
			MaterialType: "VIDEO", MaterialSelectType: "CUSTOM", MaterialStatus: "DELIVERY_OK",
		},
	}}
	writer := &batchStateWriter{reader: reader}
	service := BatchService{
		Guard:  sharedplans.GuardedExecutor{Credentials: &batchCredentialProvider{}, Locks: &batchLocker{}},
		Reader: reader, Writer: writer,
		Reconciler: batchPreflightFinder{matches: map[string][]ExistingPlan{
			batchCreatorID: {{
				AdID: batchPlanID, Name: "fixture-plan", Status: "DISABLE",
				AwemeID: batchCreatorID, ProductIDs: []string{batchProductID},
			}},
			changedCreatorID: {{
				AdID: "2000000000000002", Name: "new-plan", Status: "ENABLE",
				AwemeID: changedCreatorID, ProductIDs: []string{batchProductID},
			}},
		}},
	}
	result, err := service.Execute(context.Background(), BatchRequest{
		AdvertiserID: batchAdvertiserID, Submit: true,
		TemplateID: "fixture-template", TemplateName: "fixture-template-name", ProductName: "fixture-product",
		TemplatePayload: batchTemplatePayload(), Works: works,
		Expected: map[string]batchExpectedDecision{
			batchCreatorID:   {Action: "append", AdID: batchPlanID},
			changedCreatorID: {Action: "create"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 2 || result.Results[0].Status != "already_present" ||
		result.Results[1].Status != "preflight_changed" || result.ExitCode != 1 || writer.addCalls != 0 {
		t.Fatalf("preflight job isolation changed: %#v", result)
	}
}

type batchPreflightFinder struct {
	matches map[string][]ExistingPlan
}

func (finder batchPreflightFinder) FindCurrentPlans(
	_ context.Context,
	request CurrentPlanRequest,
) (CurrentPlanResult, error) {
	result := CurrentPlanResult{Matches: map[string][]ExistingPlan{}}
	for _, target := range request.Targets {
		result.Matches[target.AwemeID] = append([]ExistingPlan(nil), finder.matches[target.AwemeID]...)
	}
	return result, nil
}
