package marketing

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	portmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/marketing"
)

func TestMarketingBatchResumesPromotionAndSkipsCompleted(t *testing.T) {
	store := &memoryJournalStore{}
	firstWriter := &batchWriter{
		projectIDs: []string{"2001", "2002"},
		promotionResults: map[string][]batchPromotionResult{
			"2001": {{err: acknowledgedError("promotion rejected")}},
			"2002": {{id: "3002"}},
		},
	}
	request := fixtureBatchRequest()
	runner := BatchRunner{Executor: fixtureExecutor(firstWriter, nil), Journals: store}
	first, err := runner.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.ExitCode != 1 || first.Counts["promotion_failed"] != 1 || first.Counts["completed"] != 1 {
		t.Fatalf("partial batch result changed: %#v", first)
	}
	journal := store.snapshot()
	if journal.Jobs["upload:one"].ProjectID != "2001" || journal.Jobs["upload:one"].Status != "promotion_failed" {
		t.Fatalf("failed promotion was not resumable: %#v", journal.Jobs["upload:one"])
	}
	if string(journal.Jobs["upload:one"].Extra["python_extension"]) != `{"kept":true}` {
		t.Fatalf("journal extension was lost: %#v", journal.Jobs["upload:one"].Extra)
	}

	secondWriter := &batchWriter{
		promotionResults: map[string][]batchPromotionResult{"2001": {{id: "3001"}}},
	}
	runner.Executor = fixtureExecutor(secondWriter, nil)
	second, err := runner.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if second.ExitCode != 0 || second.Counts["completed"] != 1 || second.Counts["skipped_completed"] != 1 {
		t.Fatalf("resume result changed: %#v", second)
	}
	if !reflect.DeepEqual(secondWriter.calls, []string{"promotion:2001"}) {
		t.Fatalf("resume recreated a project or completed job: %#v", secondWriter.calls)
	}
}

