package marketing

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	applicationmaterials "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/materials"
	sharedplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/plans"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/configuration"
	domainmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/marketing"
	domainplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/plans"
	domaintemplates "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/templates"
)

const (
	UploadBatchJournalVersion = 1
	UploadBatchJournalKind    = "marketing_upload"
	UploadBatchRunPrefix      = "marketing-upload-"
	defaultUploadPageSize     = 100
	defaultAdGetBatchSize     = 50
	defaultAccountConcurrency = 2
	defaultGroupConcurrency   = 2
	defaultCoverConcurrency   = 4
	defaultCoverAttempts      = 8
	defaultCoverWait          = 2 * time.Second
)

type UploadBatchRequest struct {
	ConfigPath         string
	Accounts           []string
	PlanTemplate       string
	AccountTemplates   []string
	Date               string
	Filename           string
	MaterialDate       string
	ProductName        string
	ProductID          string
	Budget             any
	CPABid             any
	ROIGoal            any
	VideosPerUnit      int
	MaxVideos          int
	StartIndex         int
	AccountConcurrency int
	GroupConcurrency   int
	CoverConcurrency   int
	CoverAttempts      int
	CoverWait          time.Duration
	CoverWaitSet       bool
	PageSize           int
	AdGetBatchSize     int
	ValidateAdGet      bool
	SkipMissingCover   bool
	IncludePayloads    bool
	Submit             bool
	Channel            string
	AuthAccountID      string
}

type UploadAccountTemplate struct {
	AdvertiserID string `json:"advertiser_id"`
	PlanTemplate string `json:"plan_template"`
}

type UploadBatchSettings struct {
	Date               string `json:"date"`
	MaterialDate       string `json:"material_date"`
	VideosPerUnit      int    `json:"videos_per_unit"`
	Budget             any    `json:"budget"`
	Bid                any    `json:"bid"`
	ROIGoal            any    `json:"roi_goal"`
	AccountConcurrency int    `json:"account_concurrency"`
	GroupConcurrency   int    `json:"group_concurrency"`
	CoverConcurrency   int    `json:"cover_concurrency"`
	CoverAttempts      int    `json:"cover_attempts"`
	CoverWaitSeconds   any    `json:"cover_wait_sec"`
	ValidateAdGet      bool   `json:"validate_ad_get"`
	SkipMissingCover   bool   `json:"skip_missing_cover"`
}

type UploadBatchVideo struct {
	VideoID    string   `json:"video_id,omitempty"`
	CoverID    string   `json:"video_cover_id,omitempty"`
	MaterialID string   `json:"material_id,omitempty"`
	Filename   string   `json:"filename,omitempty"`
	CreateTime string   `json:"create_time,omitempty"`
	Width      *int64   `json:"width,omitempty"`
	Height     *int64   `json:"height,omitempty"`
	Duration   *float64 `json:"duration,omitempty"`
	Format     string   `json:"format,omitempty"`
	Source     string   `json:"source,omitempty"`
	Signature  string   `json:"signature,omitempty"`
	PosterURL  string   `json:"poster_url,omitempty"`
}

type UploadSkippedVideo struct {
	Reason        string                        `json:"reason"`
	VideoID       string                        `json:"video_id,omitempty"`
	MaterialID    string                        `json:"material_id,omitempty"`
	Filename      string                        `json:"filename,omitempty"`
	CoverStatus   string                        `json:"cover_status,omitempty"`
	CoverResponse *domainplans.OfficialResponse `json:"cover_response,omitempty"`
}

type UploadVideoQuerySummary struct {
	Endpoint        string         `json:"endpoint"`
	Params          map[string]any `json:"params"`
	ResponseCode    int64          `json:"response_code"`
	ResponseMessage string         `json:"response_message,omitempty"`
	PageInfo        any            `json:"page_info,omitempty"`
	RequestIDs      []string       `json:"request_ids"`
}

type UploadBatchGroup struct {
	GroupIndex       int                           `json:"group_index"`
	Status           string                        `json:"status"`
	Videos           []UploadBatchVideo            `json:"videos"`
	MissingFields    []string                      `json:"missing_fields"`
	PreflightSummary map[string]any                `json:"preflight_summary,omitempty"`
	PlannedOperation string                        `json:"planned_operation,omitempty"`
	ProjectID        string                        `json:"project_id,omitempty"`
	PromotionID      string                        `json:"promotion_id,omitempty"`
	FailureStage     string                        `json:"failure_stage,omitempty"`
	LastResponse     *domainplans.OfficialResponse `json:"last_response,omitempty"`
	Reconciliation   *domainplans.Reconciliation   `json:"reconciliation,omitempty"`
	Error            string                        `json:"error,omitempty"`
	ProjectPayload   map[string]any                `json:"project_payload,omitempty"`
	PromotionPayload map[string]any                `json:"promotion_payload,omitempty"`
	jobKey           string
	projectJSON      json.RawMessage
	promotionJSON    json.RawMessage
	blockedError     string
	projectName      string
	promotionName    string
}

type UploadBatchAccount struct {
	AdvertiserID           string                        `json:"advertiser_id"`
	PlanTemplate           any                           `json:"plan_template"`
	Status                 string                        `json:"status"`
	Groups                 []UploadBatchGroup            `json:"groups"`
	SkippedVideos          []UploadSkippedVideo          `json:"skipped_videos"`
	RuntimeAssetResolution map[string]any                `json:"runtime_asset_resolution,omitempty"`
	VideoQuery             *UploadVideoQuerySummary      `json:"video_query,omitempty"`
	LibraryVideoCount      int                           `json:"library_video_count,omitempty"`
	DedupedVideoCount      int                           `json:"deduped_video_count,omitempty"`
	LimitedVideoCount      *int                          `json:"limited_to_video_count,omitempty"`
	AdGetRequestIDs        []string                      `json:"ad_get_request_ids"`
	ValidatedVideoCount    int                           `json:"validated_video_count,omitempty"`
	CoverReadyVideoCount   int                           `json:"cover_ready_video_count,omitempty"`
	GroupCount             int                           `json:"group_count,omitempty"`
	CreatedProjectCount    int                           `json:"created_project_count,omitempty"`
	CreatedPromotionCount  int                           `json:"created_promotion_count,omitempty"`
	BlockedGroupCount      int                           `json:"blocked_group_count,omitempty"`
	FailedGroupCount       int                           `json:"failed_group_count,omitempty"`
	ErrorCode              string                        `json:"error_code,omitempty"`
	Error                  string                        `json:"error,omitempty"`
	Details                map[string]any                `json:"details,omitempty"`
	LastResponse           *domainplans.OfficialResponse `json:"last_response,omitempty"`
}

type UploadBatchTotals struct {
	AccountCount          int `json:"account_count"`
	QualifiedVideoCount   int `json:"qualified_video_count"`
	SkippedVideoCount     int `json:"skipped_video_count"`
	GroupCount            int `json:"group_count"`
	CreatedProjectCount   int `json:"created_project_count"`
	CreatedPromotionCount int `json:"created_promotion_count"`
	FailedGroupCount      int `json:"failed_group_count"`
	BlockedGroupCount     int `json:"blocked_group_count"`
}

type UploadBatchResult struct {
	Mode             string                  `json:"mode"`
	GeneratedAt      string                  `json:"generated_at"`
	Config           string                  `json:"config"`
	AccountTemplates []UploadAccountTemplate `json:"account_templates"`
	Settings         UploadBatchSettings     `json:"settings"`
	Accounts         []UploadBatchAccount    `json:"accounts"`
	Totals           UploadBatchTotals       `json:"totals"`
	ExitCode         int                     `json:"-"`
	RunID            string                  `json:"-"`
}

type UploadBatchService struct {
	Config        ConfigReader
	Materials     MaterialService
	RuntimeAssets RuntimeAssetResolver
	Executor      TransactionExecutor
	Journals      sharedplans.JournalStore
	Catalog       sharedplans.JournalCatalog
	ScopeLocker   sharedplans.JournalScopeLocker
	Now           func() time.Time
	NewRunID      func(string, time.Time) (string, error)
}

