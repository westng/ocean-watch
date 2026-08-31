package qianchuan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

const (
	BatchMaterialLimit   = 100
	AddMaterialsEndpoint = "/v1.0/qianchuan/uni_promotion/ad/material/add/"
)

var supportedWorkImageModes = map[string]struct{}{
	"VIDEO_LARGE": {}, "VIDEO_VERTICAL": {},
}

type BatchRequest struct {
	AdvertiserID     string
	AuthAccountID    string
	BusinessDate     string
	ReadAccessToken  string
	Submit           bool
	TemplateID       string
	TemplateName     string
	ProductName      string
	ProductShortName string
	PlanNameTemplate string
	PlanType         string
	Business         string
	TemplatePayload  json.RawMessage
	IncludePayloads  bool
	Works            []VerifiedWork
	Skipped          []SkippedWork
	QueryFailures    []WorkQueryFailure
	ReadPool         *ReadPool
	PlanInventory    *CurrentPlanInventory
	PreparedGroups   map[string]PreparedGroup
}

type BatchTemplateSummary struct {
	TemplateID   string   `json:"template_id"`
	Name         string   `json:"name"`
	AdvertiserID string   `json:"advertiser_id"`
	ProductIDs   []string `json:"product_ids"`
}

type BatchWrite struct {
	Endpoint       string                    `json:"endpoint"`
	Status         string                    `json:"status"`
	ItemIDs        []string                  `json:"item_ids"`
	DispatchState  domainplans.DispatchState `json:"dispatch_state,omitempty"`
	RequestID      string                    `json:"request_id,omitempty"`
	Reconciliation string                    `json:"reconciliation,omitempty"`
	Payload        json.RawMessage           `json:"payload,omitempty"`
	Error          string                    `json:"error,omitempty"`
}

type BatchGroupResult struct {
	GroupID          string         `json:"group_id"`
	AwemeID          string         `json:"aweme_id"`
	DouyinID         string         `json:"douyin_id,omitempty"`
	CreatorName      string         `json:"creator_name,omitempty"`
	AdID             string         `json:"ad_id,omitempty"`
	PlanName         string         `json:"plan_name,omitempty"`
	PlanStatus       string         `json:"plan_status,omitempty"`
	CandidatePlans   []ExistingPlan `json:"candidate_plans,omitempty"`
	ProductIDs       []string       `json:"product_ids"`
	PlanType         string         `json:"plan_type"`
	Business         string         `json:"business"`
	InputItemIDs     []string       `json:"input_item_ids"`
	AlreadyPresent   []string       `json:"already_present_item_ids"`
	CompletedItemIDs []string       `json:"completed_item_ids"`
	Status           string         `json:"status"`
	Writes           []BatchWrite   `json:"writes"`
	Error            string         `json:"error,omitempty"`
	ErrorCode        string         `json:"error_code,omitempty"`
	BindingDigest    string         `json:"-"`
}

type BatchResult struct {
	Mode          string               `json:"mode"`
	Channel       string               `json:"channel"`
	BusinessDate  string               `json:"business_date"`
	Template      BatchTemplateSummary `json:"template"`
	Counts        map[string]int       `json:"counts"`
	Results       []BatchGroupResult   `json:"results"`
	Skipped       []SkippedWork        `json:"skipped"`
	QueryFailures []WorkQueryFailure   `json:"query_failures"`
	FailedResults []BatchGroupResult   `json:"failed_results"`
	Presentation  domain.Presentation  `json:"presentation"`
	ExitCode      int                  `json:"exit_code"`
}

type BatchService struct {
	Guard      sharedplans.GuardedExecutor
	Reader     portqianchuan.Reader
	Writer     portqianchuan.Writer
	Reconciler CurrentPlanFinder
	Bindings   PlanBindingWriter
	Now        func() time.Time
}

type BatchTiming struct {
	GroupReconciliationSeconds float64
	MaterialDiffSeconds        float64
}

type batchTimingContextKey struct{}

func WithBatchTiming(ctx context.Context, timing *BatchTiming) context.Context {
	if ctx == nil || timing == nil {
		return ctx
	}
	return context.WithValue(ctx, batchTimingContextKey{}, timing)
}

func batchTimingFrom(ctx context.Context) *BatchTiming {
	if ctx == nil {
		return nil
	}
	timing, _ := ctx.Value(batchTimingContextKey{}).(*BatchTiming)
	return timing
}

type normalizedBatchRequest struct {
	BatchRequest
	payloadObject map[string]any
	productIDs    []string
	groups        []batchGroup
	businessDate  string
}

type batchGroup struct {
	groupID  string
	identity domainqianchuan.PlanGroupIdentity
	creator  domainqianchuan.AuthorizedCreator
	works    []VerifiedWork
}

type materialSnapshot struct {
	rowsByItem map[string]domainqianchuan.PlanMaterial
	activeIDs  map[string]struct{}
}

func (service BatchService) Execute(ctx context.Context, request BatchRequest) (BatchResult, error) {
	normalized, err := normalizeBatchRequest(request)
	if err != nil {
		return BatchResult{}, err
	}
	if strings.TrimSpace(normalized.BusinessDate) != "" {
		normalized.businessDate = strings.TrimSpace(normalized.BusinessDate)
		if err := ValidateBusinessDate(normalized.businessDate); err != nil {
			return BatchResult{}, err
		}
	} else {
		normalized.businessDate, err = ShanghaiBusinessDate(service.now())
		if err != nil {
			return BatchResult{}, err
		}
	}
	if !normalized.Submit {
		if len(normalized.groups) == 0 {
			return normalized.emptyResult("dry_run"), nil
		}
		if service.Reader == nil || service.Reconciler == nil {
			return BatchResult{}, errors.New("Qianchuan batch read dependencies are incomplete")
		}
		if strings.TrimSpace(normalized.ReadAccessToken) == "" {
			return BatchResult{}, errors.New("Qianchuan batch preflight access token is required")
		}
		return service.execute(ctx, normalized, normalized.ReadAccessToken, nil, domainplans.WriteCapability{})
	}
	guarded, err := service.Guard.Execute(ctx, sharedplans.GuardedMutation{
		Scope: domainplans.WriteScope{
			Channel: domainplans.ChannelQianchuan, AdvertiserID: normalized.AdvertiserID,
			LockFamily: domainplans.LockQianchuanWorks,
		},
		AuthAccountID: normalized.AuthAccountID,
		Submit:        true,
		Validate: func() error {
			if service.Reader == nil || service.Writer == nil || service.Reconciler == nil {
				return errors.New("Qianchuan batch write dependencies are incomplete")
			}
			return nil
		},
		Preview: normalized.emptyResult("dry_run"),
	}, func(ctx context.Context, execution sharedplans.MutationExecution) (any, error) {
		return service.execute(ctx, normalized, execution.AccessToken, execution.Dispatcher, execution.Capability)
	})
	result, ok := guarded.Value.(BatchResult)
	if !ok {
		if err != nil {
			return BatchResult{}, err
		}
		return BatchResult{}, errors.New("Qianchuan batch executor returned an invalid result")
	}
	return result, err
}

