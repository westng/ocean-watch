package marketing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	sharedplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/plans"
	domainplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/plans"
)

type BatchKind string

const (
	BatchUpload  BatchKind = "upload"
	BatchCreator BatchKind = "creator"
)

type BatchJob struct {
	Key              string
	Kind             BatchKind
	AdvertiserID     string
	AuthAccountID    string
	ProjectPayload   json.RawMessage
	PromotionPayload json.RawMessage
	BlockedError     string
	JournalExtra     map[string]json.RawMessage
}

type BatchRequest struct {
	RunID          string
	Fingerprint    string
	Submit         bool
	MaxConcurrency int
	Jobs           []BatchJob
	JournalExtra   map[string]json.RawMessage
}

type BatchRow struct {
	Key            string                        `json:"job_key"`
	Kind           BatchKind                     `json:"kind"`
	AdvertiserID   string                        `json:"advertiser_id"`
	Status         string                        `json:"status"`
	PlannedAction  string                        `json:"planned_operation"`
	ProjectID      string                        `json:"project_id,omitempty"`
	PromotionID    string                        `json:"promotion_id,omitempty"`
	Result         *Result                       `json:"result,omitempty"`
	Error          string                        `json:"error,omitempty"`
	FailureStage   string                        `json:"failure_stage,omitempty"`
	LastResponse   *domainplans.OfficialResponse `json:"last_response,omitempty"`
	Reconciliation *domainplans.Reconciliation   `json:"reconciliation,omitempty"`
}

type BatchResult struct {
	Mode        string         `json:"mode"`
	RunID       string         `json:"run_id"`
	Fingerprint string         `json:"fingerprint"`
	Rows        []BatchRow     `json:"results"`
	Counts      map[string]int `json:"counts"`
	ExitCode    int            `json:"exit_code"`
}

type BatchRunner struct {
	Executor TransactionExecutor
	Journals sharedplans.JournalStore
	Now      func() time.Time
}

func (runner BatchRunner) Execute(ctx context.Context, request BatchRequest) (BatchResult, error) {
	if err := validateBatchRequest(request); err != nil {
		return BatchResult{}, err
	}
	journal, exists, err := runner.loadJournal(ctx, request)
	if err != nil {
		return BatchResult{}, err
	}
	if exists && journal.Fingerprint != request.Fingerprint {
		return BatchResult{}, errors.New("existing Marketing batch journal belongs to a different batch")
	}
	if !exists {
		journal, err = runner.newJournal(request)
		if err != nil {
			return BatchResult{}, err
		}
	}
	for _, job := range request.Jobs {
		if _, ok := journal.Jobs[job.Key]; !ok {
			return BatchResult{}, fmt.Errorf("Marketing batch journal is missing job %q", job.Key)
		}
	}

	result := BatchResult{
		Mode: "dry_run", RunID: request.RunID, Fingerprint: request.Fingerprint,
		Rows: make([]BatchRow, len(request.Jobs)), Counts: map[string]int{},
	}
	if request.Submit {
		result.Mode = "submit"
		if runner.Journals == nil {
			return BatchResult{}, errors.New("Marketing batch journal store is required for submit")
		}
		if !exists {
			if err := runner.Journals.Save(ctx, request.RunID, journal); err != nil {
				return BatchResult{}, fmt.Errorf("create Marketing batch journal: %w", err)
			}
		}
	}

	groups := groupBatchJobs(request.Jobs)
	limit := request.MaxConcurrency
	if limit == 0 {
		limit = 4
	}
	if limit > len(groups) {
		limit = len(groups)
	}
	var journalMu sync.Mutex
	var waitGroup sync.WaitGroup
	semaphore := make(chan struct{}, max(1, limit))
	for _, group := range groups {
		group := group
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			for _, indexed := range group {
				result.Rows[indexed.index] = runner.executeJob(
					ctx, request, indexed.job, &journal, &journalMu,
				)
			}
		}()
	}
	waitGroup.Wait()
	for _, row := range result.Rows {
		result.Counts[row.Status]++
		if batchRowFailed(row.Status) {
			result.ExitCode = 1
		}
	}
	return result, nil
}