type uploadBatchScope struct {
	SchemaVersion int                     `json:"schema_version"`
	Channel       string                  `json:"channel"`
	Accounts      []UploadAccountTemplate `json:"accounts"`
	Date          string                  `json:"date"`
	Filename      string                  `json:"filename,omitempty"`
	MaterialDate  string                  `json:"material_date"`
	ProductName   string                  `json:"product_name,omitempty"`
	ProductID     string                  `json:"product_id,omitempty"`
	Budget        any                     `json:"budget,omitempty"`
	CPABid        any                     `json:"bid,omitempty"`
	ROIGoal       any                     `json:"roi_goal,omitempty"`
	VideosPerUnit int                     `json:"videos_per_unit"`
	MaxVideos     int                     `json:"max_videos,omitempty"`
	StartIndex    int                     `json:"start_index"`
	ValidateAdGet bool                    `json:"validate_ad_get"`
	SkipMissing   bool                    `json:"skip_missing_cover"`
	AuthAccountID string                  `json:"auth_account_id,omitempty"`
}

type uploadFrozenGroup struct {
	GroupIndex    int                `json:"group_index"`
	JobKey        string             `json:"job_key"`
	Videos        []UploadBatchVideo `json:"videos"`
	ProjectName   string             `json:"project_name"`
	PromotionName string             `json:"promotion_name"`
}

type uploadFrozenAccount struct {
	AdvertiserID              string              `json:"advertiser_id"`
	PlanTemplate              string              `json:"plan_template"`
	ResolvedConfigFingerprint string              `json:"resolved_config_fingerprint"`
	Base                      UploadBatchAccount  `json:"base"`
	Groups                    []uploadFrozenGroup `json:"groups"`
}

type uploadJournalMetadata struct {
	SchemaVersion    int                     `json:"schema_version"`
	Kind             string                  `json:"kind"`
	ScopeFingerprint string                  `json:"scope_fingerprint"`
	AccountTemplates []UploadAccountTemplate `json:"account_templates"`
	Accounts         []uploadFrozenAccount   `json:"accounts"`
}

type uploadAccountWork struct {
	mapping             UploadAccountTemplate
	effective           map[string]any
	resolvedFingerprint string
	account             UploadBatchAccount
	frozen              uploadFrozenAccount
	groups              []UploadBatchGroup
}

type uploadCoverResult struct {
	CoverID string
	Status  string
	Err     error
}

func (service UploadBatchService) Execute(
	ctx context.Context,
	request UploadBatchRequest,
) (UploadBatchResult, error) {
	if ctx == nil {
		return UploadBatchResult{}, errors.New("Marketing upload batch context is required")
	}
	if service.Config == nil {
		return UploadBatchResult{}, errors.New("Marketing upload batch config reader is required")
	}
	now := service.now()
	normalized, resolvedDate, err := normalizeUploadBatchRequest(request, now)
	if err != nil {
		return UploadBatchResult{}, batchInput("invalid_batch_upload", err.Error())
	}
	raw, err := service.Config.Read(ctx)
	if err != nil {
		return UploadBatchResult{}, fmt.Errorf("read Marketing upload batch config: %w", err)
	}
	runtimeConfig, _, err := configuration.Runtime(raw, "marketing", "create")
	if err != nil {
		return UploadBatchResult{}, err
	}
	mappings, effective, err := resolveUploadAccountTemplates(runtimeConfig, normalized)
	if err != nil {
		return UploadBatchResult{}, batchInput("invalid_batch_upload", err.Error())
	}
	if normalized.VideosPerUnit == 0 {
		normalized.VideosPerUnit, err = materialLimit(effective[0])
		if err != nil {
			return UploadBatchResult{}, batchInput("invalid_batch_upload", err.Error())
		}
	}
	if normalized.VideosPerUnit > defaultMaterialLimit {
		return UploadBatchResult{}, batchInput("invalid_batch_upload", "videos-per-unit must be <= 5 for the current promotion material rule")
	}
	for _, config := range effective {
		limit, limitErr := materialLimit(config)
		if limitErr != nil {
			return UploadBatchResult{}, batchInput("invalid_batch_upload", limitErr.Error())
		}
		if normalized.VideosPerUnit > limit {
			return UploadBatchResult{}, batchInput("invalid_batch_upload", fmt.Sprintf("videos-per-unit exceeds template limit %d", limit))
		}
	}
	materialDate := normalized.MaterialDate
	if materialDate == "" {
		parsedDate, _ := time.ParseInLocation("2006-01-02", resolvedDate, now.Location())
		materialDate = fmt.Sprintf("%d.%d", int(parsedDate.Month()), parsedDate.Day())
	}
	normalized.MaterialDate = materialDate
	scope := uploadBatchScope{
		SchemaVersion: UploadBatchJournalVersion, Channel: "marketing", Accounts: mappings,
		Date: resolvedDate, Filename: normalized.Filename, MaterialDate: materialDate,
		ProductName: normalized.ProductName, ProductID: normalized.ProductID,
		Budget: normalized.Budget, CPABid: normalized.CPABid, ROIGoal: normalized.ROIGoal,
		VideosPerUnit: normalized.VideosPerUnit, MaxVideos: normalized.MaxVideos,
		StartIndex: normalized.StartIndex, ValidateAdGet: normalized.ValidateAdGet,
		SkipMissing: normalized.SkipMissingCover, AuthAccountID: normalized.AuthAccountID,
	}
	scopeFingerprint, err := uploadScopeFingerprint(scope)
	if err != nil {
		return UploadBatchResult{}, err
	}
	baseResult := UploadBatchResult{
		Mode: "dry_run", GeneratedAt: now.Format("2006-01-02T15:04:05"),
		Config: normalized.ConfigPath, AccountTemplates: mappings,
		Settings: uploadSettings(normalized),
	}
	if !normalized.Submit {
		works := service.prepareNewUploadAccounts(ctx, normalized, resolvedDate, mappings, effective)
		metadata := uploadMetadata(scopeFingerprint, mappings, works)
		transactionFingerprint, fingerprintErr := uploadFingerprint(metadata)
		if fingerprintErr != nil {
			return UploadBatchResult{}, fingerprintErr
		}
		return service.finishUploadBatch(
			ctx, normalized, baseResult, scopeFingerprint, transactionFingerprint,
			"", works, metadata, nil,
		)
	}
	if service.Journals == nil || service.Catalog == nil || service.ScopeLocker == nil {
		return UploadBatchResult{}, errors.New("Marketing upload batch journal dependencies are required for submit")
	}
	release, err := service.ScopeLocker.AcquireScope(ctx, scopeFingerprint)
	if err != nil {
		return UploadBatchResult{}, fmt.Errorf("lock Marketing upload batch scope: %w", err)
	}
	defer func() { _ = release() }()
	record, exists, err := service.findUploadJournal(ctx, scopeFingerprint)
	if err != nil {
		return UploadBatchResult{}, err
	}
	baseResult.Mode = "submit"
	if exists {
		metadata, metadataErr := decodeUploadMetadata(record.Journal)
		if metadataErr != nil {
			return UploadBatchResult{}, metadataErr
		}
		works := service.prepareFrozenUploadAccounts(
			ctx, normalized, resolvedDate, metadata, effective,
		)
		updatedMetadata := uploadMetadata(scopeFingerprint, mappings, works)
		transactionFingerprint, fingerprintErr := uploadFingerprint(updatedMetadata)
		if fingerprintErr != nil {
			return UploadBatchResult{}, fingerprintErr
		}
		journal := record.Journal
		return service.finishUploadBatch(
			ctx, normalized, baseResult, scopeFingerprint, transactionFingerprint,
			record.RunID, works, updatedMetadata, &journal,
		)
	}
	works := service.prepareNewUploadAccounts(ctx, normalized, resolvedDate, mappings, effective)
	runID, err := service.newRunID(scopeFingerprint, now)
	if err != nil {
		return UploadBatchResult{}, err
	}
	metadata := uploadMetadata(scopeFingerprint, mappings, works)
	transactionFingerprint, err := uploadFingerprint(metadata)
	if err != nil {
		return UploadBatchResult{}, err
	}
	return service.finishUploadBatch(
		ctx, normalized, baseResult, scopeFingerprint, transactionFingerprint,
		runID, works, metadata, nil,
	)
}

