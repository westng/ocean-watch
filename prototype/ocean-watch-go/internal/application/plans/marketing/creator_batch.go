package marketing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	sharedplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/plans"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/configuration"
	domainplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/plans"
)

const (
	CreatorBatchSchemaVersion  = 1
	CreatorBatchConcurrency    = 4
	CreatorBatchMaxConcurrency = 10
)

type BatchInputError struct {
	Code    string
	Message string
}

func (err *BatchInputError) Error() string { return err.Message }

type ProductMatch struct {
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
}

type CreatorManifestJob struct {
	Index          int
	AdvertiserID   string
	PlanTemplate   string
	AwemeID        string
	ItemIDs        []string
	ProductMatch   ProductMatch
	Budget         any
	CPABid         any
	ROIGoal        any
	MaterialDate   string
	ProductName    any
	ProductID      any
	ProjectName    string
	PromotionName  string
	budgetSet      bool
	cpaBidSet      bool
	roiGoalSet     bool
	productNameSet bool
	productIDSet   bool
}

type CreatorBatchManifest struct {
	SchemaVersion int
	Channel       string
	Jobs          []CreatorManifestJob
}

type CreatorBatchRequest struct {
	ManifestPayload []byte
	Channel         string
	AuthAccountID   string
	RunID           string
	Preflight       bool
	Submit          bool
	IncludePayloads bool
	MaxConcurrency  int
}

type CreatorBatchRow struct {
	JobKey                 string                        `json:"job_key"`
	AdvertiserID           string                        `json:"advertiser_id"`
	PlanTemplate           string                        `json:"plan_template"`
	AwemeID                string                        `json:"aweme_id"`
	CreatorName            string                        `json:"creator_name,omitempty"`
	ItemIDs                []string                      `json:"item_ids"`
	ProductMatch           ProductMatch                  `json:"product_match"`
	Status                 string                        `json:"status"`
	ExitCode               int                           `json:"exit_code"`
	PlannedOperation       string                        `json:"planned_operation"`
	PreviousStatus         string                        `json:"previous_status,omitempty"`
	ProjectName            string                        `json:"project_name,omitempty"`
	PromotionName          string                        `json:"promotion_name,omitempty"`
	ProjectID              string                        `json:"project_id,omitempty"`
	PromotionID            string                        `json:"promotion_id,omitempty"`
	MissingFields          *[]string                     `json:"missing_fields,omitempty"`
	ErrorCode              string                        `json:"error_code,omitempty"`
	Error                  string                        `json:"error,omitempty"`
	FailureStage           string                        `json:"failure_stage,omitempty"`
	CreatorCoverResolution map[string]any                `json:"creator_cover_resolution,omitempty"`
	LastResponse           *domainplans.OfficialResponse `json:"last_response,omitempty"`
	ProjectPayload         map[string]any                `json:"project_payload,omitempty"`
	PromotionPayload       map[string]any                `json:"promotion_payload,omitempty"`
}

type CreatorBatchPreflight struct {
	TotalJobs            int            `json:"total_jobs"`
	ReadyToSubmit        int            `json:"ready_to_submit"`
	AlreadyCompleted     int            `json:"already_completed"`
	Blocked              int            `json:"blocked"`
	PlannedOperations    map[string]int `json:"planned_operations"`
	ConfirmationRequired bool           `json:"confirmation_required"`
	CreatorAuthorization map[string]any `json:"creator_authorization"`
	ProjectCapacity      map[string]any `json:"project_capacity"`
}

type CreatorBatchResult struct {
	Mode        string                `json:"mode"`
	BatchID     string                `json:"batch_id"`
	Journal     *string               `json:"journal"`
	RunID       string                `json:"-"`
	JournalUsed bool                  `json:"-"`
	Counts      map[string]int        `json:"counts"`
	Rows        []CreatorBatchRow     `json:"results"`
	Preflight   CreatorBatchPreflight `json:"preflight"`
	ExitCode    int                   `json:"-"`
}

type CreatorBatchService struct {
	Preparer Preparer
	Executor TransactionExecutor
	Journals sharedplans.JournalStore
	Now      func() time.Time
}