type indexedBatchJob struct {
	index int
	job   BatchJob
}

func groupBatchJobs(jobs []BatchJob) [][]indexedBatchJob {
	order := make([]string, 0)
	groups := map[string][]indexedBatchJob{}
	for index, job := range jobs {
		if _, ok := groups[job.AdvertiserID]; !ok {
			order = append(order, job.AdvertiserID)
		}
		groups[job.AdvertiserID] = append(groups[job.AdvertiserID], indexedBatchJob{index: index, job: job})
	}
	result := make([][]indexedBatchJob, 0, len(order))
	for _, advertiserID := range order {
		result = append(result, groups[advertiserID])
	}
	return result
}

func (runner BatchRunner) executeJob(
	ctx context.Context,
	batch BatchRequest,
	job BatchJob,
	journal *domainplans.Journal,
	journalMu *sync.Mutex,
) BatchRow {
	journalMu.Lock()
	state := journal.Jobs[job.Key]
	journalMu.Unlock()
	action, recoverProject, recoverPromotion, resumeProjectID := batchAction(state)
	row := BatchRow{
		Key: job.Key, Kind: job.Kind, AdvertiserID: job.AdvertiserID,
		Status: "ready", PlannedAction: action, ProjectID: state.ProjectID,
		PromotionID: state.PromotionID, FailureStage: state.FailureStage,
		LastResponse: state.LastResponse, Reconciliation: state.Reconciliation,
	}
	if action == "skip_completed" {
		row.Status = "skipped_completed"
		return row
	}
	if action == "blocked_ambiguous" {
		row.Status = "ambiguous"
		row.Error = "automatic recovery stopped because official reconciliation is ambiguous"
		return row
	}
	if strings.TrimSpace(job.BlockedError) != "" {
		row.Status = "blocked"
		row.Error = job.BlockedError
		return row
	}
	request := Request{
		AdvertiserID: job.AdvertiserID, AuthAccountID: job.AuthAccountID,
		Submit: batch.Submit, ProjectPayload: job.ProjectPayload,
		PromotionPayload: job.PromotionPayload, ResumeProjectID: resumeProjectID,
		RecoverProject: recoverProject, RecoverPromotion: recoverPromotion,
	}
	if batch.Submit {
		request.Checkpoint = func(checkpointContext context.Context, checkpoint Checkpoint) error {
			journalMu.Lock()
			defer journalMu.Unlock()
			current := journal.Jobs[job.Key]
			applyCheckpoint(&current, checkpoint)
			journal.Jobs[job.Key] = current
			return runner.Journals.Save(checkpointContext, batch.RunID, *journal)
		}
	}
	executionResult, err := runner.Executor.Execute(ctx, request)
	row.Result = &executionResult
	row.ProjectID = executionResult.ProjectID
	row.PromotionID = executionResult.PromotionID
	row.Reconciliation = executionResult.Reconciliation
	row.FailureStage = executionResult.FailureStage
	row.LastResponse = executionResult.LastResponse
	if err != nil {
		row.Status = checkpointFailureStatus(executionResult)
		row.Error = err.Error()
		return row
	}
	if !batch.Submit {
		row.Status = "planned"
		return row
	}
	row.Status = "completed"
	return row
}

func (runner BatchRunner) loadJournal(
	ctx context.Context,
	request BatchRequest,
) (domainplans.Journal, bool, error) {
	if runner.Journals == nil {
		return domainplans.Journal{}, false, nil
	}
	journal, err := runner.Journals.Load(ctx, request.RunID)
	if errors.Is(err, os.ErrNotExist) {
		return domainplans.Journal{}, false, nil
	}
	if err != nil {
		return domainplans.Journal{}, false, fmt.Errorf("load Marketing batch journal: %w", err)
	}
	return journal, true, nil
}