func (service UploadBatchService) finishUploadBatch(
	ctx context.Context,
	request UploadBatchRequest,
	result UploadBatchResult,
	scopeFingerprint string,
	transactionFingerprint string,
	runID string,
	works []uploadAccountWork,
	metadata uploadJournalMetadata,
	existingJournal *domainplans.Journal,
) (UploadBatchResult, error) {
	jobs := []BatchJob{}
	for workIndex := range works {
		for groupIndex := range works[workIndex].groups {
			group := &works[workIndex].groups[groupIndex]
			if len(group.MissingFields) != 0 && group.blockedError == "" {
				group.blockedError = "Marketing upload group has unresolved required fields"
			}
			jobs = append(jobs, BatchJob{
				Key: group.jobKey, Kind: BatchUpload,
				AdvertiserID:   works[workIndex].mapping.AdvertiserID,
				AuthAccountID:  request.AuthAccountID,
				ProjectPayload: group.projectJSON, PromotionPayload: group.promotionJSON,
				BlockedError: group.blockedError,
				JournalExtra: uploadGroupJournalExtra(works[workIndex].mapping.PlanTemplate, *group),
			})
		}
	}
	if request.Submit {
		_, journalErr := service.saveUploadJournal(
			ctx, runID, transactionFingerprint, metadata, works, existingJournal,
		)
		if journalErr != nil {
			return UploadBatchResult{}, journalErr
		}
	}
	if len(jobs) != 0 {
		executor := service.Executor
		if executor == nil && !request.Submit {
			executor = Executor{}
		}
		if executor == nil {
			return UploadBatchResult{}, errors.New("Marketing transaction executor is required for upload batch submit")
		}
		if runID == "" {
			runID = "marketing-upload-preview-" + scopeFingerprint[:16]
		}
		batchRequest := BatchRequest{
			RunID: runID, Fingerprint: transactionFingerprint, Submit: request.Submit,
			MaxConcurrency: request.AccountConcurrency, Jobs: jobs,
		}
		runner := BatchRunner{Executor: executor, Now: service.Now}
		if request.Submit {
			runner.Journals = service.Journals
		}
		batch, err := runner.Execute(ctx, batchRequest)
		if err != nil {
			return UploadBatchResult{}, err
		}
		applyUploadBatchRows(works, batch.Rows, request.IncludePayloads)
	}
	result.RunID = runID
	result.Accounts = make([]UploadBatchAccount, len(works))
	for index := range works {
		finalizeUploadAccount(&works[index].account, works[index].groups, request.Submit)
		result.Accounts[index] = works[index].account
	}
	result.Totals = uploadTotals(result.Accounts)
	result.ExitCode = uploadExitCode(result.Accounts)
	return result, nil
}

func (service UploadBatchService) prepareNewUploadAccounts(
	ctx context.Context,
	request UploadBatchRequest,
	resolvedDate string,
	mappings []UploadAccountTemplate,
	effective []map[string]any,
) []uploadAccountWork {
	works := make([]uploadAccountWork, len(mappings))
	limit := min(request.AccountConcurrency, len(mappings))
	semaphore := make(chan struct{}, max(1, limit))
	var waitGroup sync.WaitGroup
	for index := range mappings {
		index := index
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			works[index] = service.prepareNewUploadAccount(
				ctx, request, resolvedDate, mappings[index], effective[index],
			)
		}()
	}
	waitGroup.Wait()
	return works
}

func (service UploadBatchService) prepareNewUploadAccount(
	ctx context.Context,
	request UploadBatchRequest,
	resolvedDate string,
	mapping UploadAccountTemplate,
	effective map[string]any,
) uploadAccountWork {
	work := uploadAccountWork{mapping: mapping, effective: configuration.CloneMap(effective)}
	work.account = newUploadAccount(mapping, effective)
	resolved, err := service.resolveUploadRuntime(ctx, request, mapping, effective)
	if err != nil {
		work.account.Status = "blocked"
		work.account.Error = err.Error()
		return work
	}
	work.effective = resolved.Config
	work.account.RuntimeAssetResolution = resolved.Evidence
	work.resolvedFingerprint, err = uploadResolvedConfigFingerprint(work.effective)
	if err != nil {
		work.account.Status = "failed"
		work.account.Error = err.Error()
		return work
	}
	library, err := service.Materials.QueryVideos(ctx, applicationmaterials.VideoQuery{
		CredentialScope: applicationmaterials.CredentialScope{
			AdvertiserID: mapping.AdvertiserID, AuthAccountID: request.AuthAccountID,
		},
		Mode: "library-get", Filename: request.Filename, Date: resolvedDate,
		Page: 1, PageSize: request.PageSize, FetchAll: true,
	})
	if err != nil {
		work.account.Status = "query_failed"
		work.account.Error = err.Error()
		return work
	}
	work.account.VideoQuery = &UploadVideoQuerySummary{
		Endpoint: library.Endpoint, Params: library.Params, ResponseCode: library.ResponseCode,
		ResponseMessage: library.ResponseMessage, PageInfo: library.PageInfo,
		RequestIDs: append([]string(nil), library.RequestIDs...),
	}
	rows, ok := library.MatchedList.([]domainmarketing.VideoAsset)
	if !ok {
		work.account.Status = "query_failed"
		work.account.Error = "video library returned an invalid result type"
		return work
	}
	work.account.LibraryVideoCount = len(rows)
	videos, skipped := compactUploadVideos(rows)
	work.account.DedupedVideoCount = len(videos)
	work.account.SkippedVideos = append(work.account.SkippedVideos, skipped...)
	if request.MaxVideos > 0 && len(videos) > request.MaxVideos {
		videos = append([]UploadBatchVideo(nil), videos[:request.MaxVideos]...)
		limited := len(videos)
		work.account.LimitedVideoCount = &limited
	} else if request.MaxVideos > 0 {
		limited := len(videos)
		work.account.LimitedVideoCount = &limited
	}
	ready, skipped, requestIDs, err := service.validateUploadVideos(ctx, request, mapping.AdvertiserID, videos)
	work.account.AdGetRequestIDs = requestIDs
	if err != nil {
		work.account.Status = "failed"
		work.account.Error = err.Error()
		return work
	}
	work.account.ValidatedVideoCount = len(ready)
	work.account.SkippedVideos = append(work.account.SkippedVideos, skipped...)
	ready, skipped = service.attachUploadCovers(ctx, request, mapping.AdvertiserID, ready)
	work.account.CoverReadyVideoCount = len(ready)
	work.account.SkippedVideos = append(work.account.SkippedVideos, skipped...)
	if len(skipped) != 0 && !request.SkipMissingCover {
		work.account.Status = "blocked"
		work.account.Error = "some videos have no suggested cover"
		return work
	}
	if len(ready) == 0 {
		work.account.Status = "no_qualified_videos"
		return work
	}
	work.groups = buildUploadGroups(request, mapping, work.effective, ready)
	work.account.Groups = cloneUploadGroups(work.groups, request.IncludePayloads)
	work.account.GroupCount = len(work.groups)
	work.account.Status = "running"
	work.frozen = freezeUploadAccount(work)
	return work
}

func (service UploadBatchService) prepareFrozenUploadAccounts(
	ctx context.Context,
	request UploadBatchRequest,
	resolvedDate string,
	metadata uploadJournalMetadata,
	effective []map[string]any,
) []uploadAccountWork {
	byAdvertiser := make(map[string]map[string]any, len(metadata.Accounts))
	for index, mapping := range metadata.AccountTemplates {
		byAdvertiser[mapping.AdvertiserID] = effective[index]
	}
	works := make([]uploadAccountWork, len(metadata.Accounts))
	limit := min(request.AccountConcurrency, len(metadata.Accounts))
	semaphore := make(chan struct{}, max(1, limit))
	var waitGroup sync.WaitGroup
	for index := range metadata.Accounts {
		index := index
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			works[index] = service.prepareFrozenUploadAccount(
				ctx, request, resolvedDate, metadata.Accounts[index], byAdvertiser[metadata.Accounts[index].AdvertiserID],
			)
		}()
	}
	waitGroup.Wait()
	return works
}