func (service BatchService) execute(
	ctx context.Context,
	request normalizedBatchRequest,
	accessToken string,
	dispatcher *sharedplans.OnceDispatcher,
	capability domainplans.WriteCapability,
) (BatchResult, error) {
	mode := "dry_run"
	if request.Submit {
		mode = "submit"
	}
	result := request.emptyResult(mode)
	if len(request.groups) == 0 {
		return result, nil
	}
	targets := make([]CreatorTarget, 0, len(request.groups))
	for _, group := range request.groups {
		targets = append(targets, CreatorTarget{
			AwemeID: group.creator.AwemeID, VisibleID: group.creator.VisibleID,
			ProductIDs: append([]string(nil), group.identity.ProductIDs...),
			GroupID:    group.groupID, BusinessDate: request.businessDate, Identity: group.identity,
		})
	}
	reconcileStarted := service.now()
	discovery, err := service.Reconciler.FindCurrentPlans(ctx, CurrentPlanRequest{
		AdvertiserID: request.AdvertiserID, AccessToken: accessToken, Targets: targets,
		ReadPool: request.ReadPool, Inventory: request.PlanInventory,
	})
	if err != nil {
		return BatchResult{}, fmt.Errorf("reconcile Qianchuan batch plans: %w", err)
	}
	if timing := batchTimingFrom(ctx); timing != nil {
		timing.GroupReconciliationSeconds += durationSeconds(reconcileStarted, service.now())
	}
	presentationRows := make([]map[string]any, 0)
	type groupPreparation struct {
		plans    []ExistingPlan
		policy   PlanMatchPolicy
		material *materialSnapshot
		err      error
	}
	materialStarted := service.now()
	preparations := parallelOrdered(ctx, request.ReadPool, len(request.groups), func(ctx context.Context, index int) groupPreparation {
		group := request.groups[index]
		policy, exists := discovery.Policies[group.groupID]
		if !exists {
			return groupPreparation{err: errors.New("Qianchuan reconciliation omitted a group policy")}
		}
		plans := discovery.Matches[group.groupID]
		if request.preflightDecisionChanged(group.groupID, plans, policy) {
			return groupPreparation{plans: plans, policy: policy}
		}
		if policy.Status != "bound" || len(plans) != 1 || len(filterWorksForProducts(group.works, plans[0].ProductIDs)) == 0 {
			return groupPreparation{plans: plans, policy: policy}
		}
		snapshot, prepareErr := service.planMaterialsWithPool(
			ctx, request.AdvertiserID, accessToken, plans[0].AdID, request.ReadPool,
		)
		return groupPreparation{plans: plans, policy: policy, material: &snapshot, err: prepareErr}
	})
	if timing := batchTimingFrom(ctx); timing != nil {
		timing.MaterialDiffSeconds += durationSeconds(materialStarted, service.now())
	}
	if request.Submit && len(request.PreparedGroups) != 0 {
		var preparationErr error
		for _, preparation := range preparations {
			if preparation.err != nil {
				preparationErr = preparation.err
				break
			}
		}
		if preparationErr != nil {
			for index, group := range request.groups {
				groupResult := newBatchGroupResult(group, group.identity.ProductIDs)
				if preparations[index].err != nil {
					groupResult.Status, groupResult.Error = "failed", preparations[index].err.Error()
				} else {
					groupResult.Status = "preflight_blocked"
					groupResult.Error = "another group could not complete submit revalidation"
				}
				result.Results = append(result.Results, groupResult)
				result.Counts[groupResult.Status]++
				result.FailedResults = append(result.FailedResults, groupResult)
			}
			result.Counts["creator_groups"] = len(result.Results)
			result.Counts["matched_works"] = len(request.Works)
			result.Counts["skipped_works"] = len(result.Skipped)
			result.ExitCode = 1
			return result, preparationErr
		}
	}
	for index, group := range request.groups {
		groupResult := newBatchGroupResult(group, group.identity.ProductIDs)
		preparation := preparations[index]
		groupResult.BindingDigest = preparation.policy.BindingDigest
		plans := preparation.plans
		if request.preflightDecisionChanged(group.groupID, plans, preparation.policy) {
			groupResult.Status = "preflight_changed"
			groupResult.ErrorCode = "PREFLIGHT_CHANGED"
			groupResult.Error = "current plan target changed after preflight; run preflight again"
			result.Results = append(result.Results, groupResult)
			result.Counts[groupResult.Status]++
			result.FailedResults = append(result.FailedResults, groupResult)
			result.ExitCode = 1
			presentationRows = append(presentationRows, batchPresentationRows(group, groupResult)...)
			continue
		}
		switch preparation.policy.Status {
		case "would_create":
			groupResult, err = service.executeNewGroup(
				ctx, request, group, groupResult, accessToken, dispatcher, capability,
			)
		case "bound":
			if preparation.err != nil || len(plans) != 1 {
				if preparation.err == nil {
					preparation.err = errors.New("bound Qianchuan plan is unavailable")
				}
				groupResult.Status, groupResult.Error = "failed", preparation.err.Error()
				err = preparation.err
			} else {
				groupResult, err = service.executeExistingGroup(
					ctx, request, group, groupResult, plans[0], preparation.material,
					accessToken, dispatcher, capability,
				)
			}
		case "legacy_binding_required":
			groupResult.Status = "legacy_binding_required"
			groupResult.ErrorCode = "LEGACY_BINDING_REQUIRED"
			groupResult.Error = "matching historical plan requires an explicit daily binding"
			groupResult.CandidatePlans = append([]ExistingPlan(nil), preparation.policy.Candidates...)
		case "binding_drift":
			groupResult.Status = "binding_drift"
			groupResult.ErrorCode = "BINDING_DRIFT"
			groupResult.Error = "bound Qianchuan plan no longer matches the complete group identity"
			groupResult.CandidatePlans = append([]ExistingPlan(nil), preparation.policy.Candidates...)
		default:
			groupResult.Status = "failed"
			if preparation.err != nil {
				groupResult.Error = preparation.err.Error()
			} else {
				groupResult.Error = "Qianchuan reconciliation returned an unsupported group policy"
			}
			err = errors.New(groupResult.Error)
		}
		result.Results = append(result.Results, groupResult)
		result.Counts[groupResult.Status]++
		if batchGroupFailed(groupResult.Status) {
			result.FailedResults = append(result.FailedResults, groupResult)
			result.ExitCode = 1
		}
		presentationRows = append(presentationRows, batchPresentationRows(group, groupResult)...)
		if err != nil {
			continue
		}
	}
	result.Counts["creator_groups"] = len(result.Results)
	result.Counts["matched_works"] = len(request.Works)
	result.Counts["skipped_works"] = len(result.Skipped)
	result.Presentation = domain.NewQianchuanBatchPresentation(
		presentationRows, []string{"skipped", "query_failures", "failed_results"},
	)
	return result, nil
}