type creatorBatchWork struct {
	manifest         CreatorManifestJob
	key              string
	prepared         PreparedPlan
	state            domainplans.JournalJob
	blocked          error
	currentValidated bool
}

func ParseCreatorBatchManifest(payload []byte, channelOverride string) (CreatorBatchManifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return CreatorBatchManifest{}, batchInput("invalid_batch_manifest", "jobs file must contain a JSON object")
	}
	if raw == nil {
		return CreatorBatchManifest{}, batchInput("invalid_batch_manifest", "jobs file must contain a JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return CreatorBatchManifest{}, batchInput("invalid_batch_manifest", "jobs file contains trailing JSON")
	}
	schemaVersion, err := manifestSchemaVersion(raw["schema_version"])
	if err != nil || schemaVersion != CreatorBatchSchemaVersion {
		return CreatorBatchManifest{}, batchInput("invalid_batch_manifest", "unsupported creator batch schema version")
	}
	channel := strings.TrimSpace(channelOverride)
	if channel == "" {
		channel = textOrEmpty(raw["channel"])
	}
	if channel == "" {
		channel = "marketing"
	}
	if channel != "marketing" {
		return CreatorBatchManifest{}, batchInput("invalid_batch_manifest", "creator Marketing batches only support channel marketing")
	}
	rows, ok := raw["jobs"].([]any)
	if !ok || len(rows) == 0 {
		return CreatorBatchManifest{}, batchInput("invalid_batch_manifest", "jobs file must contain at least one job")
	}
	manifest := CreatorBatchManifest{SchemaVersion: schemaVersion, Channel: channel, Jobs: make([]CreatorManifestJob, 0, len(rows))}
	keys := map[string]struct{}{}
	for index, value := range rows {
		row, ok := value.(map[string]any)
		if !ok {
			return CreatorBatchManifest{}, batchInput("invalid_batch_job", fmt.Sprintf("jobs[%d] must be an object", index))
		}
		job, err := normalizeCreatorManifestJob(row, raw, index)
		if err != nil {
			return CreatorBatchManifest{}, err
		}
		key := CreatorBatchJobKey(channel, job)
		if _, exists := keys[key]; exists {
			return CreatorBatchManifest{}, batchInput("duplicate_batch_job", "creator batch contains duplicate jobs")
		}
		keys[key] = struct{}{}
		manifest.Jobs = append(manifest.Jobs, job)
	}
	return manifest, nil
}