func (service UploadBatchService) prepareFrozenUploadAccount(
	ctx context.Context,
	request UploadBatchRequest,
	resolvedDate string,
	frozen uploadFrozenAccount,
	effective map[string]any,
) uploadAccountWork {
	mapping := UploadAccountTemplate{AdvertiserID: frozen.AdvertiserID, PlanTemplate: frozen.PlanTemplate}
	if len(frozen.Groups) == 0 {
		work := service.prepareNewUploadAccount(ctx, request, resolvedDate, mapping, effective)
		work.frozen = freezeUploadAccount(work)
		return work
	}
	work := uploadAccountWork{mapping: mapping, frozen: frozen, account: cloneUploadAccount(frozen.Base)}
	resolved, err := service.resolveUploadRuntime(ctx, request, mapping, effective)
	if err != nil {
		return blockedFrozenUploadWork(work, frozen, err.Error())
	}
	work.effective = resolved.Config
	work.account.RuntimeAssetResolution = resolved.Evidence
	fingerprint, err := uploadResolvedConfigFingerprint(work.effective)
	if err != nil {
		return blockedFrozenUploadWork(work, frozen, err.Error())
	}
	work.resolvedFingerprint = fingerprint
	if fingerprint != frozen.ResolvedConfigFingerprint {
		return blockedFrozenUploadWork(work, frozen, "selected template or resolved runtime assets changed after the upload batch was frozen")
	}
	all := flattenFrozenUploadVideos(frozen.Groups)
	validated, skipped, requestIDs, err := service.validateUploadVideos(ctx, request, mapping.AdvertiserID, all)
	work.account.AdGetRequestIDs = requestIDs
	if err != nil {
		return blockedFrozenUploadWork(work, frozen, err.Error())
	}
	work.account.ValidatedVideoCount = len(validated)
	work.account.SkippedVideos = append(cloneSkippedVideos(frozen.Base.SkippedVideos), skipped...)
	ready, coverSkipped := service.attachUploadCovers(ctx, request, mapping.AdvertiserID, validated)
	work.account.CoverReadyVideoCount = len(ready)
	work.account.SkippedVideos = append(work.account.SkippedVideos, coverSkipped...)
	byID := make(map[string]UploadBatchVideo, len(ready))
	for _, video := range ready {
		byID[video.VideoID] = video
	}
	work.groups = make([]UploadBatchGroup, 0, len(frozen.Groups))
	for _, frozenGroup := range frozen.Groups {
		videos := make([]UploadBatchVideo, 0, len(frozenGroup.Videos))
		missing := []string{}
		for _, original := range frozenGroup.Videos {
			current, exists := byID[original.VideoID]
			if !exists {
				missing = append(missing, original.VideoID)
				current = original
			}
			videos = append(videos, current)
		}
		groups := buildUploadGroupsForFrozen(request, mapping, work.effective, frozenGroup, videos)
		group := groups[0]
		if len(missing) != 0 {
			group.blockedError = "frozen videos are no longer promotion-ready: " + strings.Join(missing, ", ")
			group.Status = "blocked"
		}
		if group.projectName != frozenGroup.ProjectName || group.promotionName != frozenGroup.PromotionName {
			group.blockedError = "project or promotion name changed after the upload batch was frozen"
			group.Status = "blocked"
		}
		work.groups = append(work.groups, group)
	}
	work.account.Groups = cloneUploadGroups(work.groups, request.IncludePayloads)
	work.account.GroupCount = len(work.groups)
	work.account.Status = "running"
	return work
}

func (service UploadBatchService) resolveUploadRuntime(
	ctx context.Context,
	request UploadBatchRequest,
	mapping UploadAccountTemplate,
	effective map[string]any,
) (RuntimeAssetResult, error) {
	if service.RuntimeAssets == nil {
		return RuntimeAssetResult{}, errors.New("Marketing runtime asset resolver is required for upload batch")
	}
	if service.Materials == nil {
		return RuntimeAssetResult{}, errors.New("Marketing material service is required for upload batch")
	}
	return service.RuntimeAssets.Resolve(ctx, RuntimeAssetRequest{
		AdvertiserID: mapping.AdvertiserID, AuthAccountID: request.AuthAccountID,
		Config: configuration.CloneMap(effective),
	})
}

func (service UploadBatchService) validateUploadVideos(
	ctx context.Context,
	request UploadBatchRequest,
	advertiserID string,
	videos []UploadBatchVideo,
) ([]UploadBatchVideo, []UploadSkippedVideo, []string, error) {
	if !request.ValidateAdGet || len(videos) == 0 {
		return append([]UploadBatchVideo(nil), videos...), []UploadSkippedVideo{}, []string{}, nil
	}
	accepted := map[string]domainmarketing.VideoAsset{}
	requestIDs := []string{}
	for start := 0; start < len(videos); start += request.AdGetBatchSize {
		end := min(start+request.AdGetBatchSize, len(videos))
		ids := make([]string, 0, end-start)
		for _, video := range videos[start:end] {
			ids = append(ids, video.VideoID)
		}
		result, err := service.Materials.QueryVideos(ctx, applicationmaterials.VideoQuery{
			CredentialScope: applicationmaterials.CredentialScope{
				AdvertiserID: advertiserID, AuthAccountID: request.AuthAccountID,
			},
			Mode: "ad-get", VideoIDs: ids,
		})
		if err != nil {
			return nil, nil, requestIDs, fmt.Errorf("validate promotion-ready uploaded videos: %w", err)
		}
		requestIDs = append(requestIDs, result.RequestIDs...)
		rows, ok := result.MatchedList.([]domainmarketing.VideoAsset)
		if !ok {
			return nil, nil, requestIDs, errors.New("promotion-ready video query returned an invalid result type")
		}
		for _, row := range rows {
			if strings.TrimSpace(row.ID) == "" {
				return nil, nil, requestIDs, errors.New("promotion-ready video query returned an empty video_id")
			}
			if _, exists := accepted[row.ID]; exists {
				return nil, nil, requestIDs, fmt.Errorf("promotion-ready video query returned duplicate video_id %s", row.ID)
			}
			accepted[row.ID] = row
		}
	}
	ready := make([]UploadBatchVideo, 0, len(videos))
	skipped := []UploadSkippedVideo{}
	for _, original := range videos {
		row, exists := accepted[original.VideoID]
		if !exists {
			skipped = append(skipped, UploadSkippedVideo{
				Reason: "not_returned_by_ad_get", VideoID: original.VideoID,
				MaterialID: original.MaterialID, Filename: original.Filename,
			})
			continue
		}
		ready = append(ready, mergeUploadVideo(original, row))
	}
	return ready, skipped, requestIDs, nil
}

func (service UploadBatchService) attachUploadCovers(
	ctx context.Context,
	request UploadBatchRequest,
	advertiserID string,
	videos []UploadBatchVideo,
) ([]UploadBatchVideo, []UploadSkippedVideo) {
	if len(videos) == 0 {
		return []UploadBatchVideo{}, []UploadSkippedVideo{}
	}
	results := make([]uploadCoverResult, len(videos))
	limit := min(request.CoverConcurrency, len(videos))
	semaphore := make(chan struct{}, max(1, limit))
	var waitGroup sync.WaitGroup
	for index := range videos {
		index := index
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			result, err := service.Materials.QueryVideos(ctx, applicationmaterials.VideoQuery{
				CredentialScope: applicationmaterials.CredentialScope{
					AdvertiserID: advertiserID, AuthAccountID: request.AuthAccountID,
				},
				Mode: "cover-suggest", VideoIDs: []string{videos[index].VideoID},
				CoverAttempts: request.CoverAttempts, CoverWait: request.CoverWait,
			})
			results[index] = uploadCoverResult{CoverID: result.SelectedCoverID, Status: result.Status, Err: err}
		}()
	}
	waitGroup.Wait()
	ready := make([]UploadBatchVideo, 0, len(videos))
	skipped := []UploadSkippedVideo{}
	for index, video := range videos {
		cover := results[index]
		if cover.Err == nil && strings.TrimSpace(cover.CoverID) != "" {
			video.CoverID = cover.CoverID
			ready = append(ready, video)
			continue
		}
		skipped = append(skipped, UploadSkippedVideo{
			Reason: "missing_video_cover", VideoID: video.VideoID,
			MaterialID: video.MaterialID, Filename: video.Filename,
			CoverStatus: cover.Status,
		})
	}
	return ready, skipped
}