func (runner BatchRunner) newJournal(request BatchRequest) (domainplans.Journal, error) {
	jobs := make(map[string]domainplans.JournalJob, len(request.Jobs))
	for _, job := range request.Jobs {
		jobs[job.Key] = domainplans.JournalJob{
			Status: "pending", AdvertiserID: job.AdvertiserID,
			Extra: cloneRawMessages(job.JournalExtra),
		}
	}
	now := time.Now().UTC()
	if runner.Now != nil {
		now = runner.Now().UTC()
	}
	journal, err := domainplans.NewJournal(request.Fingerprint, jobs, now)
	if err != nil {
		return domainplans.Journal{}, err
	}
	journal.Extra = cloneRawMessages(request.JournalExtra)
	return journal, nil
}

func validateBatchRequest(request BatchRequest) error {
	if err := domainplans.ValidateJournalID(request.RunID); err != nil {
		return err
	}
	if strings.TrimSpace(request.Fingerprint) == "" || len(request.Fingerprint) > 256 {
		return errors.New("Marketing batch fingerprint is required")
	}
	if len(request.Jobs) == 0 {
		return errors.New("Marketing batch requires at least one job")
	}
	if request.MaxConcurrency < 0 || request.MaxConcurrency > 10 {
		return errors.New("Marketing batch concurrency must be between 1 and 10")
	}
	keys := make([]string, 0, len(request.Jobs))
	for _, job := range request.Jobs {
		if job.Kind != BatchUpload && job.Kind != BatchCreator {
			return errors.New("Marketing batch job kind must be upload or creator")
		}
		if strings.TrimSpace(job.Key) == "" {
			return errors.New("Marketing batch job key is required")
		}
		if !validPositiveID(strings.TrimSpace(job.AdvertiserID)) {
			return errors.New("Marketing batch advertiser_id must be a positive decimal ID")
		}
		keys = append(keys, job.Key)
	}
	sort.Strings(keys)
	for index := 1; index < len(keys); index++ {
		if keys[index] == keys[index-1] {
			return errors.New("Marketing batch job keys must be unique")
		}
	}
	return nil
}

func batchAction(job domainplans.JournalJob) (string, bool, bool, string) {
	switch job.Status {
	case "completed":
		if validPositiveID(job.PromotionID) {
			return "skip_completed", false, false, job.ProjectID
		}
	case "ambiguous":
		return "blocked_ambiguous", false, false, job.ProjectID
	case "project_dispatching":
		return "reconcile_project", true, false, ""
	case "promotion_dispatching":
		if validPositiveID(job.ProjectID) {
			return "reconcile_promotion", false, true, job.ProjectID
		}
	case "project_created":
		if validPositiveID(job.ProjectID) {
			return "resume_promotion", false, false, job.ProjectID
		}
	case "promotion_failed", "promotion_retrying":
		if validPositiveID(job.ProjectID) {
			if job.DispatchState == domainplans.DispatchUnknown {
				return "reconcile_promotion", false, true, job.ProjectID
			}
			return "resume_promotion", false, false, job.ProjectID
		}
	case "project_failed":
		if job.DispatchState == domainplans.DispatchUnknown {
			return "reconcile_project", true, false, ""
		}
		return "retry_project_and_promotion", false, false, ""
	}
	return "create_project_and_promotion", false, false, ""
}

func applyCheckpoint(job *domainplans.JournalJob, checkpoint Checkpoint) {
	job.Status = checkpoint.Status
	if checkpoint.ProjectID != "" {
		job.ProjectID = checkpoint.ProjectID
	}
	if checkpoint.PromotionID != "" {
		job.PromotionID = checkpoint.PromotionID
	}
	job.Reconciliation = checkpoint.Reconciliation
	job.RequestID = checkpoint.RequestID
	job.DispatchState = checkpoint.DispatchState
	job.FailureStage = checkpoint.FailureStage
	job.LastResponse = checkpoint.LastResponse
}

func batchRowFailed(status string) bool {
	switch status {
	case "completed", "planned", "skipped_completed":
		return false
	default:
		return true
	}
}

func cloneRawMessages(source map[string]json.RawMessage) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage, len(source))
	for key, value := range source {
		result[key] = append(json.RawMessage(nil), value...)
	}
	return result
}