func (service CreatorBatchService) Execute(ctx context.Context, request CreatorBatchRequest) (CreatorBatchResult, error) {
	if ctx == nil {
		return CreatorBatchResult{}, errors.New("creator batch context is required")
	}
	if request.Preflight && request.Submit {
		return CreatorBatchResult{}, batchInput("invalid_batch_mode", "--preflight and --submit cannot be used together")
	}
	if request.MaxConcurrency == 0 {
		request.MaxConcurrency = CreatorBatchConcurrency
	}
	if request.MaxConcurrency < 1 || request.MaxConcurrency > CreatorBatchMaxConcurrency {
		return CreatorBatchResult{}, batchInput("invalid_concurrency", "concurrency must be between 1 and 10")
	}
	manifest, err := ParseCreatorBatchManifest(request.ManifestPayload, request.Channel)
	if err != nil {
		return CreatorBatchResult{}, err
	}
	if service.Preparer.Config == nil {
		return CreatorBatchResult{}, errors.New("creator batch config reader is required")
	}
	rawConfig, err := service.Preparer.Config.Read(ctx)
	if err != nil {
		return CreatorBatchResult{}, fmt.Errorf("read creator batch config: %w", err)
	}
	preparer := service.Preparer
	preparer.Config = batchConfigSnapshot{value: rawConfig}
	now := time.Now()
	if service.Now != nil {
		now = service.Now()
	} else if preparer.Now != nil {
		now = preparer.Now()
	}
	works := make([]creatorBatchWork, len(manifest.Jobs))
	for index, job := range manifest.Jobs {
		prepared, prepareErr := preparer.Prepare(ctx, creatorPrepareRequest(job, request, now, false, ""))
		if prepareErr != nil {
			return CreatorBatchResult{}, batchInput("invalid_batch_job", fmt.Sprintf("jobs[%d] is invalid: %v", index, prepareErr))
		}
		job.MaterialDate = prepared.MaterialDate
		if job.ProjectName == "" {
			job.ProjectName = textOrEmpty(prepared.ProjectPayload["name"])
		}
		if job.PromotionName == "" {
			job.PromotionName = textOrEmpty(prepared.PromotionPayload["name"])
		}
		manifest.Jobs[index] = job
		works[index] = creatorBatchWork{
			manifest: job, key: CreatorBatchJobKey(manifest.Channel, job), prepared: prepared,
		}
	}
	fingerprint, err := CreatorBatchFingerprint(manifest)
	if err != nil {
		return CreatorBatchResult{}, err
	}
	runID := strings.TrimSpace(request.RunID)
	if runID == "" {
		runID = "creator-batch-" + fingerprint[:16]
	}
	if err := domainplans.ValidateJournalID(runID); err != nil {
		return CreatorBatchResult{}, batchInput("invalid_batch_journal", err.Error())
	}

	journal, journalExists, err := service.loadCreatorJournal(ctx, request, runID, fingerprint)
	if err != nil {
		return CreatorBatchResult{}, err
	}
	if journalExists {
		for index := range works {
			state, exists := journal.Jobs[works[index].key]
			if !exists {
				return CreatorBatchResult{}, batchInput("batch_journal_mismatch", fmt.Sprintf("existing journal is missing job %q", works[index].key))
			}
			works[index].state = state
		}
	}
	service.prepareCurrentCreatorJobs(ctx, preparer, request, now, works)

	jobs := make([]BatchJob, len(works))
	for index, work := range works {
		jobs[index] = BatchJob{
			Key: work.key, Kind: BatchCreator, AdvertiserID: work.manifest.AdvertiserID,
			AuthAccountID: request.AuthAccountID, ProjectPayload: work.prepared.ProjectJSON,
			PromotionPayload: work.prepared.PromotionJSON, JournalExtra: creatorJournalExtra(work.manifest),
		}
		if work.blocked != nil {
			jobs[index].BlockedError = work.blocked.Error()
		}
	}
	executor := service.Executor
	if executor == nil && !request.Submit {
		executor = Executor{}
	}
	if request.Submit && executor == nil && creatorBatchHasReadyWork(works) {
		return CreatorBatchResult{}, errors.New("Marketing transaction executor is required for creator batch submit")
	}
	runner := BatchRunner{Executor: executor, Now: service.Now}
	if request.Preflight || request.Submit {
		runner.Journals = service.Journals
	}
	previewBatch, err := runner.Execute(ctx, BatchRequest{
		RunID: runID, Fingerprint: fingerprint, Submit: false,
		MaxConcurrency: request.MaxConcurrency, Jobs: jobs,
	})
	if err != nil {
		return CreatorBatchResult{}, err
	}
	preflightRows := service.creatorBatchRows(request, works, previewBatch)
	preflight := creatorPreflightSummary(preflightRows, !request.Submit)
	batch := previewBatch
	if request.Submit {
		batch, err = runner.Execute(ctx, BatchRequest{
			RunID: runID, Fingerprint: fingerprint, Submit: request.Submit,
			MaxConcurrency: request.MaxConcurrency, Jobs: jobs,
		})
		if err != nil {
			return CreatorBatchResult{}, err
		}
	}
	result := service.creatorBatchResult(request, runID, fingerprint, journalExists, works, batch, preflight)
	return result, nil
}

func creatorBatchHasReadyWork(works []creatorBatchWork) bool {
	for _, work := range works {
		action, _, _, _ := batchAction(work.state)
		if action != "skip_completed" && action != "blocked_ambiguous" && work.blocked == nil {
			return true
		}
	}
	return false
}