func (service UploadBatchService) findUploadJournal(
	ctx context.Context,
	scopeFingerprint string,
) (domainplans.JournalRecord, bool, error) {
	records, err := service.Catalog.List(ctx, UploadBatchRunPrefix)
	if err != nil {
		return domainplans.JournalRecord{}, false, fmt.Errorf("list Marketing upload batch journals: %w", err)
	}
	matches := []domainplans.JournalRecord{}
	for _, record := range records {
		metadata, decodeErr := decodeUploadMetadata(record.Journal)
		if decodeErr != nil {
			return domainplans.JournalRecord{}, false, fmt.Errorf("read Marketing upload batch journal %s: %w", record.RunID, decodeErr)
		}
		if metadata.ScopeFingerprint != scopeFingerprint || uploadJournalCompleted(record.Journal) {
			continue
		}
		matches = append(matches, record)
	}
	if len(matches) > 1 {
		ids := make([]string, len(matches))
		for index := range matches {
			ids[index] = matches[index].RunID
		}
		return domainplans.JournalRecord{}, false, batchInput(
			"ambiguous_batch_resume",
			"multiple unfinished Marketing upload batches match this scope: "+strings.Join(ids, ", "),
		)
	}
	if len(matches) == 0 {
		return domainplans.JournalRecord{}, false, nil
	}
	return matches[0], true, nil
}

func resolveUploadAccountTemplates(
	config map[string]any,
	request UploadBatchRequest,
) ([]UploadAccountTemplate, []map[string]any, error) {
	mappingValues, err := parseUploadTemplateMappings(request.AccountTemplates)
	if err != nil {
		return nil, nil, err
	}
	accounts := uniqueUploadStrings(request.Accounts)
	if len(accounts) == 0 {
		if accountID := textValue(configuration.Value(config, "account.advertiser_id")); accountID != "" && accountID != "<nil>" {
			accounts = append(accounts, accountID)
		}
		for _, value := range anySlice(config["accounts"]) {
			accountID := ""
			if row, ok := value.(map[string]any); ok {
				accountID = textValue(row["advertiser_id"])
			} else {
				accountID = textValue(value)
			}
			if accountID != "" && accountID != "<nil>" {
				accounts = append(accounts, accountID)
			}
		}
		accounts = uniqueUploadStrings(accounts)
	}
	if len(mappingValues) != 0 {
		if len(accounts) == 0 {
			for _, value := range request.AccountTemplates {
				advertiserID := strings.TrimSpace(strings.SplitN(value, "=", 2)[0])
				if _, exists := mappingValues[advertiserID]; exists {
					accounts = append(accounts, advertiserID)
				}
			}
			accounts = uniqueUploadStrings(accounts)
		} else {
			missing, extra := []string{}, []string{}
			for _, account := range accounts {
				if _, exists := mappingValues[account]; !exists {
					missing = append(missing, account)
				}
			}
			for account := range mappingValues {
				if !containsUploadString(accounts, account) {
					extra = append(extra, account)
				}
			}
			sort.Strings(extra)
			if len(missing) != 0 || len(extra) != 0 {
				return nil, nil, fmt.Errorf("account template mappings must exactly match accounts; missing=%v, extra=%v", missing, extra)
			}
		}
	}
	if len(accounts) == 0 {
		return nil, nil, errors.New("no advertiser account found; set account.advertiser_id or pass --accounts")
	}
	if strings.TrimSpace(request.PlanTemplate) != "" && len(accounts) > 1 {
		return nil, nil, errors.New("--plan-template can only be used for one account; use --account-template for multiple accounts")
	}
	mappings := make([]UploadAccountTemplate, 0, len(accounts))
	effective := make([]map[string]any, 0, len(accounts))
	for _, advertiserID := range accounts {
		if !validPositiveID(advertiserID) {
			return nil, nil, errors.New("advertiser IDs must be positive decimal strings")
		}
		templateName := mappingValues[advertiserID]
		if templateName == "" {
			templateName = strings.TrimSpace(request.PlanTemplate)
		}
		if templateName == "" {
			return nil, nil, fmt.Errorf("advertiser %s needs an explicit template mapping", advertiserID)
		}
		applied, applyErr := domaintemplates.ApplyMarketingPlanTemplate(config, domaintemplates.MarketingPlanTemplateSelection{
			Name: templateName, AdvertiserID: advertiserID, Channel: "marketing",
		})
		if applyErr != nil {
			return nil, nil, applyErr
		}
		if textValue(configuration.Value(applied, "material_strategy.source_type")) != AccountUploadSource {
			return nil, nil, fmt.Errorf("plan template %s uses creator-authorized materials; plans batch-upload only supports account uploads", templateName)
		}
		mappings = append(mappings, UploadAccountTemplate{AdvertiserID: advertiserID, PlanTemplate: templateName})
		effective = append(effective, applied)
	}
	return mappings, effective, nil
}

func normalizeUploadBatchRequest(
	request UploadBatchRequest,
	now time.Time,
) (UploadBatchRequest, string, error) {
	request.ConfigPath = strings.TrimSpace(request.ConfigPath)
	request.Accounts = splitUploadCSV(request.Accounts)
	request.PlanTemplate = strings.TrimSpace(request.PlanTemplate)
	request.Date = strings.TrimSpace(request.Date)
	if request.Date == "" {
		request.Date = "today"
	}
	request.Filename = strings.TrimSpace(request.Filename)
	request.MaterialDate = strings.TrimSpace(request.MaterialDate)
	request.ProductName = strings.TrimSpace(request.ProductName)
	request.ProductID = strings.TrimSpace(request.ProductID)
	request.Channel = strings.TrimSpace(request.Channel)
	if request.Channel == "" {
		request.Channel = "marketing"
	}
	request.AuthAccountID = strings.TrimSpace(request.AuthAccountID)
	if request.Channel != "marketing" {
		return request, "", errors.New("Marketing upload batches only support --channel marketing")
	}
	resolvedDate, err := resolveUploadDate(request.Date, now)
	if err != nil {
		return request, "", err
	}
	if request.StartIndex == 0 {
		request.StartIndex = 1
	}
	if request.AccountConcurrency == 0 {
		request.AccountConcurrency = defaultAccountConcurrency
	}
	if request.GroupConcurrency == 0 {
		request.GroupConcurrency = defaultGroupConcurrency
	}
	if request.CoverConcurrency == 0 {
		request.CoverConcurrency = defaultCoverConcurrency
	}
	if request.CoverAttempts == 0 {
		request.CoverAttempts = defaultCoverAttempts
	}
	if request.CoverWait == 0 && !request.CoverWaitSet {
		request.CoverWait = defaultCoverWait
	}
	if request.PageSize == 0 {
		request.PageSize = defaultUploadPageSize
	}
	if request.AdGetBatchSize == 0 {
		request.AdGetBatchSize = defaultAdGetBatchSize
	}
	if request.VideosPerUnit < 0 || request.MaxVideos < 0 || request.StartIndex < 1 {
		return request, "", errors.New("video limits and start-index must be positive")
	}
	for name, value := range map[string]int{
		"account-concurrency": request.AccountConcurrency,
		"group-concurrency":   request.GroupConcurrency,
		"cover-concurrency":   request.CoverConcurrency,
		"cover-attempts":      request.CoverAttempts,
	} {
		if value < 1 || value > 100 {
			return request, "", fmt.Errorf("%s must be between 1 and 100", name)
		}
	}
	if request.AccountConcurrency > CreatorBatchMaxConcurrency {
		return request, "", errors.New("account-concurrency must not exceed 10")
	}
	if request.PageSize < 1 || request.PageSize > applicationmaterials.MaxPageSize {
		return request, "", errors.New("page-size must be between 1 and 100")
	}
	if request.AdGetBatchSize < 1 || request.AdGetBatchSize > applicationmaterials.MaxBatchSize {
		return request, "", errors.New("ad-get-batch-size must be between 1 and 100")
	}
	if request.CoverWait < 0 || request.CoverWait > 5*time.Minute {
		return request, "", errors.New("cover-wait-sec must be between 0 and 300")
	}
	return request, resolvedDate, nil
}