func (service BatchService) executeExistingGroup(
	ctx context.Context,
	request normalizedBatchRequest,
	group batchGroup,
	result BatchGroupResult,
	plan ExistingPlan,
	prepared *materialSnapshot,
	accessToken string,
	dispatcher *sharedplans.OnceDispatcher,
	capability domainplans.WriteCapability,
) (BatchGroupResult, error) {
	result.AdID, result.PlanName, result.PlanStatus = plan.AdID, plan.Name, plan.Status
	result.ProductIDs = append([]string(nil), plan.ProductIDs...)
	eligible := filterWorksForProducts(group.works, plan.ProductIDs)
	if len(eligible) == 0 {
		result.Status = "skipped"
		result.Error = "existing plan products do not match the verified works"
		return result, nil
	}
	var snapshot materialSnapshot
	if prepared == nil {
		var err error
		snapshot, err = service.planMaterialsWithPool(
			ctx, request.AdvertiserID, accessToken, plan.AdID, request.ReadPool,
		)
		if err != nil {
			result.Status, result.Error = "failed", err.Error()
			return result, err
		}
	} else {
		snapshot = *prepared
	}
	newWorks := make([]VerifiedWork, 0, len(eligible))
	for _, work := range eligible {
		if _, exists := snapshot.activeIDs[work.AwemeItemID]; exists {
			result.AlreadyPresent = append(result.AlreadyPresent, work.AwemeItemID)
			result.CompletedItemIDs = append(result.CompletedItemIDs, work.AwemeItemID)
			continue
		}
		newWorks = append(newWorks, work)
	}
	if request.Submit {
		if prepared, exists := request.PreparedGroups[group.groupID]; exists &&
			(!sameStringSet(result.AlreadyPresent, prepared.ExpectedPresentItemIDs) ||
				!sameStringSet(verifiedWorkIDs(newWorks), prepared.ExpectedWriteItemIDs)) {
			result.Status = "preflight_changed"
			result.ErrorCode = "PREFLIGHT_CHANGED"
			result.Error = "current plan materials changed after preflight; run preflight again"
			return result, nil
		}
	}
	if len(newWorks) == 0 {
		result.Status = "already_present"
		return result, nil
	}
	if !request.Submit {
		result.Status = "would_append"
		for _, work := range newWorks {
			result.CompletedItemIDs = append(result.CompletedItemIDs, work.AwemeItemID)
		}
		for _, batch := range verifiedWorkBatches(newWorks, BatchMaterialLimit) {
			write := BatchWrite{
				Endpoint: AddMaterialsEndpoint, Status: "would_append", ItemIDs: verifiedWorkIDs(batch),
			}
			if request.IncludePayloads {
				payload, err := buildAddMaterialsPayload(
					request.AdvertiserID, plan.AdID, plan.ProductIDs, batch,
				)
				if err != nil {
					result.Status, result.Error = "failed", err.Error()
					return result, err
				}
				write.Payload = payload
			}
			result.Writes = append(result.Writes, write)
		}
		return result, nil
	}
	return service.appendWorks(
		ctx, request, result, group.groupID, plan.AdID, plan.ProductIDs, newWorks,
		accessToken, dispatcher, capability,
	)
}

