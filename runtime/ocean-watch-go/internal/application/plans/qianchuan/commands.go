package qianchuan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	applicationworkmetadata "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/workmetadata"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	domaintemplates "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/templates"
)

const DefaultBatchConcurrency = 8

var qianchuanMarkdownLinkPattern = regexp.MustCompile(`(?i)\]\((https://[^)[:space:]]+)\)`)
var batchPreflightIDPattern = regexp.MustCompile(`^qianchuan-preflight-[0-9]{8}t[0-9]{6}-[0-9a-f]{12}$`)

type batchWorkEntry struct {
	URL      string
	PlanType string
	Business string
}

type CommandConfigReader interface {
	Read(context.Context) (map[string]any, error)
}

type WorkLinkResolver interface {
	Resolve(context.Context, applicationworkmetadata.ResolveRequest) (applicationworkmetadata.ResolveResult, error)
}

type CreatePlanCommand struct {
	ConfigPath    string
	Payload       json.RawMessage
	PlanTemplate  string
	LiveTemplate  string
	Name          string
	AdvertiserID  string
	AuthAccountID string
	Submit        bool
}

type CreateTemplateSummary struct {
	TemplateID       string `json:"template_id"`
	Name             string `json:"name"`
	ProductName      string `json:"product_name,omitempty"`
	ProductShortName string `json:"product_short_name,omitempty"`
	CreatorName      string `json:"creator_name,omitempty"`
	TemplateType     string `json:"template_type"`
}

type CreatePreflight struct {
	AdvertiserID  string `json:"advertiser_id"`
	MarketingGoal string `json:"marketing_goal"`
	Name          string `json:"name,omitempty"`
	AwemeID       string `json:"aweme_id,omitempty"`
	ProductCount  int    `json:"product_count"`
	Budget        any    `json:"budget,omitempty"`
	SmartBidType  string `json:"smart_bid_type,omitempty"`
	ROI2Goal      any    `json:"roi2_goal,omitempty"`
	VideoCount    int    `json:"video_count"`
	ImageCount    int    `json:"image_count"`
	CarouselCount int    `json:"carousel_count"`
}

type CreateCommandResult struct {
	Mode           string                        `json:"mode"`
	Channel        string                        `json:"channel"`
	Config         string                        `json:"config"`
	PlanTemplate   *CreateTemplateSummary        `json:"plan_template"`
	Preflight      CreatePreflight               `json:"preflight"`
	BlockingFields []string                      `json:"blocking_fields"`
	Endpoint       string                        `json:"endpoint"`
	Payload        map[string]any                `json:"payload"`
	Status         string                        `json:"status"`
	AdID           string                        `json:"ad_id,omitempty"`
	RequestID      string                        `json:"request_id,omitempty"`
	FailureStage   string                        `json:"failure_stage,omitempty"`
	DispatchState  domainplans.DispatchState     `json:"dispatch_state,omitempty"`
	Reconciliation *domainplans.Reconciliation   `json:"reconciliation,omitempty"`
	LastResponse   *domainplans.OfficialResponse `json:"last_response,omitempty"`
	SubmitBlocked  bool                          `json:"submit_blocked,omitempty"`
	ExitCode       int                           `json:"exit_code"`
}

type BatchWorksCommand struct {
	PreflightID     string
	PlanTemplate    string
	WorkURLs        []string
	Concurrency     int
	AuthAccountID   string
	IncludePayloads bool
	PlanType        string
	Business        string
	Submit          bool
}

type PreflightStageError struct {
	Stage string
	cause error
}

func (err *PreflightStageError) Error() string {
	return "Qianchuan preflight stage failed: " + err.Stage
}

func (err *PreflightStageError) Unwrap() error {
	return err.cause
}

func preflightStageError(stage string, err error) error {
	if err == nil {
		return nil
	}
	return &PreflightStageError{Stage: stage, cause: err}
}

type OwnerHintCachePerformance struct {
	OwnerHintSummary
	Loaded                 int `json:"loaded"`
	LoadedFromCache        int `json:"loaded_from_cache"`
	LoadedFromLinkMetadata int `json:"loaded_from_link_metadata"`
	Stored                 int `json:"stored"`
	Warning                any `json:"warning"`
}

type LinkMetadataPerformance struct {
	Provider string `json:"provider"`
	Enabled  bool   `json:"enabled"`
}

type BatchPerformance struct {
	LinkResolutionSeconds       float64                   `json:"link_resolution_seconds"`
	CredentialResolutionSeconds float64                   `json:"credential_resolution_seconds"`
	MaterialResolutionSeconds   float64                   `json:"material_resolution_seconds"`
	PlanReconciliationSeconds   float64                   `json:"plan_reconciliation_seconds"`
	TotalSeconds                float64                   `json:"total_seconds"`
	OwnerHintCache              OwnerHintCachePerformance `json:"owner_hint_cache"`
	LinkMetadata                LinkMetadataPerformance   `json:"link_metadata"`
}