func buildUploadGroups(
	request UploadBatchRequest,
	mapping UploadAccountTemplate,
	effective map[string]any,
	videos []UploadBatchVideo,
) []UploadBatchGroup {
	groups := []UploadBatchGroup{}
	for start, groupNumber := 0, 1; start < len(videos); start, groupNumber = start+request.VideosPerUnit, groupNumber+1 {
		end := min(start+request.VideosPerUnit, len(videos))
		groups = append(groups, buildUploadGroup(
			request, mapping, effective, groupNumber,
			append([]UploadBatchVideo(nil), videos[start:end]...), "", "",
		))
	}
	return groups
}

func buildUploadGroupsForFrozen(
	request UploadBatchRequest,
	mapping UploadAccountTemplate,
	effective map[string]any,
	frozen uploadFrozenGroup,
	videos []UploadBatchVideo,
) []UploadBatchGroup {
	return []UploadBatchGroup{buildUploadGroup(
		request, mapping, effective, frozen.GroupIndex, videos,
		frozen.ProjectName, frozen.PromotionName,
	)}
}

func buildUploadGroup(
	request UploadBatchRequest,
	mapping UploadAccountTemplate,
	effective map[string]any,
	groupNumber int,
	videos []UploadBatchVideo,
	frozenProjectName string,
	frozenPromotionName string,
) UploadBatchGroup {
	config := configuration.CloneMap(effective)
	prepared := make([]PreparedVideo, 0, len(videos))
	for _, video := range videos {
		prepared = append(prepared, PreparedVideo{VideoID: video.VideoID, CoverID: video.CoverID, Title: video.Filename})
	}
	applyPreparedVideos(config, prepared)
	projectName, promotionName, suffixIndex, suffix := uploadGroupNames(config, request, groupNumber)
	payloads, err := BuildPayloads(config, PayloadOptions{
		AdvertiserID: mapping.AdvertiserID, Budget: request.Budget, CPABid: request.CPABid,
		ROIGoal: request.ROIGoal, MaterialDate: request.MaterialDate,
		ProductName: request.ProductName, ProductID: request.ProductID,
		ProjectName: projectName, PromotionName: promotionName,
		GroupIndex: suffixIndex, Index: suffixIndex, Suffix: suffix,
	})
	group := UploadBatchGroup{
		GroupIndex: groupNumber, Status: "submit_pending", Videos: videos,
		jobKey:      fmt.Sprintf("upload:%s:%d", mapping.AdvertiserID, groupNumber),
		projectName: projectName, promotionName: promotionName,
	}
	if !request.Submit {
		group.Status = "planned"
	}
	if err != nil {
		group.Status = "blocked"
		group.blockedError = err.Error()
		group.Error = err.Error()
		return group
	}
	group.MissingFields = append([]string(nil), payloads.MissingFields...)
	group.PreflightSummary = uploadPreflightSummary(payloads)
	group.ProjectPayload = payloads.Project
	group.PromotionPayload = payloads.Promotion
	group.projectJSON, group.promotionJSON, err = payloads.JSON()
	if err != nil {
		group.Status = "blocked"
		group.blockedError = err.Error()
		group.Error = err.Error()
	}
	if len(group.MissingFields) != 0 {
		group.Status = "blocked"
		group.blockedError = "Marketing upload group has unresolved required fields"
	}
	if frozenProjectName != "" && projectName != frozenProjectName {
		group.Status = "blocked"
		group.blockedError = "project name changed after the upload batch was frozen"
	}
	if frozenPromotionName != "" && promotionName != frozenPromotionName {
		group.Status = "blocked"
		group.blockedError = "promotion name changed after the upload batch was frozen"
	}
	if !request.IncludePayloads {
		group.ProjectPayload = nil
		group.PromotionPayload = nil
	}
	return group
}

func uploadGroupNames(
	config map[string]any,
	request UploadBatchRequest,
	groupNumber int,
) (string, string, int, string) {
	suffixIndex := request.StartIndex + groupNumber - 1
	suffix := fmt.Sprintf("%02d", suffixIndex)
	productName := request.ProductName
	if productName == "" {
		productName = textValue(configuration.Value(config, "defaults.product_name"))
	}
	values := map[string]string{
		"material_date": request.MaterialDate, "product_name": productName,
		"group_index": strconv.Itoa(suffixIndex), "index": strconv.Itoa(suffixIndex),
		"suffix": suffix,
	}
	projectTemplate := textValue(configuration.Value(config, "defaults.project_name_template"))
	promotionTemplate := textValue(configuration.Value(config, "defaults.promotion_name_template"))
	projectName := renderMarketingName(projectTemplate, values)
	promotionName := renderMarketingName(promotionTemplate, values)
	if !containsUploadGroupToken(projectTemplate) {
		projectName += "_" + suffix
	}
	if !containsUploadGroupToken(promotionTemplate) {
		promotionName += "_" + suffix
	}
	return projectName, promotionName, suffixIndex, suffix
}

func uploadPreflightSummary(payloads Payloads) map[string]any {
	delivery := configuration.Object(payloads.Project["delivery_setting"])
	materials := configuration.Object(payloads.Promotion["promotion_materials"])
	result := map[string]any{
		"advertiser_id": payloads.Project["advertiser_id"],
		"project_name":  payloads.Project["name"], "promotion_name": payloads.Promotion["name"],
		"budget": delivery["budget"], "cpa_bid": delivery["cpa_bid"],
		"city_count":  len(anySlice(configuration.Value(payloads.Project, "audience.city"))),
		"video_count": len(anySlice(materials["video_material_list"])),
		"operation":   payloads.Project["operation"],
	}
	if !configuration.Missing(delivery["roi_goal"]) {
		result["roi_goal"] = delivery["roi_goal"]
	}
	return result
}

func applyUploadBatchRows(
	works []uploadAccountWork,
	rows []BatchRow,
	includePayloads bool,
) {
	byKey := make(map[string]BatchRow, len(rows))
	for _, row := range rows {
		byKey[row.Key] = row
	}
	for workIndex := range works {
		for groupIndex := range works[workIndex].groups {
			group := &works[workIndex].groups[groupIndex]
			row, exists := byKey[group.jobKey]
			if !exists {
				continue
			}
			group.PlannedOperation = row.PlannedAction
			group.ProjectID = row.ProjectID
			group.PromotionID = row.PromotionID
			group.FailureStage = row.FailureStage
			group.LastResponse = row.LastResponse
			group.Reconciliation = row.Reconciliation
			group.Error = row.Error
			switch row.Status {
			case "completed", "skipped_completed":
				group.Status = "created"
			case "planned":
				group.Status = "planned"
			default:
				group.Status = row.Status
			}
			if !includePayloads {
				group.ProjectPayload = nil
				group.PromotionPayload = nil
			}
		}
		works[workIndex].account.Groups = cloneUploadGroups(works[workIndex].groups, includePayloads)
	}
}

func finalizeUploadAccount(
	account *UploadBatchAccount,
	groups []UploadBatchGroup,
	submit bool,
) {
	if len(groups) == 0 {
		return
	}
	account.Groups = cloneUploadGroups(groups, true)
	account.GroupCount = len(groups)
	account.CreatedProjectCount = 0
	account.CreatedPromotionCount = 0
	account.BlockedGroupCount = 0
	account.FailedGroupCount = 0
	for _, group := range groups {
		if group.ProjectID != "" {
			account.CreatedProjectCount++
		}
		if group.PromotionID != "" {
			account.CreatedPromotionCount++
		}
		switch group.Status {
		case "blocked", "ambiguous":
			account.BlockedGroupCount++
		case "failed", "project_failed", "promotion_failed":
			account.FailedGroupCount++
		}
	}
	if submit {
		account.Status = "completed"
		if account.FailedGroupCount != 0 || account.BlockedGroupCount != 0 {
			account.Status = "completed_with_errors"
		}
	} else {
		account.Status = "planned"
		if account.BlockedGroupCount != 0 {
			account.Status = "planned_with_blocks"
		}
	}
}

func uploadMetadata(
	scopeFingerprint string,
	mappings []UploadAccountTemplate,
	works []uploadAccountWork,
) uploadJournalMetadata {
	accounts := make([]uploadFrozenAccount, len(works))
	for index := range works {
		if works[index].frozen.AdvertiserID != "" {
			accounts[index] = works[index].frozen
		} else {
			accounts[index] = freezeUploadAccount(works[index])
		}
	}
	return uploadJournalMetadata{
		SchemaVersion: UploadBatchJournalVersion, Kind: UploadBatchJournalKind,
		ScopeFingerprint: scopeFingerprint, AccountTemplates: append([]UploadAccountTemplate(nil), mappings...),
		Accounts: accounts,
	}
}