func (service BatchService) executeNewGroup(
	ctx context.Context,
	request normalizedBatchRequest,
	group batchGroup,
	result BatchGroupResult,
	accessToken string,
	dispatcher *sharedplans.OnceDispatcher,
	capability domainplans.WriteCapability,
) (BatchGroupResult, error) {
	first, remaining := splitVerifiedWorks(group.works, BatchMaterialLimit)
	planName, err := service.planName(request, group)
	if err != nil {
		result.Status, result.Error = "failed", err.Error()
		return result, err
	}
	payload, err := buildBatchCreatePayload(request, group.creator, planName, first)
	if err != nil {
		result.Status, result.Error = "failed", err.Error()
		return result, err
	}
	result.PlanName = planName
	if !request.Submit {
		result.Status = "would_create"
		write := BatchWrite{
			Endpoint: CreateEndpoint, Status: "would_create", ItemIDs: verifiedWorkIDs(first),
		}
		if request.IncludePayloads {
			write.Payload = append(json.RawMessage(nil), payload...)
		}
		result.Writes = append(result.Writes, write)
		for _, batch := range verifiedWorkBatches(remaining, BatchMaterialLimit) {
			result.Writes = append(result.Writes, BatchWrite{
				Endpoint: AddMaterialsEndpoint, Status: "would_append", ItemIDs: verifiedWorkIDs(batch),
			})
		}
		result.CompletedItemIDs = verifiedWorkIDs(group.works)
		return result, nil
	}
	normalizedCreate, err := normalizeCreateRequest(CreateRequest{
		AdvertiserID: request.AdvertiserID, AuthAccountID: request.AuthAccountID,
		Submit: true, Payload: payload, VisibleID: group.creator.VisibleID,
		OperationKey: batchOperationKey(
			request.businessDate, group.groupID, "create", "new", verifiedWorkIDs(group.works),
		),
	})
	if err != nil {
		result.Status, result.Error = "failed", err.Error()
		return result, err
	}
	created, err := (CreateExecutor{
		Writer: service.Writer, Reconciler: service.Reconciler,
	}).execute(ctx, normalizedCreate, sharedplans.MutationExecution{
		Capability: capability, AccessToken: accessToken, Dispatcher: dispatcher,
	})
	createWrite := BatchWrite{
		Endpoint: CreateEndpoint, ItemIDs: verifiedWorkIDs(first),
		DispatchState: created.DispatchState, RequestID: created.RequestID,
	}
	if request.IncludePayloads {
		createWrite.Payload = append(json.RawMessage(nil), payload...)
	}
	if err != nil {
		createWrite.Status, createWrite.Error = "failed", err.Error()
		result.Writes = append(result.Writes, createWrite)
		result.Status, result.Error = "create_failed", err.Error()
		return result, err
	}
	createWrite.Status = created.Status
	if created.Reconciliation != nil {
		createWrite.Reconciliation = string(created.Reconciliation.State)
	}
	result.Writes = append(result.Writes, createWrite)
	result.AdID = created.AdID
	result.CompletedItemIDs = append(result.CompletedItemIDs, verifiedWorkIDs(first)...)
	if err := service.verifyCreatedPlan(ctx, request, group, planName, created.AdID, accessToken); err != nil {
		result.Status, result.Error = "create_unverified", err.Error()
		return result, err
	}
	if service.Bindings == nil {
		result.Status, result.Error = "binding_failed", "Qianchuan plan binding writer is required"
		return result, errors.New(result.Error)
	}
	binding, bindingErr := NewPlanBinding(group.identity, group.groupID, request.businessDate, created.AdID, service.now())
	if bindingErr == nil {
		bindingErr = service.Bindings.Put(ctx, binding)
	}
	if bindingErr != nil {
		result.Status, result.Error = "binding_failed", bindingErr.Error()
		return result, bindingErr
	}
	if len(remaining) == 0 {
		result.Status = "created"
		return result, nil
	}
	result.Status = "created"
	return service.appendWorks(
		ctx, request, result, group.groupID, created.AdID, group.identity.ProductIDs, remaining,
		accessToken, dispatcher, capability,
	)
}

func (service BatchService) verifyCreatedPlan(
	ctx context.Context,
	request normalizedBatchRequest,
	group batchGroup,
	planName string,
	adID string,
	accessToken string,
) error {
	if service.Reader == nil {
		return errors.New("Qianchuan plan reader is required for post-create verification")
	}
	detail, err := runRead(ctx, request.ReadPool, func(ctx context.Context) (domainqianchuan.PlanDetail, error) {
		return service.Reader.FetchPlanDetail(ctx, portqianchuan.PlanDetailRequest{
			AdvertiserID: request.AdvertiserID, AccessToken: accessToken, AdID: adID,
		})
	})
	if err != nil {
		return fmt.Errorf("verify created Qianchuan plan: %w", err)
	}
	if detail.AdID != adID || detail.AwemeID != group.identity.CreatorID || detail.Name != planName ||
		isDeletedPlan(detail.Status, detail.OptStatus) ||
		!sameStringSet(planProductIDs(detail.Products), group.identity.ProductIDs) {
		return errors.New("created Qianchuan plan did not match the complete verified target")
	}
	return nil
}

func (service BatchService) now() time.Time {
	if service.Now != nil {
		return service.Now()
	}
	return time.Now()
}

func (service BatchService) appendWorks(
	ctx context.Context,
	request normalizedBatchRequest,
	result BatchGroupResult,
	groupID string,
	adID string,
	productIDs []string,
	works []VerifiedWork,
	accessToken string,
	dispatcher *sharedplans.OnceDispatcher,
	capability domainplans.WriteCapability,
) (BatchGroupResult, error) {
	for _, batch := range verifiedWorkBatches(works, BatchMaterialLimit) {
		payload, err := buildAddMaterialsPayload(request.AdvertiserID, adID, productIDs, batch)
		if err != nil {
			result.Status, result.Error = "append_failed", err.Error()
			return result, err
		}
		itemIDs := verifiedWorkIDs(batch)
		receipt := dispatcher.Dispatch(
			ctx, capability,
			batchOperationKey(request.businessDate, groupID, "append", adID, itemIDs),
			func(ctx context.Context) (any, error) {
				return service.Writer.AddMaterials(ctx, portqianchuan.MaterialWriteRequest{
					AdvertiserID: request.AdvertiserID, AccessToken: accessToken, AdID: adID, Payload: payload,
				})
			},
		)
		write := BatchWrite{
			Endpoint: AddMaterialsEndpoint, ItemIDs: itemIDs, DispatchState: receipt.State,
		}
		if request.IncludePayloads {
			write.Payload = append(json.RawMessage(nil), payload...)
		}
		if receipt.Error == nil {
			written, ok := receipt.Value.(portqianchuan.WriteResult)
			if !ok || len(written.RowErrors) != 0 {
				err = errors.New("Qianchuan material add returned an invalid success result")
				write.Status, write.Error = "failed", err.Error()
				result.Writes = append(result.Writes, write)
				result.Status, result.Error = "append_failed", err.Error()
				return result, err
			}
			write.Status, write.RequestID = "appended", written.RequestID
			result.Writes = append(result.Writes, write)
			result.CompletedItemIDs = appendUniqueStrings(result.CompletedItemIDs, itemIDs...)
			continue
		}
		write.Error = receipt.Error.Error()
		if receipt.State != domainplans.DispatchUnknown {
			write.Status = "failed"
			result.Writes = append(result.Writes, write)
			result.Status, result.Error = "append_failed", receipt.Error.Error()
			return result, receipt.Error
		}
		snapshot, readErr := service.refreshPlanMaterialsWithPool(
			ctx, request.AdvertiserID, accessToken, adID, request.ReadPool,
		)
		if readErr != nil {
			write.Status = "unknown"
			result.Writes = append(result.Writes, write)
			result.Status, result.Error = "append_unknown", readErr.Error()
			return result, readErr
		}
		applied := make([]string, 0, len(itemIDs))
		for _, itemID := range itemIDs {
			if _, exists := snapshot.activeIDs[itemID]; exists {
				applied = append(applied, itemID)
			}
		}
		if len(applied) != len(itemIDs) {
			write.Status = "unknown"
			write.Reconciliation = "not_fully_applied"
			result.Writes = append(result.Writes, write)
			result.CompletedItemIDs = appendUniqueStrings(result.CompletedItemIDs, applied...)
			err = fmt.Errorf("Qianchuan material append remains unknown after readback: %d of %d present", len(applied), len(itemIDs))
			result.Status, result.Error = "append_unknown", err.Error()
			return result, err
		}
		write.Status, write.Reconciliation = "reconciled", "applied"
		result.Writes = append(result.Writes, write)
		result.CompletedItemIDs = appendUniqueStrings(result.CompletedItemIDs, itemIDs...)
	}
	if result.Status == "created" {
		result.Status = "created"
	} else {
		result.Status = "appended"
	}
	return result, nil
}