type BatchCommandResult struct {
	BatchResult
	Performance BatchPerformance `json:"performance"`
	PreflightID string           `json:"preflight_id,omitempty"`
	ExpiresAt   string           `json:"expires_at,omitempty"`
}

type RemoveWorksCommand struct {
	AdvertiserID  string
	AuthAccountID string
	AdID          string
	WorkURLs      []string
	Concurrency   int
	Submit        bool
	ConfirmDelete bool
}

type CommandService struct {
	Config         CommandConfigReader
	Tokens         authapplication.TokenProvider
	Links          WorkLinkResolver
	OwnerHints     OwnerHintCache
	Verifier       WorkVerifier
	Create         CreateExecutor
	Batch          BatchService
	Remove         RemoveExecutor
	Locks          sharedplans.AdvertiserLocker
	Journals       sharedplans.JournalStore
	NewPreflightID func(string, time.Time) (string, error)
	Now            func() time.Time
}

func (service CommandService) CreatePlan(
	ctx context.Context,
	command CreatePlanCommand,
) (CreateCommandResult, error) {
	payload, summary, blocking, err := service.createPayload(ctx, command)
	if err != nil {
		return CreateCommandResult{}, err
	}
	advertiserID, err := commandAdvertiserID(payload, command.AdvertiserID)
	if err != nil {
		return CreateCommandResult{}, err
	}
	request := CreateRequest{
		AdvertiserID: advertiserID, AuthAccountID: strings.TrimSpace(command.AuthAccountID),
		Submit: command.Submit && len(blocking) == 0, Payload: payload,
	}
	executed, err := service.Create.Execute(ctx, request)
	if err != nil && strings.TrimSpace(executed.Mode) == "" {
		return CreateCommandResult{}, err
	}
	result := createCommandResult(executed, summary, blocking, command.ConfigPath)
	if err != nil {
		result.ExitCode = 1
		return result, err
	}
	if command.Submit && len(blocking) != 0 {
		result.Mode, result.Status, result.SubmitBlocked, result.ExitCode = "submit", "blocked", true, 1
	}
	return result, nil
}