func (service CreatorBatchService) loadCreatorJournal(
	ctx context.Context,
	request CreatorBatchRequest,
	runID string,
	fingerprint string,
) (domainplans.Journal, bool, error) {
	if !request.Preflight && !request.Submit {
		return domainplans.Journal{}, false, nil
	}
	if service.Journals == nil {
		if request.Submit {
			return domainplans.Journal{}, false, errors.New("creator batch journal store is required for submit")
		}
		return domainplans.Journal{}, false, nil
	}
	journal, err := service.Journals.Load(ctx, runID)
	if errors.Is(err, os.ErrNotExist) {
		return domainplans.Journal{}, false, nil
	}
	if err != nil {
		return domainplans.Journal{}, false, fmt.Errorf("load creator batch journal: %w", err)
	}
	if journal.Fingerprint != fingerprint {
		return domainplans.Journal{}, false, batchInput("batch_journal_mismatch", "existing journal belongs to a different creator batch")
	}
	return journal, true, nil
}

func (service CreatorBatchService) prepareCurrentCreatorJobs(
	ctx context.Context,
	preparer Preparer,
	request CreatorBatchRequest,
	now time.Time,
	works []creatorBatchWork,
) {
	semaphore := make(chan struct{}, request.MaxConcurrency)
	var waitGroup sync.WaitGroup
	for index := range works {
		if action, _, _, _ := batchAction(works[index].state); action == "skip_completed" || action == "blocked_ambiguous" {
			continue
		}
		index := index
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			projectID := ""
			if validPositiveID(works[index].state.ProjectID) {
				projectID = works[index].state.ProjectID
			}
			prepared, err := preparer.Prepare(
				ctx, creatorPrepareRequest(works[index].manifest, request, now, true, projectID),
			)
			if err != nil {
				works[index].blocked = err
				return
			}
			works[index].prepared = prepared
			works[index].currentValidated = true
			if previousAuthorizationPeriodFailure(works[index].state) && historicalCreatorCoverUsed(prepared.CreatorCoverResolution) {
				works[index].blocked = batchInput(
					"creator_reauthorization_required",
					"the official create API previously rejected this work as outside its authorization period; reauthorize it before retrying the promotion",
				)
				return
			}
			if len(prepared.MissingFields) != 0 {
				works[index].blocked = fmt.Errorf("blocking fields: %s", strings.Join(prepared.MissingFields, ", "))
			}
		}()
	}
	waitGroup.Wait()
}

func (service CreatorBatchService) creatorBatchResult(
	request CreatorBatchRequest,
	runID string,
	fingerprint string,
	journalExists bool,
	works []creatorBatchWork,
	batch BatchResult,
	preflight CreatorBatchPreflight,
) CreatorBatchResult {
	mode := "dry_run"
	if request.Preflight {
		mode = "preflight"
	}
	if request.Submit {
		mode = "submit"
	}
	result := CreatorBatchResult{
		Mode: mode, BatchID: fingerprint[:16],
		Counts: map[string]int{}, Rows: make([]CreatorBatchRow, len(works)),
	}
	if request.Submit || request.Preflight && journalExists {
		result.RunID = runID
		result.JournalUsed = true
	}
	result.Rows = service.creatorBatchRows(request, works, batch)
	for _, row := range result.Rows {
		result.Counts[row.Status]++
	}
	result.Preflight = preflight
	if request.Submit {
		result.ExitCode = batch.ExitCode
	} else if result.Preflight.Blocked != 0 {
		result.ExitCode = 2
	}
	return result
}