func freezeUploadAccount(work uploadAccountWork) uploadFrozenAccount {
	groups := make([]uploadFrozenGroup, 0, len(work.groups))
	for _, group := range work.groups {
		groups = append(groups, uploadFrozenGroup{
			GroupIndex: group.GroupIndex, JobKey: group.jobKey,
			Videos: cloneUploadVideos(group.Videos), ProjectName: group.projectName,
			PromotionName: group.promotionName,
		})
	}
	base := cloneUploadAccount(work.account)
	base.Groups = nil
	return uploadFrozenAccount{
		AdvertiserID: work.mapping.AdvertiserID, PlanTemplate: work.mapping.PlanTemplate,
		ResolvedConfigFingerprint: work.resolvedFingerprint, Base: base, Groups: groups,
	}
}

func decodeUploadMetadata(journal domainplans.Journal) (uploadJournalMetadata, error) {
	kind := ""
	if payload := journal.Extra["batch_kind"]; len(payload) != 0 {
		_ = json.Unmarshal(payload, &kind)
	}
	if kind != UploadBatchJournalKind {
		return uploadJournalMetadata{}, errors.New("operation journal is not a Marketing upload batch")
	}
	payload := journal.Extra["upload_batch"]
	if len(payload) == 0 {
		return uploadJournalMetadata{}, errors.New("Marketing upload batch journal is missing frozen metadata")
	}
	var metadata uploadJournalMetadata
	if err := json.Unmarshal(payload, &metadata); err != nil {
		return uploadJournalMetadata{}, fmt.Errorf("decode frozen Marketing upload batch: %w", err)
	}
	if metadata.SchemaVersion != UploadBatchJournalVersion || metadata.Kind != UploadBatchJournalKind ||
		len(metadata.ScopeFingerprint) != 64 || len(metadata.AccountTemplates) == 0 || len(metadata.Accounts) == 0 {
		return uploadJournalMetadata{}, errors.New("Marketing upload batch journal has an invalid frozen schema")
	}
	transactionFingerprint, err := uploadFingerprint(metadata)
	if err != nil {
		return uploadJournalMetadata{}, err
	}
	if transactionFingerprint != journal.Fingerprint {
		return uploadJournalMetadata{}, errors.New("Marketing upload batch frozen metadata does not match the journal fingerprint")
	}
	if len(metadata.AccountTemplates) != len(metadata.Accounts) {
		return uploadJournalMetadata{}, errors.New("Marketing upload batch journal account metadata is incomplete")
	}
	for index, mapping := range metadata.AccountTemplates {
		account := metadata.Accounts[index]
		if mapping.AdvertiserID != account.AdvertiserID || mapping.PlanTemplate != account.PlanTemplate {
			return uploadJournalMetadata{}, errors.New("Marketing upload batch journal account order is inconsistent")
		}
		for _, group := range account.Groups {
			if group.JobKey == "" || len(group.Videos) == 0 {
				return uploadJournalMetadata{}, errors.New("Marketing upload batch journal contains an invalid frozen group")
			}
			if _, exists := journal.Jobs[group.JobKey]; !exists {
				return uploadJournalMetadata{}, fmt.Errorf("Marketing upload batch journal is missing frozen job %q", group.JobKey)
			}
		}
	}
	return metadata, nil
}

func uploadJournalCompleted(journal domainplans.Journal) bool {
	if len(journal.Jobs) == 0 {
		return true
	}
	for _, job := range journal.Jobs {
		action, _, _, _ := batchAction(job)
		if action != "skip_completed" {
			return false
		}
	}
	return true
}

func (service UploadBatchService) saveUploadJournal(
	ctx context.Context,
	runID string,
	transactionFingerprint string,
	metadata uploadJournalMetadata,
	works []uploadAccountWork,
	existing *domainplans.Journal,
) (domainplans.Journal, error) {
	metadataPayload, err := json.Marshal(metadata)
	if err != nil {
		return domainplans.Journal{}, fmt.Errorf("encode Marketing upload batch journal metadata: %w", err)
	}
	jobs := map[string]domainplans.JournalJob{}
	createdAt := service.now()
	if existing != nil {
		createdAt, _ = time.Parse(time.RFC3339Nano, existing.CreatedAt)
		for key, value := range existing.Jobs {
			jobs[key] = value
		}
	}
	for _, work := range works {
		sentinel := uploadAccountSentinelKey(work.mapping.AdvertiserID)
		if len(work.frozen.Groups) == 0 {
			if _, exists := jobs[sentinel]; !exists {
				jobs[sentinel] = domainplans.JournalJob{
					Status: "account_pending", AdvertiserID: work.mapping.AdvertiserID,
					Extra: map[string]json.RawMessage{
						"plan_template": mustRawJSON(work.mapping.PlanTemplate),
					},
				}
			}
			continue
		}
		delete(jobs, sentinel)
		groupsByKey := map[string]UploadBatchGroup{}
		for _, group := range work.groups {
			groupsByKey[group.jobKey] = group
		}
		for _, group := range work.frozen.Groups {
			if _, exists := jobs[group.JobKey]; exists {
				continue
			}
			current := groupsByKey[group.JobKey]
			jobs[group.JobKey] = domainplans.JournalJob{
				Status: "pending", AdvertiserID: work.mapping.AdvertiserID,
				Extra: uploadGroupJournalExtra(work.mapping.PlanTemplate, current),
			}
		}
	}
	journal, err := domainplans.NewJournal(transactionFingerprint, jobs, createdAt)
	if err != nil {
		return domainplans.Journal{}, err
	}
	if existing != nil {
		journal.Extra = cloneRawMessages(existing.Extra)
	}
	if journal.Extra == nil {
		journal.Extra = map[string]json.RawMessage{}
	}
	journal.Extra["batch_kind"] = json.RawMessage(`"marketing_upload"`)
	journal.Extra["scope_fingerprint"] = mustRawJSON(metadata.ScopeFingerprint)
	journal.Extra["upload_batch"] = metadataPayload
	if err := service.Journals.Save(ctx, runID, journal); err != nil {
		return domainplans.Journal{}, fmt.Errorf("save Marketing upload batch journal: %w", err)
	}
	return journal, nil
}

func blockedFrozenUploadWork(
	work uploadAccountWork,
	frozen uploadFrozenAccount,
	message string,
) uploadAccountWork {
	work.groups = make([]UploadBatchGroup, 0, len(frozen.Groups))
	for _, group := range frozen.Groups {
		work.groups = append(work.groups, UploadBatchGroup{
			GroupIndex: group.GroupIndex, Status: "blocked", Videos: cloneUploadVideos(group.Videos),
			MissingFields: []string{}, Error: message, blockedError: message,
			jobKey: group.JobKey, projectName: group.ProjectName, promotionName: group.PromotionName,
		})
	}
	work.account.Status = "completed_with_errors"
	work.account.Error = message
	work.account.Groups = cloneUploadGroups(work.groups, false)
	return work
}

func newUploadAccount(
	mapping UploadAccountTemplate,
	effective map[string]any,
) UploadBatchAccount {
	return UploadBatchAccount{
		AdvertiserID: mapping.AdvertiserID,
		PlanTemplate: configuration.Clone(effective["_selected_plan_template"]),
		Status:       "running", Groups: []UploadBatchGroup{},
		SkippedVideos: []UploadSkippedVideo{}, AdGetRequestIDs: []string{},
	}
}

func compactUploadVideos(rows []domainmarketing.VideoAsset) ([]UploadBatchVideo, []UploadSkippedVideo) {
	videos := []UploadBatchVideo{}
	skipped := []UploadSkippedVideo{}
	seen := map[string]struct{}{}
	for _, row := range rows {
		videoID := strings.TrimSpace(row.ID)
		if videoID == "" {
			skipped = append(skipped, UploadSkippedVideo{
				Reason: "missing_video_id", MaterialID: row.MaterialID, Filename: row.Filename,
			})
			continue
		}
		if _, exists := seen[videoID]; exists {
			skipped = append(skipped, UploadSkippedVideo{
				Reason: "duplicate_video_id", VideoID: videoID,
				MaterialID: row.MaterialID, Filename: row.Filename,
			})
			continue
		}
		seen[videoID] = struct{}{}
		videos = append(videos, uploadVideoFromAsset(row))
	}
	return videos, skipped
}