func (service CommandService) BatchWorks(
	ctx context.Context,
	command BatchWorksCommand,
) (BatchCommandResult, error) {
	started := service.now()
	command.PreflightID = strings.TrimSpace(command.PreflightID)
	if command.PreflightID != "" {
		return service.submitBatchPreflight(ctx, command, started)
	}
	if service.Config == nil {
		return BatchCommandResult{}, errors.New("Qianchuan batch command dependencies are incomplete")
	}
	concurrency := command.Concurrency
	if concurrency == 0 {
		concurrency = DefaultBatchConcurrency
	}
	if concurrency < 1 || concurrency > applicationworkmetadata.MaxConcurrency {
		return BatchCommandResult{}, errors.New("concurrency must be between 1 and 10")
	}
	if len(command.WorkURLs) == 0 {
		return BatchCommandResult{}, errors.New("at least one work URL is required")
	}
	readPool, err := NewReadPool(concurrency)
	if err != nil {
		return BatchCommandResult{}, err
	}
	entries := parseBatchWorkEntries(command.WorkURLs, command.PlanType, command.Business)
	config, err := service.Config.Read(ctx)
	if err != nil {
		return BatchCommandResult{}, preflightStageError("configuration", err)
	}
	exported, err := domaintemplates.ExportQianchuanPlanPayload(
		config, domaintemplates.QianchuanTemplateProduct, command.PlanTemplate, "",
	)
	if err != nil {
		return BatchCommandResult{}, preflightStageError("template", err)
	}
	if !exported.Active {
		return BatchCommandResult{}, errors.New("Qianchuan product template is not active")
	}
	metadata := LinkMetadataPerformance{Provider: "f2_cli", Enabled: true}
	linkResolver := service.Links
	if linkResolver == nil {
		return BatchCommandResult{}, errors.New("Qianchuan work-link resolver is required")
	}
	cachePerformance := OwnerHintCachePerformance{}
	baseRequest := BatchRequest{
		AdvertiserID: exported.AdvertiserID, AuthAccountID: strings.TrimSpace(command.AuthAccountID),
		Submit: command.Submit, TemplateID: exported.TemplateID, TemplateName: exported.DisplayName,
		ProductName: exported.ProductName, ProductShortName: exported.ProductShortName,
		TemplatePayload:  exported.Payload,
		PlanNameTemplate: exported.PlanNameTemplate,
		PlanType:         strings.TrimSpace(command.PlanType), Business: strings.TrimSpace(command.Business),
		IncludePayloads: command.IncludePayloads, ReadPool: readPool,
	}
	type linkOutcome struct {
		result   applicationworkmetadata.ResolveResult
		err      error
		finished time.Time
	}
	type leaseOutcome struct {
		lease    authapplication.TokenLease
		ctx      context.Context
		err      error
		finished time.Time
	}
	linkChannel := make(chan linkOutcome, 1)
	leaseChannel := make(chan leaseOutcome, 1)
	parallelContext, cancelParallel := context.WithCancel(ctx)
	defer cancelParallel()
	go func() {
		resolved, resolveErr := linkResolver.Resolve(parallelContext, applicationworkmetadata.ResolveRequest{
			URLs: batchWorkEntryURLs(entries), Concurrency: concurrency,
		})
		linkChannel <- linkOutcome{result: resolved, err: resolveErr, finished: service.now()}
	}()
	go func() {
		lease, scopedContext, leaseErr := service.readLease(parallelContext, exported.AdvertiserID, command.AuthAccountID)
		leaseChannel <- leaseOutcome{lease: lease, ctx: scopedContext, err: leaseErr, finished: service.now()}
	}()
	linksFinished := started
	linksReady := false
	linksResult := linkOutcome{}
	leaseResult := leaseOutcome{}
	select {
	case linksResult = <-linkChannel:
		linksReady = true
		linksFinished = linksResult.finished
		if linksResult.err != nil {
			cancelParallel()
			return BatchCommandResult{}, preflightStageError("work_metadata", linksResult.err)
		}
		baseRequest.Skipped = qianchuanSkippedLinks(linksResult.result.Skipped)
		if len(linksResult.result.Resolved) == 0 {
			cancelParallel()
			result, executeErr := service.Batch.Execute(ctx, baseRequest)
			finished := service.now()
			return BatchCommandResult{
				BatchResult: result,
				Performance: batchPerformance(
					started, linksFinished, linksFinished, linksFinished, finished, metadata, cachePerformance,
				),
			}, executeErr
		}
		leaseResult = <-leaseChannel
	case leaseResult = <-leaseChannel:
	}
	if leaseResult.err != nil {
		cancelParallel()
		return BatchCommandResult{}, preflightStageError("authorization", leaseResult.err)
	}
	lease, scopedContext := leaseResult.lease, leaseResult.ctx
	credentialsFinished := leaseResult.finished
	if service.Locks == nil {
		return BatchCommandResult{}, errors.New("Qianchuan batch advertiser lock is required")
	}
	var result BatchResult
	var executeErr error
	materialsFinished := credentialsFinished
	scope := domainplans.WriteScope{
		Channel: domainplans.ChannelQianchuan, AdvertiserID: exported.AdvertiserID,
		LockFamily: domainplans.LockQianchuanWorks,
	}
	err = sharedplans.WithAdvertiserLock(scopedContext, service.Locks, scope, func(lockedContext context.Context) error {
		inventoryChannel := make(chan struct {
			inventory CurrentPlanInventory
			err       error
		}, 1)
		scanner, canScan := service.Batch.Reconciler.(CurrentPlanScanner)
		if canScan {
			go func() {
				inventory, scanErr := scanner.ScanCurrentPlans(lockedContext, CurrentPlanScanRequest{
					AdvertiserID: exported.AdvertiserID, AccessToken: lease.AccessToken, ReadPool: readPool,
				})
				inventoryChannel <- struct {
					inventory CurrentPlanInventory
					err       error
				}{inventory: inventory, err: scanErr}
			}()
		}
		if !linksReady {
			linksResult = <-linkChannel
			linksFinished = linksResult.finished
		}
		if linksResult.err != nil {
			cancelParallel()
			if canScan {
				<-inventoryChannel
			}
			return preflightStageError("work_metadata", linksResult.err)
		}
		links := linksResult.result
		baseRequest.Skipped = qianchuanSkippedLinks(links.Skipped)
		if len(links.Resolved) == 0 {
			if canScan {
				<-inventoryChannel
			}
			result, executeErr = service.Batch.Execute(lockedContext, baseRequest)
			return executeErr
		}
		cachedHints := map[string]OwnerHint{}
		if service.OwnerHints != nil {
			cachedHints, err = service.OwnerHints.Load(lockedContext, exported.AdvertiserID, resolvedWorkIDs(links.Resolved))
			if err != nil {
				cachedHints = map[string]OwnerHint{}
				cachePerformance.Warning = ownerHintCacheWarning("owner_hint_cache_read_failed", err)
			}
		}
		linkHints := ownerHintsFromResolvedLinks(links.Resolved)
		ownerHints := mergeOwnerHints(cachedHints, linkHints)
		cachePerformance.Loaded = len(ownerHints)
		cachePerformance.LoadedFromCache = len(cachedHints)
		cachePerformance.LoadedFromLinkMetadata = len(linkHints)
		verification, verifyErr := service.Verifier.Verify(lockedContext, WorkVerificationRequest{
			AdvertiserID: exported.AdvertiserID, AccessToken: lease.AccessToken,
			ProductIDs: append([]string(nil), exported.ProductIDs...), Works: qianchuanWorkInputs(links.Resolved, ownerHints, entries),
			ReadPool: readPool,
		})
		if verifyErr != nil {
			cancelParallel()
			if canScan {
				<-inventoryChannel
			}
			return preflightStageError("work_verification", verifyErr)
		}
		if canScan {
			inventoryResult := <-inventoryChannel
			if inventoryResult.err != nil {
				return preflightStageError("plan_inventory", inventoryResult.err)
			}
			baseRequest.PlanInventory = &inventoryResult.inventory
		}
		cachePerformance.OwnerHintSummary = verification.OwnerHintSummary
		if service.OwnerHints != nil {
			stored, storeErr := service.OwnerHints.Store(lockedContext, exported.AdvertiserID, verification.ResolvedOwnerHints)
			if storeErr != nil {
				if cachePerformance.Warning == nil {
					cachePerformance.Warning = ownerHintCacheWarning("owner_hint_cache_write_failed", storeErr)
				}
			} else {
				cachePerformance.Stored = stored
			}
		}
		materialsFinished = service.now()
		baseRequest.ReadAccessToken = lease.AccessToken
		baseRequest.Works = verification.Matched
		baseRequest.Skipped = append(baseRequest.Skipped, verification.Skipped...)
		baseRequest.QueryFailures = verification.QueryFailures
		batch := service.Batch
		if command.Submit {
			batch.Guard.Credentials = commandLeaseCredentials{lease: lease, advertiserID: exported.AdvertiserID}
		}
		result, executeErr = batch.Execute(lockedContext, baseRequest)
		return executeErr
	})
	if err != nil && executeErr == nil {
		var stageError *PreflightStageError
		if errors.As(err, &stageError) {
			return BatchCommandResult{}, err
		}
		return BatchCommandResult{}, preflightStageError("local_coordination", err)
	}
	finished := service.now()
	commandResult := BatchCommandResult{
		BatchResult: result,
		Performance: batchPerformance(started, linksFinished, credentialsFinished, materialsFinished, finished, metadata, cachePerformance),
	}
	if executeErr != nil || command.Submit {
		return commandResult, preflightStageError("plan_reconciliation", executeErr)
	}
	snapshot, eligible, snapshotErr := prepareBatchSnapshot(baseRequest, result, finished)
	if snapshotErr != nil {
		return BatchCommandResult{}, preflightStageError("snapshot", snapshotErr)
	}
	if !eligible {
		return commandResult, nil
	}
	if service.Journals == nil {
		return BatchCommandResult{}, errors.New("Qianchuan batch preflight journal store is required")
	}
	newID := service.NewPreflightID
	if newID == nil {
		newID = newBatchPreflightID
	}
	preflightID, snapshotErr := newID(snapshot.AdvertiserID, finished)
	if snapshotErr != nil {
		return BatchCommandResult{}, preflightStageError("snapshot", snapshotErr)
	}
	journal, snapshotErr := batchPreflightJournal(snapshot, finished)
	if snapshotErr != nil {
		return BatchCommandResult{}, preflightStageError("snapshot", snapshotErr)
	}
	if snapshotErr = service.Journals.Save(ctx, preflightID, journal); snapshotErr != nil {
		return BatchCommandResult{}, preflightStageError("snapshot", snapshotErr)
	}
	commandResult.PreflightID, commandResult.ExpiresAt = preflightID, snapshot.ExpiresAt
	return commandResult, nil
}