func (service CreatorBatchService) creatorBatchRows(
	request CreatorBatchRequest,
	works []creatorBatchWork,
	batch BatchResult,
) []CreatorBatchRow {
	rows := make([]CreatorBatchRow, len(works))
	for index, work := range works {
		batchRow := batch.Rows[index]
		status := batchRow.Status
		if status == "planned" {
			status = "ready"
		}
		if request.Submit && status == "completed" {
			status = "created"
		}
		row := CreatorBatchRow{
			JobKey: work.key, AdvertiserID: work.manifest.AdvertiserID,
			PlanTemplate: work.manifest.PlanTemplate, AwemeID: work.manifest.AwemeID,
			ItemIDs: append([]string(nil), work.manifest.ItemIDs...), ProductMatch: work.manifest.ProductMatch,
			Status: status, PlannedOperation: batchRow.PlannedAction,
			PreviousStatus: work.state.Status, ProjectID: batchRow.ProjectID,
			PromotionID:   batchRow.PromotionID,
			FailureStage:  batchRow.FailureStage,
			LastResponse:  batchRow.LastResponse,
			ProjectName:   textOrEmpty(work.prepared.ProjectPayload["name"]),
			PromotionName: textOrEmpty(work.prepared.PromotionPayload["name"]),
		}
		if work.prepared.CreatorCoverResolution != nil {
			row.CreatorCoverResolution = configuration.CloneMap(work.prepared.CreatorCoverResolution)
		}
		if work.currentValidated {
			missing := append([]string{}, work.prepared.MissingFields...)
			row.MissingFields = &missing
		}
		if work.prepared.SelectedCreator != nil {
			row.CreatorName = work.prepared.SelectedCreator.AwemeName
		}
		if batchRow.Error != "" {
			row.Error = batchRow.Error
			var inputErr *BatchInputError
			if errors.As(work.blocked, &inputErr) {
				row.ErrorCode = inputErr.Code
			} else if status == "blocked" {
				row.ErrorCode = "creator_preflight_failed"
			} else {
				row.ErrorCode = "creator_submit_failed"
			}
		}
		if request.IncludePayloads {
			row.ProjectPayload = configuration.CloneMap(work.prepared.ProjectPayload)
			row.PromotionPayload = configuration.CloneMap(work.prepared.PromotionPayload)
		}
		if status != "ready" && status != "created" && status != "skipped_completed" {
			row.ExitCode = 1
			if !request.Submit {
				row.ExitCode = 2
			}
		}
		rows[index] = row
	}
	return rows
}

func creatorPreflightSummary(rows []CreatorBatchRow, confirmationRequired bool) CreatorBatchPreflight {
	operations := map[string]int{}
	advertisers := map[string]struct{}{}
	result := CreatorBatchPreflight{
		TotalJobs: len(rows), PlannedOperations: operations,
		CreatorAuthorization: map[string]any{
			"status":  "SNAPSHOT_VALIDATED",
			"message": "Current authorization snapshots supplied the creator materials used by ready jobs.",
		},
	}
	historicalCoverJobs := 0
	for _, row := range rows {
		operations[row.PlannedOperation]++
		advertisers[row.AdvertiserID] = struct{}{}
		if historicalCreatorCoverUsed(row.CreatorCoverResolution) {
			historicalCoverJobs++
		}
		switch row.Status {
		case "ready":
			result.ReadyToSubmit++
		case "skipped_completed":
			result.AlreadyCompleted++
		case "blocked", "ambiguous":
			result.Blocked++
		}
	}
	if historicalCoverJobs != 0 {
		result.CreatorAuthorization = map[string]any{
			"status": "CREATE_TIME_ONLY", "historical_cover_jobs": historicalCoverJobs,
			"message": "A historical official cover does not prove that the work remains within its authorization period; promotion creation performs the final check.",
		}
	} else {
		result.CreatorAuthorization["historical_cover_jobs"] = 0
	}
	advertiserIDs := make([]string, 0, len(advertisers))
	for advertiserID := range advertisers {
		advertiserIDs = append(advertiserIDs, advertiserID)
	}
	sort.Strings(advertiserIDs)
	result.ConfirmationRequired = confirmationRequired && result.ReadyToSubmit != 0
	result.ProjectCapacity = map[string]any{
		"status": "CREATE_TIME_ONLY", "known_limit_per_advertiser": 200,
		"advertiser_ids": advertiserIDs, "endpoint": ProjectCreateEndpoint,
		"message": "The project-create endpoint performs the final capacity check.",
	}
	return result
}

func historicalCreatorCoverUsed(resolution map[string]any) bool {
	return textOrEmpty(resolution["source"]) == "matching_official_promotion"
}