func (service BatchService) planMaterials(
	ctx context.Context,
	advertiserID string,
	accessToken string,
	adID string,
) (materialSnapshot, error) {
	return service.planMaterialsWithPool(ctx, advertiserID, accessToken, adID, nil)
}

func (service BatchService) planMaterialsWithPool(
	ctx context.Context,
	advertiserID string,
	accessToken string,
	adID string,
	pool *ReadPool,
) (materialSnapshot, error) {
	return runReadOnce(
		ctx, pool, planMaterialsReadKey(advertiserID, adID),
		func(ctx context.Context) (materialSnapshot, error) {
			return service.fetchPlanMaterialsSnapshot(ctx, advertiserID, accessToken, adID, pool)
		},
	)
}

func (service BatchService) refreshPlanMaterialsWithPool(
	ctx context.Context,
	advertiserID string,
	accessToken string,
	adID string,
	pool *ReadPool,
) (materialSnapshot, error) {
	pool.forgetRead(planMaterialsReadKey(advertiserID, adID))
	return service.planMaterialsWithPool(ctx, advertiserID, accessToken, adID, pool)
}

func (service BatchService) fetchPlanMaterialsSnapshot(
	ctx context.Context,
	advertiserID string,
	accessToken string,
	adID string,
	pool *ReadPool,
) (materialSnapshot, error) {
	rows, err := fetchAllPlanMaterialsWithPool(ctx, service.Reader, advertiserID, accessToken, adID, pool)
	if err != nil {
		return materialSnapshot{}, err
	}
	snapshot := materialSnapshot{rowsByItem: map[string]domainqianchuan.PlanMaterial{}, activeIDs: map[string]struct{}{}}
	for _, row := range rows {
		if row.AwemeItemID == "" || strings.EqualFold(row.MaterialStatus, "DELETED") ||
			(row.Deleted != nil && *row.Deleted) {
			continue
		}
		if existing, exists := snapshot.rowsByItem[row.AwemeItemID]; exists && existing.MaterialID != row.MaterialID {
			return materialSnapshot{}, errors.New("Qianchuan plan contains ambiguous active materials for one work")
		}
		snapshot.rowsByItem[row.AwemeItemID] = row
		snapshot.activeIDs[row.AwemeItemID] = struct{}{}
	}
	return snapshot, nil
}

func planMaterialsReadKey(advertiserID, adID string) string {
	return "plan-materials\x00" + advertiserID + "\x00" + adID
}