func (service CommandService) submitBatchPreflight(
	ctx context.Context,
	command BatchWorksCommand,
	started time.Time,
) (BatchCommandResult, error) {
	if !command.Submit {
		return BatchCommandResult{}, errors.New("preflight_id requires submit")
	}
	if strings.TrimSpace(command.PlanTemplate) != "" || len(command.WorkURLs) != 0 ||
		strings.TrimSpace(command.PlanType) != "" || strings.TrimSpace(command.Business) != "" {
		return BatchCommandResult{}, errors.New("preflight_id cannot be combined with template, work URL, plan type, or business")
	}
	if service.Config == nil || service.Journals == nil {
		return BatchCommandResult{}, errors.New("Qianchuan batch preflight dependencies are incomplete")
	}
	concurrency := command.Concurrency
	if concurrency == 0 {
		concurrency = DefaultBatchConcurrency
	}
	if concurrency < 1 || concurrency > applicationworkmetadata.MaxConcurrency {
		return BatchCommandResult{}, errors.New("concurrency must be between 1 and 10")
	}
	readPool, err := NewReadPool(concurrency)
	if err != nil {
		return BatchCommandResult{}, err
	}
	journal, err := service.Journals.Load(ctx, command.PreflightID)
	if err != nil {
		return BatchCommandResult{}, fmt.Errorf("load Qianchuan batch preflight: %w", err)
	}
	snapshot, err := decodeBatchPreflight(journal, started)
	if err != nil {
		return BatchCommandResult{}, err
	}
	config, err := service.Config.Read(ctx)
	if err != nil {
		return BatchCommandResult{}, err
	}
	exported, err := domaintemplates.ExportQianchuanPlanPayload(
		config, domaintemplates.QianchuanTemplateProduct, snapshot.TemplateID, "",
	)
	if err != nil {
		return BatchCommandResult{}, errors.New("Qianchuan batch preflight template changed; run preflight again")
	}
	currentTemplate := BatchRequest{
		AdvertiserID: exported.AdvertiserID, TemplateID: exported.TemplateID,
		TemplateName: exported.DisplayName, ProductName: exported.ProductName,
		ProductShortName: exported.ProductShortName, PlanNameTemplate: exported.PlanNameTemplate,
		TemplatePayload: exported.Payload,
	}
	if !exported.Active || batchTemplateDigest(currentTemplate) != snapshot.TemplateDigest {
		return BatchCommandResult{}, errors.New("Qianchuan batch preflight template changed; run preflight again")
	}
	authAccountID := strings.TrimSpace(command.AuthAccountID)
	if authAccountID == "" {
		authAccountID = snapshot.AuthAccountID
	}
	lease, scopedContext, err := service.readLease(ctx, snapshot.AdvertiserID, authAccountID)
	if err != nil {
		return BatchCommandResult{}, err
	}
	credentialsFinished := service.now()
	if service.Locks == nil {
		return BatchCommandResult{}, errors.New("Qianchuan batch advertiser lock is required")
	}
	scope := domainplans.WriteScope{
		Channel: domainplans.ChannelQianchuan, AdvertiserID: snapshot.AdvertiserID,
		LockFamily: domainplans.LockQianchuanWorks,
	}
	var result BatchResult
	var executeErr error
	err = sharedplans.WithAdvertiserLock(scopedContext, service.Locks, scope, func(lockedContext context.Context) error {
		scanner, ok := service.Batch.Reconciler.(CurrentPlanScanner)
		if !ok {
			return errors.New("Qianchuan batch current-plan scanner is required")
		}
		inventory, scanErr := scanner.ScanCurrentPlans(lockedContext, CurrentPlanScanRequest{
			AdvertiserID: snapshot.AdvertiserID, AccessToken: lease.AccessToken, ReadPool: readPool,
		})
		if scanErr != nil {
			return scanErr
		}
		request := snapshot.batchRequest(authAccountID, readPool)
		request.PlanInventory = &inventory
		batch := service.Batch
		batch.Guard.Credentials = commandLeaseCredentials{lease: lease, advertiserID: snapshot.AdvertiserID}
		result, executeErr = batch.Execute(lockedContext, request)
		return executeErr
	})
	if err != nil && executeErr == nil {
		return BatchCommandResult{}, err
	}
	finished := service.now()
	return BatchCommandResult{
		BatchResult: result,
		Performance: batchPerformance(
			started, started, credentialsFinished, credentialsFinished, finished,
			LinkMetadataPerformance{Provider: "preflight_snapshot", Enabled: false}, OwnerHintCachePerformance{},
		),
		PreflightID: command.PreflightID, ExpiresAt: snapshot.ExpiresAt,
	}, executeErr
}