func uploadVideoFromAsset(row domainmarketing.VideoAsset) UploadBatchVideo {
	return UploadBatchVideo{
		VideoID: row.ID, MaterialID: row.MaterialID, Filename: row.Filename,
		CreateTime: row.CreateTime, Width: row.Width, Height: row.Height,
		Duration: row.Duration, Format: row.Format, Source: row.Source,
		Signature: row.Signature, PosterURL: row.PosterURL,
	}
}

func mergeUploadVideo(original UploadBatchVideo, row domainmarketing.VideoAsset) UploadBatchVideo {
	current := uploadVideoFromAsset(row)
	if current.MaterialID == "" {
		current.MaterialID = original.MaterialID
	}
	if current.Filename == "" {
		current.Filename = original.Filename
	}
	if current.CreateTime == "" {
		current.CreateTime = original.CreateTime
	}
	return current
}

func flattenFrozenUploadVideos(groups []uploadFrozenGroup) []UploadBatchVideo {
	result := []UploadBatchVideo{}
	seen := map[string]struct{}{}
	for _, group := range groups {
		for _, video := range group.Videos {
			if _, exists := seen[video.VideoID]; exists {
				continue
			}
			seen[video.VideoID] = struct{}{}
			result = append(result, video)
		}
	}
	return result
}

func uploadResolvedConfigFingerprint(config map[string]any) (string, error) {
	value := configuration.CloneMap(config)
	clearRuntimeVideos(value)
	delete(value, "api")
	delete(value, "oauth")
	delete(value, "channels")
	return uploadFingerprint(value)
}

func uploadScopeFingerprint(scope uploadBatchScope) (string, error) {
	scope.Filename = strings.ToLower(scope.Filename)
	scope.Budget = canonicalBatchNumber(scope.Budget)
	scope.CPABid = canonicalBatchNumber(scope.CPABid)
	scope.ROIGoal = canonicalBatchNumber(scope.ROIGoal)
	return uploadFingerprint(scope)
}

func uploadFingerprint(value any) (string, error) {
	buffer := new(bytes.Buffer)
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", fmt.Errorf("encode Marketing upload batch fingerprint: %w", err)
	}
	digest := sha256.Sum256(bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}))
	return hex.EncodeToString(digest[:]), nil
}

func (service UploadBatchService) newRunID(scope string, now time.Time) (string, error) {
	if service.NewRunID != nil {
		return service.NewRunID(scope, now)
	}
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate Marketing upload batch run ID: %w", err)
	}
	runID := UploadBatchRunPrefix + now.UTC().Format("20060102t150405") + "-" + hex.EncodeToString(random)
	if err := domainplans.ValidateJournalID(runID); err != nil {
		return "", err
	}
	return runID, nil
}

func (service UploadBatchService) now() time.Time {
	if service.Now != nil {
		return service.Now()
	}
	return time.Now()
}

func uploadSettings(request UploadBatchRequest) UploadBatchSettings {
	return UploadBatchSettings{
		Date: request.Date, MaterialDate: request.MaterialDate,
		VideosPerUnit: request.VideosPerUnit, Budget: request.Budget,
		Bid: request.CPABid, ROIGoal: request.ROIGoal,
		AccountConcurrency: request.AccountConcurrency,
		GroupConcurrency:   request.GroupConcurrency,
		CoverConcurrency:   request.CoverConcurrency, CoverAttempts: request.CoverAttempts,
		CoverWaitSeconds: request.CoverWait.Seconds(), ValidateAdGet: request.ValidateAdGet,
		SkipMissingCover: request.SkipMissingCover,
	}
}

func uploadTotals(accounts []UploadBatchAccount) UploadBatchTotals {
	result := UploadBatchTotals{AccountCount: len(accounts)}
	for _, account := range accounts {
		result.QualifiedVideoCount += account.CoverReadyVideoCount
		result.SkippedVideoCount += len(account.SkippedVideos)
		result.GroupCount += account.GroupCount
		result.CreatedProjectCount += account.CreatedProjectCount
		result.CreatedPromotionCount += account.CreatedPromotionCount
		result.FailedGroupCount += account.FailedGroupCount
		result.BlockedGroupCount += account.BlockedGroupCount
	}
	return result
}

func uploadExitCode(accounts []UploadBatchAccount) int {
	for _, account := range accounts {
		switch account.Status {
		case "blocked", "query_failed", "failed", "completed_with_errors", "planned_with_blocks", "no_qualified_videos":
			return 1
		}
	}
	return 0
}

func uploadGroupJournalExtra(template string, group UploadBatchGroup) map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"plan_template": mustRawJSON(template), "group_index": mustRawJSON(group.GroupIndex),
		"video_ids":    mustRawJSON(uploadVideoIDs(group.Videos)),
		"project_name": mustRawJSON(group.projectName), "promotion_name": mustRawJSON(group.promotionName),
	}
}

func uploadAccountSentinelKey(advertiserID string) string {
	return "upload-account:" + advertiserID
}

func uploadVideoIDs(videos []UploadBatchVideo) []string {
	result := make([]string, len(videos))
	for index := range videos {
		result[index] = videos[index].VideoID
	}
	return result
}

func parseUploadTemplateMappings(values []string) (map[string]string, error) {
	result := map[string]string{}
	for _, raw := range values {
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 {
			return nil, errors.New("account template mapping must use ADVERTISER_ID=TEMPLATE_NAME")
		}
		advertiserID, templateName := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if advertiserID == "" || templateName == "" {
			return nil, errors.New("account template mapping must include both advertiser ID and template name")
		}
		if existing, exists := result[advertiserID]; exists && existing != templateName {
			return nil, fmt.Errorf("advertiser %s has multiple template mappings", advertiserID)
		}
		result[advertiserID] = templateName
	}
	return result, nil
}

func resolveUploadDate(value string, now time.Time) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "today":
		return now.Format("2006-01-02"), nil
	case "yesterday":
		return now.AddDate(0, 0, -1).Format("2006-01-02"), nil
	default:
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil {
			return "", errors.New("date must be today, yesterday, or yyyy-mm-dd")
		}
		return parsed.Format("2006-01-02"), nil
	}
}

func splitUploadCSV(values []string) []string {
	result := []string{}
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				result = append(result, item)
			}
		}
	}
	return uniqueUploadStrings(result)
}

func uniqueUploadStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func containsUploadString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsUploadGroupToken(value string) bool {
	return strings.Contains(value, "{group_index}") || strings.Contains(value, "{index}") || strings.Contains(value, "{suffix}")
}

func cloneUploadVideos(values []UploadBatchVideo) []UploadBatchVideo {
	return append([]UploadBatchVideo(nil), values...)
}

func cloneSkippedVideos(values []UploadSkippedVideo) []UploadSkippedVideo {
	return append([]UploadSkippedVideo(nil), values...)
}

func cloneUploadGroups(values []UploadBatchGroup, includePayloads bool) []UploadBatchGroup {
	result := make([]UploadBatchGroup, len(values))
	for index, value := range values {
		result[index] = value
		result[index].Videos = cloneUploadVideos(value.Videos)
		result[index].MissingFields = append([]string(nil), value.MissingFields...)
		result[index].PreflightSummary = configuration.CloneMap(value.PreflightSummary)
		if value.ProjectPayload != nil {
			result[index].ProjectPayload = configuration.CloneMap(value.ProjectPayload)
		}
		if value.PromotionPayload != nil {
			result[index].PromotionPayload = configuration.CloneMap(value.PromotionPayload)
		}
		if !includePayloads {
			result[index].ProjectPayload = nil
			result[index].PromotionPayload = nil
		}
	}
	return result
}

func cloneUploadAccount(value UploadBatchAccount) UploadBatchAccount {
	payload, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var result UploadBatchAccount
	if err := json.Unmarshal(payload, &result); err != nil {
		return value
	}
	return result
}

func mustRawJSON(value any) json.RawMessage {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return payload
}