func normalizeBatchRequest(request BatchRequest) (normalizedBatchRequest, error) {
	request.AdvertiserID = strings.TrimSpace(request.AdvertiserID)
	request.AuthAccountID = strings.TrimSpace(request.AuthAccountID)
	request.ReadAccessToken = strings.TrimSpace(request.ReadAccessToken)
	request.TemplateID = strings.TrimSpace(request.TemplateID)
	request.TemplateName = strings.TrimSpace(request.TemplateName)
	request.ProductName = strings.TrimSpace(request.ProductName)
	request.ProductShortName = strings.TrimSpace(request.ProductShortName)
	if request.ProductShortName == "" {
		request.ProductShortName = request.ProductName
	}
	request.PlanType = strings.TrimSpace(request.PlanType)
	request.Business = strings.TrimSpace(request.Business)
	request.PreparedGroups = clonePreparedGroups(request.PreparedGroups)
	if !validPositiveID(request.AdvertiserID) {
		return normalizedBatchRequest{}, errors.New("advertiser_id must be a positive decimal ID")
	}
	if request.TemplateID == "" || request.TemplateName == "" || request.ProductName == "" {
		return normalizedBatchRequest{}, errors.New("Qianchuan batch template identity is incomplete")
	}
	object, err := decodeCreatePayload(request.TemplatePayload)
	if err != nil {
		return normalizedBatchRequest{}, err
	}
	if fmt.Sprint(object["advertiser_id"]) != request.AdvertiserID || object["marketing_goal"] != "VIDEO_PROM_GOODS" {
		return normalizedBatchRequest{}, errors.New("Qianchuan batch template payload does not match advertiser and product goal")
	}
	productIDs, err := payloadIDs(object["product_ids"], "product_ids", 30)
	if err != nil {
		return normalizedBatchRequest{}, err
	}
	if delivery, ok := object["delivery_setting"].(map[string]any); !ok {
		return normalizedBatchRequest{}, errors.New("Qianchuan batch template delivery_setting is required")
	} else if err := validateDeliverySetting(delivery); err != nil {
		return normalizedBatchRequest{}, err
	}
	productSet := stringSetFrom(productIDs)
	groups := make([]batchGroup, 0)
	groupIndex := map[string]int{}
	seenWorks := map[string]struct{}{}
	normalizedWorks := make([]VerifiedWork, 0, len(request.Works))
	for _, work := range request.Works {
		work.AwemeItemID = strings.TrimSpace(work.AwemeItemID)
		work.Creator.AwemeID = strings.TrimSpace(work.Creator.AwemeID)
		work.Creator.VisibleID = strings.TrimSpace(work.Creator.VisibleID)
		work.Creator.Name = strings.TrimSpace(work.Creator.Name)
		work.CreatorName = strings.TrimSpace(work.CreatorName)
		work.PlanType = strings.TrimSpace(work.PlanType)
		work.Business = strings.TrimSpace(work.Business)
		work.Material.ImageMode = strings.TrimSpace(work.Material.ImageMode)
		if !validPositiveID(work.AwemeItemID) || !validPositiveID(work.Creator.AwemeID) {
			return normalizedBatchRequest{}, errors.New("verified Qianchuan work contains an invalid identity")
		}
		if _, duplicate := seenWorks[work.AwemeItemID]; duplicate {
			return normalizedBatchRequest{}, errors.New("verified Qianchuan works must be unique")
		}
		seenWorks[work.AwemeItemID] = struct{}{}
		if work.Material.AwemeItemID != "" && work.Material.AwemeItemID != work.AwemeItemID {
			return normalizedBatchRequest{}, errors.New("verified Qianchuan material identity does not match the work")
		}
		if _, supported := supportedWorkImageModes[work.Material.ImageMode]; !supported {
			request.Skipped = append(request.Skipped, SkippedWork{
				InputIndex: work.InputIndex, InputURL: work.InputURL, AwemeItemID: work.AwemeItemID,
				Reason: "unsupported_image_mode", Message: "作品素材类型不支持投放",
			})
			continue
		}
		matched := make([]string, 0, len(work.MatchedProductIDs))
		seenProducts := map[string]struct{}{}
		for _, productID := range work.MatchedProductIDs {
			productID = strings.TrimSpace(productID)
			if _, allowed := productSet[productID]; !allowed {
				continue
			}
			if _, duplicate := seenProducts[productID]; duplicate {
				continue
			}
			seenProducts[productID] = struct{}{}
			matched = append(matched, productID)
		}
		if len(matched) == 0 {
			request.Skipped = append(request.Skipped, SkippedWork{
				InputIndex: work.InputIndex, InputURL: work.InputURL, AwemeItemID: work.AwemeItemID,
				Reason: "product_mismatch", Message: "作品与模板绑定商品不匹配",
			})
			continue
		}
		products, productErr := domainqianchuan.NormalizeProductIDs(matched)
		if productErr != nil {
			return normalizedBatchRequest{}, productErr
		}
		work.MatchedProductIDs = products
		normalizedWorks = append(normalizedWorks, work)
		identity, identityErr := domainqianchuan.NewPlanGroupIdentity(
			request.AdvertiserID, request.TemplateID, work.Creator.AwemeID,
			work.MatchedProductIDs, work.PlanType, work.Business,
		)
		if identityErr != nil {
			return normalizedBatchRequest{}, identityErr
		}
		groupID, groupErr := domainqianchuan.GroupID(identity)
		if groupErr != nil {
			return normalizedBatchRequest{}, groupErr
		}
		index, exists := groupIndex[groupID]
		if !exists {
			index = len(groups)
			groupIndex[groupID] = index
			groups = append(groups, batchGroup{
				groupID: groupID, identity: identity, creator: work.Creator, works: []VerifiedWork{},
			})
		}
		groups[index].works = append(groups[index].works, work)
	}
	request.Works = normalizedWorks
	if len(request.PreparedGroups) != 0 {
		if !request.Submit {
			return normalizedBatchRequest{}, errors.New("Qianchuan preflight decisions are valid only for submit")
		}
		if len(request.PreparedGroups) != len(groups) {
			return normalizedBatchRequest{}, errors.New("Qianchuan preflight decisions do not match verified creator groups")
		}
		for _, group := range groups {
			expected, exists := request.PreparedGroups[group.groupID]
			if !exists {
				return normalizedBatchRequest{}, errors.New("Qianchuan preflight decision is missing a verified plan group")
			}
			if canonicalID, identityErr := domainqianchuan.GroupID(expected.Identity); identityErr != nil ||
				canonicalID != group.groupID || canonicalID != expected.GroupID {
				return normalizedBatchRequest{}, errors.New("Qianchuan preflight group identity changed")
			}
			switch expected.ExpectedAction {
			case "would_create":
				if strings.TrimSpace(expected.ExpectedAdID) != "" {
					return normalizedBatchRequest{}, errors.New("Qianchuan create preflight decision contains an ad_id")
				}
			case "would_append", "noop":
				if !validPositiveID(expected.ExpectedAdID) {
					return normalizedBatchRequest{}, errors.New("Qianchuan append preflight decision requires a valid ad_id")
				}
			default:
				return normalizedBatchRequest{}, errors.New("Qianchuan preflight decision is unsupported")
			}
		}
	}
	return normalizedBatchRequest{
		BatchRequest: request, payloadObject: object,
		productIDs: productIDs, groups: groups,
	}, nil
}

func (request normalizedBatchRequest) preflightDecisionChanged(
	groupID string,
	plans []ExistingPlan,
	policy PlanMatchPolicy,
) bool {
	if len(request.PreparedGroups) == 0 {
		return false
	}
	expected := request.PreparedGroups[groupID]
	if expected.BindingDigest == "" || expected.BindingDigest != policy.BindingDigest {
		return true
	}
	if expected.ExpectedAction == "would_create" {
		return policy.Status != "would_create" || len(plans) != 0
	}
	return policy.Status != "bound" || len(plans) != 1 || plans[0].AdID != expected.ExpectedAdID
}