func (service CommandService) RemoveWorks(
	ctx context.Context,
	command RemoveWorksCommand,
) (RemoveResult, error) {
	if command.Submit && !command.ConfirmDelete {
		return RemoveResult{}, errors.New("Qianchuan material deletion requires explicit confirm-delete")
	}
	if !command.Submit && command.ConfirmDelete {
		return RemoveResult{}, errors.New("confirm-delete is valid only with submit")
	}
	if service.Links == nil {
		return RemoveResult{}, errors.New("Qianchuan material removal link resolver is required")
	}
	concurrency := command.Concurrency
	if concurrency == 0 {
		concurrency = applicationworkmetadata.DefaultConcurrency
	}
	links, err := service.Links.Resolve(ctx, applicationworkmetadata.ResolveRequest{
		URLs: append([]string(nil), command.WorkURLs...), Concurrency: concurrency,
	})
	if err != nil {
		return RemoveResult{}, err
	}
	works := make([]RemoveWork, 0, len(links.Resolved))
	for _, work := range links.Resolved {
		works = append(works, RemoveWork{
			InputIndex: work.InputIndex, InputURL: work.InputURL, AwemeItemID: work.AwemeItemID,
		})
	}
	if len(works) == 0 {
		return RemoveResult{
			Mode: modeFromSubmit(command.Submit), Channel: "qianchuan",
			AdvertiserID: strings.TrimSpace(command.AdvertiserID), AdID: strings.TrimSpace(command.AdID),
			Endpoint: DeleteMaterialsEndpoint, RiskNotice: DeleteRiskNotice,
			Counts:  map[string]int{"input_works": 0, "skipped_links": len(links.Skipped)},
			Results: []RemoveRow{}, SkippedLinks: qianchuanSkippedLinks(links.Skipped), Batches: []RemoveBatch{},
		}, nil
	}
	lease, scopedContext, err := service.readLease(ctx, command.AdvertiserID, command.AuthAccountID)
	if err != nil {
		return RemoveResult{}, err
	}
	executor := service.Remove
	if command.Submit {
		executor.Guard.Credentials = commandLeaseCredentials{
			lease: lease, advertiserID: strings.TrimSpace(command.AdvertiserID),
		}
	}
	if service.Locks == nil {
		return RemoveResult{}, errors.New("Qianchuan material removal advertiser lock is required")
	}
	var result RemoveResult
	scope := domainplans.WriteScope{
		Channel: domainplans.ChannelQianchuan, AdvertiserID: strings.TrimSpace(command.AdvertiserID),
		LockFamily: domainplans.LockQianchuanWorks,
	}
	err = sharedplans.WithAdvertiserLock(scopedContext, service.Locks, scope, func(lockedContext context.Context) error {
		var executeErr error
		result, executeErr = executor.Execute(lockedContext, RemoveCommand{
			AdvertiserID: command.AdvertiserID, AuthAccountID: command.AuthAccountID,
			ReadAccessToken: lease.AccessToken, AdID: command.AdID, Submit: command.Submit,
			ConfirmDelete: command.ConfirmDelete, Works: works, SkippedLinks: qianchuanSkippedLinks(links.Skipped),
		})
		return executeErr
	})
	return result, err
}