func previousAuthorizationPeriodFailure(state domainplans.JournalJob) bool {
	if state.LastResponse == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(state.LastResponse.Message))
	for _, marker := range []string{"不在授权期间", "not in authorization period"} {
		if strings.Contains(message, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func creatorPrepareRequest(
	job CreatorManifestJob,
	request CreatorBatchRequest,
	now time.Time,
	online bool,
	projectID string,
) PrepareRequest {
	return PrepareRequest{
		Kind: PrepareCreator, AdvertiserID: job.AdvertiserID,
		AuthAccountID: strings.TrimSpace(request.AuthAccountID), PlanTemplate: job.PlanTemplate,
		OnlinePreflight: online, ItemIDs: append([]string(nil), job.ItemIDs...),
		ExpectedAwemeID: job.AwemeID, AppendCreatorID: true,
		Budget: job.Budget, CPABid: job.CPABid, ROIGoal: job.ROIGoal,
		MaterialDate: job.MaterialDate, ProductName: textOrEmpty(job.ProductName),
		ProductID: textOrEmpty(job.ProductID), ProjectName: job.ProjectName,
		PromotionName: job.PromotionName, ProjectID: projectID,
		GroupIndex: job.Index + 1, Index: job.Index + 1,
		Suffix: fmt.Sprintf("%02d", job.Index+1), Now: now,
	}
}

func normalizeCreatorManifestJob(row, manifest map[string]any, index int) (CreatorManifestJob, error) {
	advertiserID, err := requiredManifestID(inheritedManifestValue(row, manifest, "advertiser_id"), fmt.Sprintf("jobs[%d].advertiser_id", index))
	if err != nil {
		return CreatorManifestJob{}, err
	}
	planTemplate, err := requiredManifestText(inheritedManifestValue(row, manifest, "plan_template"), fmt.Sprintf("jobs[%d].plan_template", index))
	if err != nil {
		return CreatorManifestJob{}, err
	}
	awemeID, err := requiredManifestText(row["aweme_id"], fmt.Sprintf("jobs[%d].aweme_id", index))
	if err != nil {
		return CreatorManifestJob{}, err
	}
	items, ok := row["item_ids"].([]any)
	if !ok || len(items) == 0 {
		return CreatorManifestJob{}, batchInput("invalid_batch_job", fmt.Sprintf("jobs[%d].item_ids cannot be empty", index))
	}
	itemIDs := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		itemID, itemErr := requiredManifestID(item, fmt.Sprintf("jobs[%d].item_ids", index))
		if itemErr != nil {
			return CreatorManifestJob{}, itemErr
		}
		if _, exists := seen[itemID]; exists {
			continue
		}
		seen[itemID] = struct{}{}
		itemIDs = append(itemIDs, itemID)
	}
	productMatch, err := normalizeProductMatch(row["product_match"], index)
	if err != nil {
		return CreatorManifestJob{}, err
	}
	job := CreatorManifestJob{
		Index: index, AdvertiserID: advertiserID, PlanTemplate: planTemplate,
		AwemeID: awemeID, ItemIDs: itemIDs, ProductMatch: productMatch,
	}
	job.Budget, job.budgetSet = inheritedOptionalValue(row, manifest, "budget")
	job.CPABid, job.cpaBidSet = inheritedOptionalValue(row, manifest, "bid")
	job.ROIGoal, job.roiGoalSet = inheritedOptionalValue(row, manifest, "roi_goal")
	job.ProductName, job.productNameSet = inheritedOptionalValue(row, manifest, "product_name")
	job.ProductID, job.productIDSet = inheritedOptionalValue(row, manifest, "product_id")
	if value, set := inheritedOptionalValue(row, manifest, "material_date"); set {
		job.MaterialDate = textOrEmpty(value)
	}
	if value, set := inheritedOptionalValue(row, manifest, "project_name"); set {
		job.ProjectName = textOrEmpty(value)
	}
	if value, set := inheritedOptionalValue(row, manifest, "promotion_name"); set {
		job.PromotionName = textOrEmpty(value)
	}
	return job, nil
}

func normalizeProductMatch(value any, index int) (ProductMatch, error) {
	mapped, ok := value.(map[string]any)
	if !ok {
		return ProductMatch{}, batchInput(
			"product_match_confirmation_required",
			fmt.Sprintf("jobs[%d].product_match must record the product selection decision", index),
		)
	}
	status := strings.ToUpper(textOrEmpty(mapped["status"]))
	evidence := textOrEmpty(mapped["evidence"])
	if status != "MATCHED" && status != "USER_CONFIRMED" || evidence == "" {
		return ProductMatch{}, batchInput(
			"product_match_confirmation_required",
			fmt.Sprintf("jobs[%d].product_match requires status MATCHED or USER_CONFIRMED and evidence", index),
		)
	}
	return ProductMatch{Status: status, Evidence: evidence}, nil
}

func CreatorBatchJobKey(channel string, job CreatorManifestJob) string {
	items := append([]string(nil), job.ItemIDs...)
	sort.Strings(items)
	return strings.Join([]string{
		channel, job.AdvertiserID, job.PlanTemplate, job.AwemeID, strings.Join(items, ","),
	}, ":")
}

func CreatorBatchFingerprint(manifest CreatorBatchManifest) (string, error) {
	rows := make([]any, 0, len(manifest.Jobs))
	for _, job := range manifest.Jobs {
		row := map[string]any{
			"channel": manifest.Channel, "advertiser_id": job.AdvertiserID,
			"plan_template": job.PlanTemplate, "aweme_id": job.AwemeID,
			"item_ids": append([]string(nil), job.ItemIDs...), "material_date": job.MaterialDate,
			"project_name": job.ProjectName, "promotion_name": job.PromotionName,
		}
		for key, optional := range map[string]struct {
			value any
			set   bool
		}{
			"budget": {job.Budget, job.budgetSet}, "bid": {job.CPABid, job.cpaBidSet},
			"roi_goal": {job.ROIGoal, job.roiGoalSet}, "product_name": {job.ProductName, job.productNameSet},
			"product_id": {job.ProductID, job.productIDSet},
		} {
			if optional.set {
				row[key] = canonicalBatchNumber(optional.value)
			}
		}
		rows = append(rows, row)
	}
	buffer := new(bytes.Buffer)
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(rows); err != nil {
		return "", fmt.Errorf("encode creator batch fingerprint: %w", err)
	}
	digest := sha256.Sum256(bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}))
	return hex.EncodeToString(digest[:]), nil
}