func TestMarketingBatchRecoversDispatchWindowsBeforeWriting(t *testing.T) {
	request := fixtureBatchRequest()
	request.Jobs = []BatchJob{
		request.Jobs[0],
		{
			Key: "creator:two", Kind: BatchCreator, AdvertiserID: "1001",
			ProjectPayload:   fixtureProjectPayload("project-two"),
			PromotionPayload: fixturePromotionPayload("promotion-two"),
		},
	}
	journal, err := domainplans.NewJournal(request.Fingerprint, map[string]domainplans.JournalJob{
		"upload:one": {Status: "project_dispatching", AdvertiserID: "1001"},
		"creator:two": {
			Status: "promotion_dispatching", AdvertiserID: "1001", ProjectID: "2002",
		},
	}, time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryJournalStore{journal: journal, exists: true}
	writer := &batchWriter{
		promotionResults: map[string][]batchPromotionResult{"2001": {{id: "3001"}}},
	}
	reconciler := &batchReconciler{
		projectsByName:   map[string][]string{"project-one": {"2001"}},
		promotionsByName: map[string][]string{"promotion-two": {"3002"}},
	}
	result, err := (BatchRunner{
		Executor: fixtureExecutor(writer, reconciler), Journals: store,
	}).Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.Counts["completed"] != 2 {
		t.Fatalf("dispatch recovery failed: %#v", result)
	}
	if !reflect.DeepEqual(writer.calls, []string{"promotion:2001"}) {
		t.Fatalf("recovery replayed a dispatched write: %#v", writer.calls)
	}
	if !reflect.DeepEqual(reconciler.calls, []string{"project:project-one", "promotion:2002:promotion-two"}) {
		t.Fatalf("recovery reconciliation changed: %#v", reconciler.calls)
	}
}

func TestMarketingBatchAmbiguousRecoveryStopsAllLaterWritesForJob(t *testing.T) {
	request := fixtureBatchRequest()
	request.Jobs = request.Jobs[:1]
	journal, err := domainplans.NewJournal(request.Fingerprint, map[string]domainplans.JournalJob{
		"upload:one": {Status: "project_dispatching", AdvertiserID: "1001"},
	}, time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryJournalStore{journal: journal, exists: true}
	writer := &batchWriter{}
	reconciler := &batchReconciler{
		projectsByName: map[string][]string{"project-one": {"2001", "2002"}},
	}
	runner := BatchRunner{Executor: fixtureExecutor(writer, reconciler), Journals: store}
	first, err := runner.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.ExitCode != 1 || first.Rows[0].Status != "ambiguous" || len(writer.calls) != 0 {
		t.Fatalf("ambiguous recovery did not stop: result=%#v calls=%#v", first, writer.calls)
	}
	second, err := runner.Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if second.Rows[0].PlannedAction != "blocked_ambiguous" || len(reconciler.calls) != 1 || len(writer.calls) != 0 {
		t.Fatalf("ambiguous journal was automatically retried: result=%#v reconciler=%#v", second, reconciler.calls)
	}
}

func TestMarketingBatchPersistsOfficialFailureDiagnostics(t *testing.T) {
	request := fixtureBatchRequest()
	request.Jobs = request.Jobs[:1]
	code := int64(40000)
	writer := &batchWriter{
		projectIDs: []string{"2001"},
		promotionResults: map[string][]batchPromotionResult{
			"2001": {{err: &domainplans.DispatchFailure{
				State: domainplans.DispatchAcknowledged,
				Cause: officialResponseError{summary: domainplans.OfficialResponse{
					Code: &code, Message: "视频不在授权期间", RequestID: "promotion-request-1",
				}},
			}}},
		},
	}
	store := &memoryJournalStore{}
	result, err := (BatchRunner{
		Executor: fixtureExecutor(writer, nil), Journals: store,
	}).Execute(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	row := result.Rows[0]
	if row.Status != "promotion_failed" || row.FailureStage != "promotion_create" ||
		row.LastResponse == nil || row.LastResponse.Code == nil || *row.LastResponse.Code != code ||
		row.LastResponse.Message != "视频不在授权期间" || row.LastResponse.RequestID != "promotion-request-1" {
		t.Fatalf("batch row lost official failure diagnostics: %#v", row)
	}
	job := store.snapshot().Jobs[request.Jobs[0].Key]
	if job.FailureStage != "promotion_create" || job.LastResponse == nil ||
		job.LastResponse.Code == nil || *job.LastResponse.Code != code ||
		job.LastResponse.Message != "视频不在授权期间" || job.LastResponse.RequestID != "promotion-request-1" {
		t.Fatalf("journal lost official failure diagnostics: %#v", job)
	}
}

func fixtureBatchRequest() BatchRequest {
	return BatchRequest{
		RunID: "marketing-batch-20260726", Fingerprint: "fixture-fingerprint",
		Submit: true, MaxConcurrency: 4,
		Jobs: []BatchJob{
			{
				Key: "upload:one", Kind: BatchUpload, AdvertiserID: "1001",
				ProjectPayload:   fixtureProjectPayload("project-one"),
				PromotionPayload: fixturePromotionPayload("promotion-one"),
				JournalExtra: map[string]json.RawMessage{
					"python_extension": json.RawMessage(`{"kept":true}`),
				},
			},
			{
				Key: "creator:two", Kind: BatchCreator, AdvertiserID: "1001",
				ProjectPayload:   fixtureProjectPayload("project-two"),
				PromotionPayload: fixturePromotionPayload("promotion-two"),
			},
		},
	}
}

func fixtureProjectPayload(name string) json.RawMessage {
	return mustJSON(map[string]any{"advertiser_id": 1001, "name": name})
}

func fixturePromotionPayload(name string) json.RawMessage {
	return mustJSON(map[string]any{
		"advertiser_id": 1001, "name": name, "project_id": "{{project_id}}",
	})
}

type memoryJournalStore struct {
	mu      sync.Mutex
	journal domainplans.Journal
	exists  bool
}

func (store *memoryJournalStore) Load(context.Context, string) (domainplans.Journal, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.exists {
		return domainplans.Journal{}, os.ErrNotExist
	}
	return cloneJournal(store.journal), nil
}

func (store *memoryJournalStore) Save(_ context.Context, _ string, journal domainplans.Journal) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.journal = cloneJournal(journal)
	store.exists = true
	return nil
}

func (store *memoryJournalStore) snapshot() domainplans.Journal {
	store.mu.Lock()
	defer store.mu.Unlock()
	return cloneJournal(store.journal)
}

func cloneJournal(journal domainplans.Journal) domainplans.Journal {
	payload, err := json.Marshal(journal)
	if err != nil {
		panic(err)
	}
	var cloned domainplans.Journal
	if err := json.Unmarshal(payload, &cloned); err != nil {
		panic(err)
	}
	return cloned
}

type batchPromotionResult struct {
	id  string
	err error
}

type officialResponseError struct {
	summary domainplans.OfficialResponse
}

func (err officialResponseError) Error() string {
	return err.summary.Message
}

func (err officialResponseError) OfficialResponseSummary() domainplans.OfficialResponse {
	return err.summary
}

type batchWriter struct {
	mu               sync.Mutex
	projectIDs       []string
	promotionResults map[string][]batchPromotionResult
	calls            []string
}

func (writer *batchWriter) CreateProject(context.Context, portmarketing.ProjectCreateRequest) (portmarketing.CreateResult, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.calls = append(writer.calls, "project")
	if len(writer.projectIDs) == 0 {
		return portmarketing.CreateResult{}, errors.New("unexpected project write")
	}
	projectID := writer.projectIDs[0]
	writer.projectIDs = writer.projectIDs[1:]
	return portmarketing.CreateResult{ObjectID: projectID}, nil
}

func (writer *batchWriter) CreatePromotion(_ context.Context, request portmarketing.PromotionCreateRequest) (portmarketing.CreateResult, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.calls = append(writer.calls, "promotion:"+request.ProjectID)
	results := writer.promotionResults[request.ProjectID]
	if len(results) == 0 {
		return portmarketing.CreateResult{}, errors.New("unexpected promotion write")
	}
	result := results[0]
	writer.promotionResults[request.ProjectID] = results[1:]
	return portmarketing.CreateResult{ObjectID: result.id}, result.err
}

type batchReconciler struct {
	mu               sync.Mutex
	projectsByName   map[string][]string
	promotionsByName map[string][]string
	calls            []string
}

func (reconciler *batchReconciler) FindProjects(_ context.Context, request portmarketing.ProjectReconciliationRequest) ([]string, error) {
	reconciler.mu.Lock()
	defer reconciler.mu.Unlock()
	reconciler.calls = append(reconciler.calls, "project:"+request.Name)
	return append([]string(nil), reconciler.projectsByName[request.Name]...), nil
}

func (reconciler *batchReconciler) FindPromotions(_ context.Context, request portmarketing.PromotionReconciliationRequest) ([]string, error) {
	reconciler.mu.Lock()
	defer reconciler.mu.Unlock()
	reconciler.calls = append(reconciler.calls, "promotion:"+request.ProjectID+":"+request.Name)
	return append([]string(nil), reconciler.promotionsByName[request.Name]...), nil
}