func (service CommandService) createPayload(
	ctx context.Context,
	command CreatePlanCommand,
) (json.RawMessage, *CreateTemplateSummary, []string, error) {
	sources := 0
	if len(command.Payload) != 0 {
		sources++
	}
	if strings.TrimSpace(command.PlanTemplate) != "" {
		sources++
	}
	if strings.TrimSpace(command.LiveTemplate) != "" {
		sources++
	}
	if sources != 1 {
		return nil, nil, nil, errors.New("exactly one Qianchuan payload or template source is required")
	}
	if len(command.Payload) != 0 {
		if strings.TrimSpace(command.Name) != "" {
			return nil, nil, nil, errors.New("name is supported only with a product plan template")
		}
		return append(json.RawMessage(nil), command.Payload...), nil, []string{}, nil
	}
	if service.Config == nil {
		return nil, nil, nil, errors.New("Qianchuan plan command config reader is required")
	}
	config, err := service.Config.Read(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	kind, selector := domaintemplates.QianchuanTemplateProduct, command.PlanTemplate
	if strings.TrimSpace(command.LiveTemplate) != "" {
		kind, selector = domaintemplates.QianchuanTemplateLive, command.LiveTemplate
	}
	exported, err := domaintemplates.ExportQianchuanPlanPayload(config, kind, selector, command.Name)
	if err != nil {
		return nil, nil, nil, err
	}
	summary := &CreateTemplateSummary{
		TemplateID: exported.TemplateID, Name: exported.DisplayName,
		ProductName: exported.ProductName, ProductShortName: exported.ProductShortName,
		CreatorName: exported.CreatorName,
	}
	blocking := []string{}
	if kind == domaintemplates.QianchuanTemplateProduct {
		summary.TemplateType = "商品全域"
		blocking = append(blocking, "runtime_creator_materials")
	} else {
		summary.TemplateType = "直播全域"
	}
	if !exported.Active {
		blocking = append(blocking, "template_not_active")
	}
	return exported.Payload, summary, blocking, nil
}

func (service CommandService) readLease(
	ctx context.Context,
	advertiserID string,
	authAccountID string,
) (authapplication.TokenLease, context.Context, error) {
	if service.Tokens == nil {
		return authapplication.TokenLease{}, nil, errors.New("Qianchuan read token provider is required")
	}
	lease, err := service.Tokens.Ensure(ctx, authapplication.TokenQuery{
		Channel: "qianchuan", AdvertiserID: strings.TrimSpace(advertiserID),
		AuthAccountID: strings.TrimSpace(authAccountID),
	})
	if err != nil {
		return authapplication.TokenLease{}, nil, err
	}
	scoped, err := authapplication.WithAdvertiserTokenLease(ctx, lease, strings.TrimSpace(advertiserID))
	if err != nil {
		return authapplication.TokenLease{}, nil, err
	}
	return lease, scoped, nil
}

type commandLeaseCredentials struct {
	lease        authapplication.TokenLease
	advertiserID string
}

func (provider commandLeaseCredentials) AccessToken(
	ctx context.Context,
	channel domainplans.Channel,
	advertiserID string,
	_ string,
) (sharedplans.CredentialLease, error) {
	if ctx == nil || channel != domainplans.ChannelQianchuan ||
		strings.TrimSpace(advertiserID) != provider.advertiserID ||
		provider.lease.Channel != "qianchuan" {
		return sharedplans.CredentialLease{}, errors.New("preloaded Qianchuan token lease does not match write scope")
	}
	if strings.TrimSpace(provider.lease.AuthorizationID) == "" || strings.TrimSpace(provider.lease.AccessToken) == "" {
		return sharedplans.CredentialLease{}, errors.New("preloaded Qianchuan token lease is incomplete")
	}
	return sharedplans.CredentialLease{
		AuthorizationID: provider.lease.AuthorizationID, AccessToken: provider.lease.AccessToken,
	}, nil
}

func commandAdvertiserID(payload json.RawMessage, override string) (string, error) {
	object, err := decodeCreatePayload(payload)
	if err != nil {
		return "", err
	}
	advertiserID := payloadID(object["advertiser_id"])
	if !validPositiveID(advertiserID) {
		return "", errors.New("Qianchuan payload advertiser_id must be a positive decimal ID")
	}
	override = strings.TrimSpace(override)
	if override != "" && override != advertiserID {
		return "", errors.New("Qianchuan payload advertiser_id does not match command advertiser_id")
	}
	return advertiserID, nil
}

func createCommandResult(
	executed CreateResult,
	summary *CreateTemplateSummary,
	blocking []string,
	configPath string,
) CreateCommandResult {
	return CreateCommandResult{
		Mode: executed.Mode, Channel: "qianchuan", Config: strings.TrimSpace(configPath), PlanTemplate: summary,
		Preflight: createPreflight(executed.Payload), BlockingFields: append([]string(nil), blocking...),
		Endpoint: executed.Endpoint, Payload: executed.Payload, Status: executed.Status,
		AdID: executed.AdID, RequestID: executed.RequestID, FailureStage: executed.FailureStage,
		DispatchState: executed.DispatchState, Reconciliation: executed.Reconciliation,
		LastResponse: executed.LastResponse,
	}
}

func createPreflight(payload map[string]any) CreatePreflight {
	delivery, _ := payload["delivery_setting"].(map[string]any)
	preflight := CreatePreflight{
		AdvertiserID:  payloadID(payload["advertiser_id"]),
		MarketingGoal: strings.TrimSpace(fmt.Sprint(payload["marketing_goal"])),
		Name:          strings.TrimSpace(fmt.Sprint(payload["name"])),
		AwemeID:       payloadID(payload["aweme_id"]), Budget: delivery["budget"],
		SmartBidType: strings.TrimSpace(fmt.Sprint(delivery["smart_bid_type"])),
		ROI2Goal:     delivery["roi2_goal"],
	}
	if preflight.Name == "<nil>" {
		preflight.Name = ""
	}
	if preflight.AwemeID == "<nil>" {
		preflight.AwemeID = ""
	}
	if values, ok := payload["product_ids"].([]any); ok {
		preflight.ProductCount = len(values)
	}
	if creatives, ok := payload["multi_product_creative_list"].([]any); ok {
		for _, raw := range creatives {
			creative, _ := raw.(map[string]any)
			preflight.VideoCount += anySliceLength(creative["video_material"])
			preflight.ImageCount += anySliceLength(creative["image_material"])
			preflight.CarouselCount += anySliceLength(creative["carousel_material"])
		}
	}
	return preflight
}

func anySliceLength(value any) int {
	values, _ := value.([]any)
	return len(values)
}

func qianchuanWorkInputs(
	values []domain.ResolvedWorkLink,
	hints map[string]OwnerHint,
	entries []batchWorkEntry,
) []WorkInput {
	result := make([]WorkInput, 0, len(values))
	for _, value := range values {
		var hint *OwnerHint
		if selected, exists := hints[value.AwemeItemID]; exists {
			copy := selected
			hint = &copy
		}
		result = append(result, WorkInput{
			InputIndex: value.InputIndex, InputURL: value.InputURL, AwemeItemID: value.AwemeItemID,
			CreatorName: strings.TrimSpace(value.CreatorName),
			PlanType:    batchWorkEntryAt(entries, value.InputIndex).PlanType,
			Business:    batchWorkEntryAt(entries, value.InputIndex).Business,
			OwnerHint:   hint,
		})
	}
	return result
}

func parseBatchWorkEntries(values []string, planType, business string) []batchWorkEntry {
	result := make([]batchWorkEntry, 0, len(values))
	planType, business = strings.TrimSpace(planType), strings.TrimSpace(business)
	for _, value := range values {
		columns := strings.Split(value, "\t")
		for index := range columns {
			columns[index] = strings.TrimSpace(columns[index])
		}
		entry := batchWorkEntry{URL: columns[0], PlanType: planType, Business: business}
		if match := qianchuanMarkdownLinkPattern.FindStringSubmatch(entry.URL); len(match) == 2 {
			entry.URL = match[1]
		}
		if len(columns) == 2 && columns[1] != "" {
			entry.PlanType = columns[1]
		}
		if len(columns) >= 3 {
			if columns[len(columns)-2] != "" {
				entry.PlanType = columns[len(columns)-2]
			}
			if columns[len(columns)-1] != "" {
				entry.Business = columns[len(columns)-1]
			}
		}
		result = append(result, entry)
	}
	return result
}

func batchWorkEntryURLs(entries []batchWorkEntry) []string {
	result := make([]string, len(entries))
	for index, entry := range entries {
		result[index] = entry.URL
	}
	return result
}

func batchWorkEntryAt(entries []batchWorkEntry, index int) batchWorkEntry {
	if index < 0 || index >= len(entries) {
		return batchWorkEntry{}
	}
	return entries[index]
}

func resolvedWorkIDs(values []domain.ResolvedWorkLink) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.AwemeItemID)
	}
	return result
}