func (request normalizedBatchRequest) emptyResult(mode string) BatchResult {
	return BatchResult{
		Mode: mode, Channel: "qianchuan", BusinessDate: request.businessDate,
		Template: BatchTemplateSummary{
			TemplateID: request.TemplateID, Name: request.TemplateName,
			AdvertiserID: request.AdvertiserID, ProductIDs: append([]string(nil), request.productIDs...),
		},
		Counts: map[string]int{}, Results: []BatchGroupResult{},
		Skipped:       append([]SkippedWork(nil), request.Skipped...),
		QueryFailures: append([]WorkQueryFailure(nil), request.QueryFailures...),
		FailedResults: []BatchGroupResult{},
		Presentation: domain.NewQianchuanBatchPresentation(
			nil, []string{"skipped", "query_failures", "failed_results"},
		),
	}
}

func newBatchGroupResult(group batchGroup, productIDs []string) BatchGroupResult {
	creatorName := group.creator.Name
	for _, work := range group.works {
		if strings.TrimSpace(work.CreatorName) != "" {
			creatorName = strings.TrimSpace(work.CreatorName)
			break
		}
	}
	return BatchGroupResult{
		GroupID: group.groupID, AwemeID: group.creator.AwemeID, DouyinID: group.creator.VisibleID,
		CreatorName: creatorName, ProductIDs: append([]string(nil), productIDs...),
		PlanType: group.identity.PlanType, Business: group.identity.Business,
		InputItemIDs: verifiedWorkIDs(group.works), CompletedItemIDs: []string{},
		AlreadyPresent: []string{}, Writes: []BatchWrite{}, Status: "ready",
	}
}

func (service BatchService) planName(request normalizedBatchRequest, group batchGroup) (string, error) {
	fields, err := batchGroupPlanNameFields(group.works, request.PlanType, request.Business)
	if err != nil {
		return "", err
	}
	creator := group.creator
	label := fields.creatorName
	if label == "" {
		label = creator.Name
	}
	if label == "" {
		label = creator.VisibleID
	}
	if label == "" {
		label = creator.AwemeID
	}
	now := time.Now()
	if service.Now != nil {
		now = service.Now()
	}
	values := map[string]string{
		"product_name":       request.ProductName,
		"product_short_name": request.ProductShortName,
		"creator_name":       label,
		"aweme_id":           creator.AwemeID,
		"douyin_id":          creator.VisibleID,
		"date":               now.Format("20060102"),
		"time":               now.Format("150405"),
		"datetime":           now.Format("20060102150405"),
		"month_day":          fmt.Sprintf("%d.%d", int(now.Month()), now.Day()),
		"type":               fields.planType,
		"business":           fields.business,
	}
	pattern := strings.TrimSpace(request.PlanNameTemplate)
	if pattern == "" {
		pattern = "{month_day}-{creator_name}-{product_short_name}-{type}-{business}"
	}
	if strings.Contains(pattern, "{creator_name}") && fields.creatorName == "" {
		return "", errors.New("F2 未返回达人名称，无法创建千川计划")
	}
	for _, key := range []string{"type", "business"} {
		if values[key] == "" {
			pattern = removeOptionalPlanNamePlaceholder(pattern, key)
		}
	}
	for key, value := range values {
		pattern = strings.ReplaceAll(pattern, "{"+key+"}", value)
	}
	name := weightedTruncate(sanitizePlanName(pattern), 100)
	if name == "" {
		return "", errors.New("Qianchuan rendered plan name is empty")
	}
	return name, nil
}

type batchPlanNameFields struct {
	creatorName string
	planType    string
	business    string
}

func batchGroupPlanNameFields(works []VerifiedWork, planType, business string) (batchPlanNameFields, error) {
	fields := batchPlanNameFields{}
	for index, work := range works {
		workType := strings.TrimSpace(work.PlanType)
		if workType == "" {
			workType = strings.TrimSpace(planType)
		}
		workBusiness := strings.TrimSpace(work.Business)
		if workBusiness == "" {
			workBusiness = strings.TrimSpace(business)
		}
		if index == 0 {
			fields.planType = workType
		} else if fields.planType != workType {
			return batchPlanNameFields{}, errors.New("同一达人素材的类型不一致，无法合并到同一个千川计划")
		}
		if index == 0 {
			fields.business = workBusiness
		} else if fields.business != workBusiness {
			return batchPlanNameFields{}, errors.New("同一达人素材的商务不一致，无法合并到同一个千川计划")
		}
		creatorName := strings.TrimSpace(work.CreatorName)
		if creatorName == "" {
			continue
		}
		if fields.creatorName == "" {
			fields.creatorName = creatorName
		} else if fields.creatorName != creatorName {
			return batchPlanNameFields{}, errors.New("同一达人素材的 F2 达人名称不一致")
		}
	}
	return fields, nil
}

func removeOptionalPlanNamePlaceholder(pattern, key string) string {
	token := "{" + key + "}"
	for strings.Contains(pattern, token) {
		byteIndex := strings.Index(pattern, token)
		runeIndex := utf8.RuneCountInString(pattern[:byteIndex])
		runes := []rune(pattern)
		tokenLength := len([]rune(token))
		start := runeIndex
		for start > 0 && isPlanNameSeparator(runes[start-1]) {
			start--
		}
		if start < runeIndex && start > 0 {
			pattern = string(append(runes[:start], runes[runeIndex+tokenLength:]...))
			continue
		}
		end := runeIndex + tokenLength
		for end < len(runes) && isPlanNameSeparator(runes[end]) {
			end++
		}
		pattern = string(append(runes[:runeIndex], runes[end:]...))
	}
	return pattern
}

func isPlanNameSeparator(value rune) bool {
	return strings.ContainsRune("-_/|· ", value)
}

func buildBatchCreatePayload(
	request normalizedBatchRequest,
	creator domainqianchuan.AuthorizedCreator,
	name string,
	works []VerifiedWork,
) (json.RawMessage, error) {
	object := cloneMap(request.payloadObject)
	awemeID, err := strconv.ParseInt(creator.AwemeID, 10, 64)
	if err != nil || awemeID <= 0 {
		return nil, errors.New("Qianchuan creator aweme_id exceeds the official integer range")
	}
	object["aweme_id"] = awemeID
	object["name"] = name
	creatives, err := buildProductCreatives(request.productIDs, works, true)
	if err != nil {
		return nil, err
	}
	object["multi_product_creative_list"] = creatives
	return json.Marshal(object)
}

