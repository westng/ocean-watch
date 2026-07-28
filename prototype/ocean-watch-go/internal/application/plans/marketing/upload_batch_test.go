package marketing

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	applicationmaterials "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/materials"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/configuration"
	domainmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/marketing"
	domainplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/plans"
)

func TestMarketingUploadBatchFreezesGroupsAndQueriesAccountAssetsOnce(t *testing.T) {
	store := newUploadJournalStoreStub()
	materials := newUploadMaterialStub(7)
	writer := &batchWriter{
		projectIDs: []string{"2001", "2002"},
		promotionResults: map[string][]batchPromotionResult{
			"2001": {{id: "3001"}}, "2002": {{id: "3002"}},
		},
	}
	service := uploadBatchFixtureService(store, materials, fixtureExecutor(writer, nil))
	result, err := service.Execute(context.Background(), uploadBatchFixtureRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.Mode != "submit" || len(result.Accounts) != 1 {
		t.Fatalf("unexpected upload batch result: %#v", result)
	}
	account := result.Accounts[0]
	if account.Status != "completed" || account.GroupCount != 2 ||
		account.CreatedProjectCount != 2 || account.CreatedPromotionCount != 2 {
		t.Fatalf("upload account summary changed: %#v", account)
	}
	if got := uploadVideoIDs(account.Groups[0].Videos); !reflect.DeepEqual(got, []string{
		"video-1", "video-2", "video-3", "video-4", "video-5",
	}) {
		t.Fatalf("first frozen group = %#v", got)
	}
	if got := uploadVideoIDs(account.Groups[1].Videos); !reflect.DeepEqual(got, []string{"video-6", "video-7"}) {
		t.Fatalf("second frozen group = %#v", got)
	}
	counts := materials.callCounts()
	if counts["library"] != 1 || counts["ad"] != 1 || counts["cover"] != 7 {
		t.Fatalf("account assets were queried per group: %#v", counts)
	}
	if !reflect.DeepEqual(writer.calls, []string{"project", "promotion:2001", "project", "promotion:2002"}) {
		t.Fatalf("same-advertiser writes were not serialized: %#v", writer.calls)
	}
	journal, ok := store.snapshot(result.RunID)
	if !ok || len(journal.Jobs) != 2 || journal.Extra["upload_batch"] == nil {
		t.Fatalf("frozen upload journal missing: %#v", journal)
	}
	metadata, err := decodeUploadMetadata(journal)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.ScopeFingerprint == journal.Fingerprint || len(metadata.Accounts[0].Groups) != 2 {
		t.Fatalf("scope and frozen transaction fingerprints were not separated: %#v", metadata)
	}
}

func TestMarketingUploadBatchResumeUsesFrozenVideosAndOnlyRetriesFailure(t *testing.T) {
	store := newUploadJournalStoreStub()
	materials := newUploadMaterialStub(7)
	firstWriter := &batchWriter{
		projectIDs: []string{"2001", "2002"},
		promotionResults: map[string][]batchPromotionResult{
			"2001": {{err: acknowledgedError("promotion rejected")}},
			"2002": {{id: "3002"}},
		},
	}
	service := uploadBatchFixtureService(store, materials, fixtureExecutor(firstWriter, nil))
	request := uploadBatchFixtureRequest()
	first, err := service.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.ExitCode != 1 || first.Accounts[0].Status != "completed_with_errors" {
		t.Fatalf("first partial result changed: %#v", first)
	}
	materials.addLibraryVideo("video-8")
	secondWriter := &batchWriter{promotionResults: map[string][]batchPromotionResult{
		"2001": {{id: "3001"}},
	}}
	service.Executor = fixtureExecutor(secondWriter, nil)
	second, err := service.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if second.ExitCode != 0 || second.RunID != first.RunID || second.Accounts[0].Status != "completed" {
		t.Fatalf("upload resume changed: first=%#v second=%#v", first, second)
	}
	if !reflect.DeepEqual(secondWriter.calls, []string{"promotion:2001"}) {
		t.Fatalf("resume recreated completed objects: %#v", secondWriter.calls)
	}
	if got := flattenUploadResultVideoIDs(second.Accounts[0].Groups); !reflect.DeepEqual(got, []string{
		"video-1", "video-2", "video-3", "video-4", "video-5", "video-6", "video-7",
	}) {
		t.Fatalf("resume regrouped today's changed library: %#v", got)
	}
	counts := materials.callCounts()
	if counts["library"] != 1 || counts["ad"] != 2 || counts["cover"] != 14 {
		t.Fatalf("resume request boundary changed: %#v", counts)
	}
	if materials.sawVideo("video-8") {
		t.Fatal("newly uploaded video entered an existing frozen batch")
	}
}

func TestMarketingUploadBatchPendingAccountCanResumeDiscovery(t *testing.T) {
	store := newUploadJournalStoreStub()
	materials := newUploadMaterialStub(2)
	materials.failNextLibrary(errors.New("temporary library failure"))
	writer := &batchWriter{
		projectIDs:       []string{"2001"},
		promotionResults: map[string][]batchPromotionResult{"2001": {{id: "3001"}}},
	}
	service := uploadBatchFixtureService(store, materials, fixtureExecutor(writer, nil))
	request := uploadBatchFixtureRequest()
	first, err := service.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.ExitCode != 1 || first.Accounts[0].Status != "query_failed" {
		t.Fatalf("failed discovery was not reported: %#v", first)
	}
	journal, ok := store.snapshot(first.RunID)
	if !ok || journal.Jobs[uploadAccountSentinelKey("1234567890")].Status != "account_pending" {
		t.Fatalf("pending account was not journaled: %#v", journal)
	}
	second, err := service.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if second.ExitCode != 0 || second.RunID != first.RunID || second.Accounts[0].CreatedPromotionCount != 1 {
		t.Fatalf("pending account did not resume: %#v", second)
	}
	journal, ok = store.snapshot(second.RunID)
	if !ok || len(journal.Jobs) != 1 {
		t.Fatalf("pending sentinel was not replaced by frozen jobs: %#v", journal)
	}
	if _, exists := journal.Jobs[uploadAccountSentinelKey("1234567890")]; exists {
		t.Fatal("pending sentinel remained after account discovery succeeded")
	}
	if materials.callCounts()["library"] != 2 {
		t.Fatalf("pending account discovery count changed: %#v", materials.callCounts())
	}
}

func TestMarketingUploadBatchAmbiguousUnfinishedScopeIsBlocked(t *testing.T) {
	store := newUploadJournalStoreStub()
	materials := newUploadMaterialStub(1)
	service := uploadBatchFixtureService(store, materials, fixtureExecutor(&batchWriter{}, nil))
	request := uploadBatchFixtureRequest()
	request.Submit = false
	preview, err := service.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	scope := uploadBatchScope{
		SchemaVersion: UploadBatchJournalVersion, Channel: "marketing", Accounts: preview.AccountTemplates,
		Date: "2026-07-26", MaterialDate: "7.26", VideosPerUnit: 5, StartIndex: 1,
		ValidateAdGet: true, SkipMissing: true,
	}
	scopeFingerprint, err := uploadFingerprint(scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, runID := range []string{"marketing-upload-one", "marketing-upload-two"} {
		metadata := uploadJournalMetadata{
			SchemaVersion: UploadBatchJournalVersion, Kind: UploadBatchJournalKind,
			ScopeFingerprint: scopeFingerprint, AccountTemplates: preview.AccountTemplates,
			Accounts: []uploadFrozenAccount{{
				AdvertiserID: "1234567890", PlanTemplate: "upload-template",
				Base: UploadBatchAccount{AdvertiserID: "1234567890", Status: "query_failed"},
			}},
		}
		fingerprint, fingerprintErr := uploadFingerprint(metadata)
		if fingerprintErr != nil {
			t.Fatal(fingerprintErr)
		}
		journal, journalErr := domainplans.NewJournal(fingerprint, map[string]domainplans.JournalJob{
			uploadAccountSentinelKey("1234567890"): {Status: "account_pending", AdvertiserID: "1234567890"},
		}, time.Now())
		if journalErr != nil {
			t.Fatal(journalErr)
		}
		journal.Extra = jsonRawMessageFixture{}.raw(metadata, scopeFingerprint)
		if saveErr := store.Save(context.Background(), runID, journal); saveErr != nil {
			t.Fatal(saveErr)
		}
	}
	request.Submit = true
	_, err = service.Execute(context.Background(), request)
	var inputErr *BatchInputError
	if !errors.As(err, &inputErr) || inputErr.Code != "ambiguous_batch_resume" {
		t.Fatalf("ambiguous unfinished scope was not blocked: %v", err)
	}
}

func TestMarketingUploadBatchBlockedGroupRemainsFrozenAndUnfinished(t *testing.T) {
	store := newUploadJournalStoreStub()
	materials := newUploadMaterialStub(1)
	config := marketingPrepareFixture(AccountUploadSource)
	defaultTemplate := configuration.Object(config["default_plan_template"])
	configuration.Object(defaultTemplate["resolved_ids"])["city_ids"] = []any{}
	writer := &batchWriter{}
	service := uploadBatchFixtureService(store, materials, fixtureExecutor(writer, nil))
	service.Config = staticMarketingConfig{value: config}

	result, err := service.Execute(context.Background(), uploadBatchFixtureRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 1 || result.Accounts[0].BlockedGroupCount != 1 || len(writer.calls) != 0 {
		t.Fatalf("blocked upload group crossed the write boundary: %#v calls=%#v", result, writer.calls)
	}
	journal, ok := store.snapshot(result.RunID)
	if !ok || uploadJournalCompleted(journal) || len(journal.Jobs) != 1 {
		t.Fatalf("blocked upload group was not retained as unfinished: %#v", journal)
	}
	metadata, err := decodeUploadMetadata(journal)
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata.Accounts) != 1 || len(metadata.Accounts[0].Groups) != 1 {
		t.Fatalf("blocked upload group disappeared from frozen metadata: %#v", metadata)
	}
}

func TestMarketingUploadBatchScopeFingerprintCanonicalizesEquivalentInputs(t *testing.T) {
	base := uploadBatchScope{
		SchemaVersion: UploadBatchJournalVersion, Channel: "marketing",
		Accounts: []UploadAccountTemplate{{AdvertiserID: "1234567890", PlanTemplate: "upload-template"}},
		Date:     "2026-07-26", Filename: "Drink.MP4", MaterialDate: "7.26",
		Budget: json.Number("2.0"), CPABid: json.Number("3e0"), ROIGoal: json.Number("4.00"),
		VideosPerUnit: 5, StartIndex: 1, ValidateAdGet: true, SkipMissing: true,
	}
	want, err := uploadScopeFingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	base.Filename = "drink.mp4"
	base.Budget = float64(2)
	base.CPABid = float64(3)
	base.ROIGoal = float64(4)
	got, err := uploadScopeFingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("equivalent upload scopes produced different fingerprints: got=%s want=%s", got, want)
	}
}

func TestMarketingUploadBatchExplicitZeroCoverWaitIsPreserved(t *testing.T) {
	request := uploadBatchFixtureRequest()
	request.CoverWait = 0
	request.CoverWaitSet = true
	normalized, _, err := normalizeUploadBatchRequest(
		request,
		time.Date(2026, 7, 26, 12, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.CoverWait != 0 {
		t.Fatalf("explicit zero cover wait became %s", normalized.CoverWait)
	}
	request.CoverWaitSet = false
	normalized, _, err = normalizeUploadBatchRequest(request, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if normalized.CoverWait != defaultCoverWait {
		t.Fatalf("unset cover wait default = %s, want %s", normalized.CoverWait, defaultCoverWait)
	}
}

func uploadBatchFixtureService(
	store *uploadJournalStoreStub,
	materials *uploadMaterialStub,
	executor TransactionExecutor,
) UploadBatchService {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	return UploadBatchService{
		Config:    staticMarketingConfig{value: marketingPrepareFixture(AccountUploadSource)},
		Materials: materials, RuntimeAssets: passthroughRuntimeAssets{}, Executor: executor,
		Journals: store, Catalog: store, ScopeLocker: store,
		Now:      func() time.Time { return time.Date(2026, 7, 26, 12, 0, 0, 0, location) },
		NewRunID: func(string, time.Time) (string, error) { return "marketing-upload-fixture", nil },
	}
}

func uploadBatchFixtureRequest() UploadBatchRequest {
	return UploadBatchRequest{
		ConfigPath: "/fixture/config.json", Accounts: []string{"1234567890"},
		PlanTemplate: "upload-template", Date: "today", VideosPerUnit: 5,
		AccountConcurrency: 2, GroupConcurrency: 2, CoverConcurrency: 4,
		CoverAttempts: 8, CoverWait: 2 * time.Second, PageSize: 100,
		AdGetBatchSize: 50, ValidateAdGet: true, SkipMissingCover: true,
		Submit: true, Channel: "marketing",
	}
}

type uploadJournalStoreStub struct {
	mu       sync.Mutex
	journals map[string]domainplans.Journal
	locks    []string
}

func newUploadJournalStoreStub() *uploadJournalStoreStub {
	return &uploadJournalStoreStub{journals: map[string]domainplans.Journal{}}
}

func (store *uploadJournalStoreStub) Load(_ context.Context, runID string) (domainplans.Journal, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	journal, exists := store.journals[runID]
	if !exists {
		return domainplans.Journal{}, os.ErrNotExist
	}
	return cloneJournal(journal), nil
}

func (store *uploadJournalStoreStub) Save(_ context.Context, runID string, journal domainplans.Journal) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.journals[runID] = cloneJournal(journal)
	return nil
}

func (store *uploadJournalStoreStub) List(_ context.Context, prefix string) ([]domainplans.JournalRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	result := []domainplans.JournalRecord{}
	for runID, journal := range store.journals {
		if strings.HasPrefix(runID, prefix) {
			result = append(result, domainplans.JournalRecord{RunID: runID, Journal: cloneJournal(journal)})
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left].RunID < result[right].RunID })
	return result, nil
}

func (store *uploadJournalStoreStub) AcquireScope(_ context.Context, scope string) (func() error, error) {
	store.mu.Lock()
	store.locks = append(store.locks, scope)
	store.mu.Unlock()
	return func() error { return nil }, nil
}

func (store *uploadJournalStoreStub) snapshot(runID string) (domainplans.Journal, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	journal, exists := store.journals[runID]
	return cloneJournal(journal), exists
}

type uploadMaterialStub struct {
	mu             sync.Mutex
	library        []domainmarketing.VideoAsset
	calls          []string
	seenVideoIDs   []string
	libraryFailure error
}

func newUploadMaterialStub(count int) *uploadMaterialStub {
	stub := &uploadMaterialStub{}
	for index := 1; index <= count; index++ {
		stub.addLibraryVideo("video-" + string(rune('0'+index)))
	}
	return stub
}

func (stub *uploadMaterialStub) QueryVideos(
	_ context.Context,
	query applicationmaterials.VideoQuery,
) (applicationmaterials.VideoResult, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	switch query.Mode {
	case "library-get":
		stub.calls = append(stub.calls, "library")
		if stub.libraryFailure != nil {
			err := stub.libraryFailure
			stub.libraryFailure = nil
			return applicationmaterials.VideoResult{}, err
		}
		rows := append([]domainmarketing.VideoAsset(nil), stub.library...)
		return applicationmaterials.VideoResult{
			Endpoint:   applicationmaterials.LibraryVideoEndpoint,
			Params:     map[string]any{"advertiser_id": query.AdvertiserID},
			RequestIDs: []string{"library-request"}, MatchedList: rows,
			MatchedCount: len(rows), PageInfo: &domainmarketing.PageInfo{
				Page: 1, PageSize: 100, TotalPages: 1, TotalNumber: len(rows),
			},
		}, nil
	case "ad-get":
		stub.calls = append(stub.calls, "ad")
		stub.seenVideoIDs = append(stub.seenVideoIDs, query.VideoIDs...)
		byID := map[string]domainmarketing.VideoAsset{}
		for _, row := range stub.library {
			byID[row.ID] = row
		}
		rows := make([]domainmarketing.VideoAsset, 0, len(query.VideoIDs))
		for _, videoID := range query.VideoIDs {
			if row, exists := byID[videoID]; exists {
				rows = append(rows, row)
			}
		}
		return applicationmaterials.VideoResult{
			RequestIDs: []string{"ad-request"}, MatchedList: rows, MatchedCount: len(rows),
		}, nil
	case "cover-suggest":
		stub.calls = append(stub.calls, "cover")
		stub.seenVideoIDs = append(stub.seenVideoIDs, query.VideoIDs...)
		return applicationmaterials.VideoResult{SelectedCoverID: "cover-" + query.VideoIDs[0]}, nil
	default:
		return applicationmaterials.VideoResult{}, errors.New("unexpected upload material query")
	}
}

func (stub *uploadMaterialStub) QueryCreator(
	context.Context,
	applicationmaterials.CreatorQuery,
) (applicationmaterials.CreatorResult, error) {
	return applicationmaterials.CreatorResult{}, errors.New("unexpected creator query")
}

func (stub *uploadMaterialStub) addLibraryVideo(videoID string) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.library = append(stub.library, domainmarketing.VideoAsset{
		ID: videoID, MaterialID: "material-" + videoID,
		Filename: videoID + ".mp4", CreateTime: "2026-07-26 08:00:00",
	})
}

func (stub *uploadMaterialStub) failNextLibrary(err error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.libraryFailure = err
}

func (stub *uploadMaterialStub) callCounts() map[string]int {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	result := map[string]int{}
	for _, call := range stub.calls {
		result[call]++
	}
	return result
}

func (stub *uploadMaterialStub) sawVideo(videoID string) bool {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	for _, seen := range stub.seenVideoIDs {
		if seen == videoID {
			return true
		}
	}
	return false
}

func flattenUploadResultVideoIDs(groups []UploadBatchGroup) []string {
	result := []string{}
	for _, group := range groups {
		result = append(result, uploadVideoIDs(group.Videos)...)
	}
	return result
}

type jsonRawMessageFixture map[string]struct{}

func (jsonRawMessageFixture) raw(metadata uploadJournalMetadata, scope string) map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"batch_kind":        json.RawMessage(`"marketing_upload"`),
		"scope_fingerprint": mustRawJSON(scope),
		"upload_batch":      mustRawJSON(metadata),
	}
}