func ownerHintsFromResolvedLinks(values []domain.ResolvedWorkLink) map[string]OwnerHint {
	result := map[string]OwnerHint{}
	for _, value := range values {
		if value.OwnerHint == nil {
			continue
		}
		hint := OwnerHint{
			AwemeID:     strings.TrimSpace(value.OwnerHint.AwemeID),
			AwemeShowID: strings.TrimSpace(value.OwnerHint.AwemeShowID),
		}
		if validPositiveID(hint.AwemeID) {
			result[value.AwemeItemID] = hint
		}
	}
	return result
}

func mergeOwnerHints(cached, metadata map[string]OwnerHint) map[string]OwnerHint {
	result := make(map[string]OwnerHint, len(cached)+len(metadata))
	for itemID, hint := range cached {
		result[itemID] = hint
	}
	for itemID, hint := range metadata {
		result[itemID] = hint
	}
	return result
}

func ownerHintCacheWarning(code string, err error) map[string]string {
	message := strings.TrimSpace(err.Error())
	if len(message) > 256 {
		message = message[:256]
	}
	return map[string]string{"code": code, "message": message}
}

func qianchuanSkippedLinks(values []domain.SkippedWorkLink) []SkippedWork {
	result := make([]SkippedWork, 0, len(values))
	for _, value := range values {
		result = append(result, SkippedWork{
			InputIndex: value.InputIndex, InputURL: value.InputURL, AwemeItemID: value.AwemeItemID,
			Reason: value.Reason, Message: value.Message,
		})
	}
	return result
}

func batchPerformance(
	started time.Time,
	linksFinished time.Time,
	credentialsFinished time.Time,
	materialsFinished time.Time,
	finished time.Time,
	metadata LinkMetadataPerformance,
	cache OwnerHintCachePerformance,
) BatchPerformance {
	return BatchPerformance{
		LinkResolutionSeconds:       durationSeconds(started, linksFinished),
		CredentialResolutionSeconds: durationSeconds(started, credentialsFinished),
		MaterialResolutionSeconds:   durationSeconds(credentialsFinished, materialsFinished),
		PlanReconciliationSeconds:   durationSeconds(materialsFinished, finished),
		TotalSeconds:                durationSeconds(started, finished),
		OwnerHintCache:              cache,
		LinkMetadata:                metadata,
	}
}

func durationSeconds(start, end time.Time) float64 {
	if start.IsZero() || end.Before(start) {
		return 0
	}
	return float64(end.Sub(start).Round(time.Millisecond)) / float64(time.Second)
}

func (service CommandService) now() time.Time {
	if service.Now != nil {
		return service.Now()
	}
	return time.Now()
}

func modeFromSubmit(submit bool) string {
	if submit {
		return "submit"
	}
	return "dry_run"
}