func buildAddMaterialsPayload(
	advertiserID string,
	adID string,
	productIDs []string,
	works []VerifiedWork,
) (json.RawMessage, error) {
	advertiser, err := strconv.ParseInt(advertiserID, 10, 64)
	if err != nil || advertiser <= 0 {
		return nil, errors.New("Qianchuan advertiser_id exceeds the official integer range")
	}
	plan, err := strconv.ParseInt(adID, 10, 64)
	if err != nil || plan <= 0 {
		return nil, errors.New("Qianchuan ad_id exceeds the official integer range")
	}
	creatives, err := buildProductCreatives(productIDs, works, false)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"advertiser_id": advertiser, "ad_id": plan,
		"multi_product_creative_list": creatives,
	})
}

func buildProductCreatives(productIDs []string, works []VerifiedWork, forCreate bool) ([]map[string]any, error) {
	byProduct := map[string][]map[string]any{}
	allowed := stringSetFrom(productIDs)
	for _, work := range works {
		itemID, err := strconv.ParseInt(work.AwemeItemID, 10, 64)
		if err != nil || itemID <= 0 {
			return nil, errors.New("Qianchuan aweme_item_id exceeds the official integer range")
		}
		for _, productID := range work.MatchedProductIDs {
			if _, ok := allowed[productID]; !ok {
				continue
			}
			byProduct[productID] = append(byProduct[productID], map[string]any{
				"aweme_item_id": itemID, "image_mode": work.Material.ImageMode,
			})
		}
	}
	result := make([]map[string]any, 0, len(byProduct))
	for _, productID := range productIDs {
		videos := byProduct[productID]
		if len(videos) == 0 {
			continue
		}
		parsedProduct, err := strconv.ParseInt(productID, 10, 64)
		if err != nil || parsedProduct <= 0 {
			return nil, errors.New("Qianchuan product_id exceeds the official integer range")
		}
		row := map[string]any{"product_id": parsedProduct, "video_material": videos}
		if forCreate {
			row["creative_type"] = "PROGRAMMATIC_CREATIVE"
		}
		result = append(result, row)
	}
	if len(result) == 0 {
		return nil, errors.New("Qianchuan material payload has no product-matched works")
	}
	return result, nil
}

func batchPresentationRows(group batchGroup, result BatchGroupResult) []map[string]any {
	completed := stringSetFrom(result.CompletedItemIDs)
	products := stringSetFrom(result.ProductIDs)
	rows := make([]map[string]any, 0)
	for _, work := range group.works {
		if _, ok := completed[work.AwemeItemID]; !ok {
			continue
		}
		materialID := work.Material.MaterialID
		if materialID == "" {
			materialID = work.AwemeItemID
		}
		for _, productID := range work.MatchedProductIDs {
			if _, ok := products[productID]; !ok {
				continue
			}
			rows = append(rows, map[string]any{
				"plan_id": result.AdID, "creator_nickname": result.CreatorName,
				"product_id": productID, "material_id": materialID,
				"material_title": work.Material.Title,
			})
		}
	}
	return rows
}

func filterWorksForProducts(works []VerifiedWork, productIDs []string) []VerifiedWork {
	allowed := stringSetFrom(productIDs)
	result := make([]VerifiedWork, 0, len(works))
	for _, work := range works {
		matched := make([]string, 0, len(work.MatchedProductIDs))
		for _, productID := range work.MatchedProductIDs {
			if _, ok := allowed[productID]; ok {
				matched = append(matched, productID)
			}
		}
		if len(matched) != 0 {
			work.MatchedProductIDs = matched
			result = append(result, work)
		}
	}
	return result
}

func filterExistingPlansForProducts(plans []ExistingPlan, productIDs []string) []ExistingPlan {
	allowed := stringSetFrom(productIDs)
	result := make([]ExistingPlan, 0, len(plans))
	for _, plan := range plans {
		for _, productID := range plan.ProductIDs {
			if _, matched := allowed[productID]; matched {
				result = append(result, plan)
				break
			}
		}
	}
	return result
}

func splitVerifiedWorks(values []VerifiedWork, size int) ([]VerifiedWork, []VerifiedWork) {
	if len(values) <= size {
		return values, nil
	}
	return values[:size], values[size:]
}

func verifiedWorkBatches(values []VerifiedWork, size int) [][]VerifiedWork {
	result := make([][]VerifiedWork, 0, (len(values)+size-1)/size)
	for start := 0; start < len(values); start += size {
		end := min(start+size, len(values))
		result = append(result, values[start:end])
	}
	return result
}

func verifiedWorkIDs(values []VerifiedWork) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.AwemeItemID
	}
	return result
}

func appendUniqueStrings(target []string, values ...string) []string {
	seen := stringSetFrom(target)
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		target = append(target, value)
	}
	return target
}

func batchOperationKey(businessDate, groupID, action, target string, itemIDs []string) string {
	items := append([]string(nil), itemIDs...)
	sort.Strings(items)
	digest := sha256.Sum256([]byte(strings.Join([]string{
		businessDate, groupID, action, target, strings.Join(items, "\x00"),
	}, "\x01")))
	return "qianchuan-batch-" + hex.EncodeToString(digest[:16])
}

func weightedTruncate(value string, maximum int) string {
	var builder strings.Builder
	length := 0
	for _, character := range value {
		width := 1
		if character >= 128 {
			width = 2
		}
		if length+width > maximum {
			break
		}
		builder.WriteRune(character)
		length += width
	}
	return builder.String()
}

func sanitizePlanName(value string) string {
	var builder strings.Builder
	for _, character := range value {
		if unicode.IsSpace(character) {
			builder.WriteByte(' ')
			continue
		}
		if character == '\u20e3' || character == '\ufe0e' || character == '\ufe0f' ||
			!unicode.IsGraphic(character) ||
			unicode.In(character, unicode.Sc, unicode.Sk, unicode.Sm, unicode.So) {
			continue
		}
		builder.WriteRune(character)
	}
	return strings.Join(strings.Fields(builder.String()), " ")
}

func batchGroupFailed(status string) bool {
	switch status {
	case "would_create", "would_append", "created", "appended", "already_present", "skipped":
		return false
	default:
		return true
	}
}