func creatorJournalExtra(job CreatorManifestJob) map[string]json.RawMessage {
	result := map[string]json.RawMessage{}
	for key, value := range map[string]any{
		"aweme_id": job.AwemeID, "item_ids": job.ItemIDs, "plan_template": job.PlanTemplate,
	} {
		payload, _ := json.Marshal(value)
		result[key] = payload
	}
	return result
}

type batchConfigSnapshot struct{ value map[string]any }

func (snapshot batchConfigSnapshot) Read(context.Context) (map[string]any, error) {
	return configuration.CloneMap(snapshot.value), nil
}

func inheritedManifestValue(row, manifest map[string]any, key string) any {
	if value, exists := row[key]; exists {
		return value
	}
	return manifest[key]
}

func inheritedOptionalValue(row, manifest map[string]any, key string) (any, bool) {
	value := inheritedManifestValue(row, manifest, key)
	if value == nil {
		return nil, false
	}
	return value, true
}

func requiredManifestID(value any, field string) (string, error) {
	text := textOrEmpty(value)
	if !validPositiveID(text) {
		return "", batchInput("invalid_batch_job", field+" must be a positive decimal string")
	}
	return text, nil
}

func requiredManifestText(value any, field string) (string, error) {
	text := textOrEmpty(value)
	if text == "" {
		return "", batchInput("invalid_batch_job", field+" is required")
	}
	return text, nil
}

func manifestSchemaVersion(value any) (int, error) {
	if value == nil || textOrEmpty(value) == "" || textOrEmpty(value) == "<nil>" {
		return CreatorBatchSchemaVersion, nil
	}
	return strconv.Atoi(textOrEmpty(value))
}

func canonicalBatchNumber(value any) any {
	number, ok := value.(json.Number)
	if !ok || !strings.ContainsAny(number.String(), ".eE") {
		return value
	}
	parsed, err := number.Float64()
	if err != nil {
		return value
	}
	return parsed
}

func textOrEmpty(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func batchInput(code, message string) error {
	return &BatchInputError{Code: code, Message: message}
}
