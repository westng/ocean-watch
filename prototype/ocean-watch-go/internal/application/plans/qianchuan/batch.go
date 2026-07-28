package qianchuan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	sharedplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/plans"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	domainplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/plans"
	domainqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/qianchuan"
	portqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/qianchuan"
)

const (
	BatchMaterialLimit   = 100
	AddMaterialsEndpoint = "/v1.0/qianchuan/uni_promotion/ad/material/add/"
)

var supportedWorkImageModes = map[string]struct{}{
	"VIDEO_LARGE": {}, "VIDEO_VERTICAL": {},
}

type BatchRequest struct {
	AdvertiserID    string
	AuthAccountID   string
	ReadAccessToken string
	Submit          bool
	TemplateID      string
	TemplateName    string
	ProductName     string
	TemplatePayload json.RawMessage
	IncludePayloads bool
	Works           []VerifiedWork
	Skipped         []SkippedWork
	QueryFailures   []WorkQueryFailure
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
	AwemeID          string       `json:"aweme_id"`
	DouyinID         string       `json:"douyin_id,omitempty"`
	CreatorName      string       `json:"creator_name,omitempty"`
	AdID             string       `json:"ad_id,omitempty"`
	PlanName         string       `json:"plan_name,omitempty"`
	PlanStatus       string       `json:"plan_status,omitempty"`
	ProductIDs       []string     `json:"product_ids"`
	InputItemIDs     []string     `json:"input_item_ids"`
	AlreadyPresent   []string     `json:"already_present_item_ids"`
	CompletedItemIDs []string     `json:"completed_item_ids"`
	Status           string       `json:"status"`
	Writes           []BatchWrite `json:"writes"`
	Error            string       `json:"error,omitempty"`
}

type BatchResult struct {
	Mode          string               `json:"mode"`
	Channel       string               `json:"channel"`
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
	Now        func() time.Time
}

type normalizedBatchRequest struct {
	BatchRequest
	payloadObject map[string]any
	productIDs    []string
	groups        []batchGroup
}

type batchGroup struct {
	creator domainqianchuan.AuthorizedCreator
	works   []VerifiedWork
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
			ProductIDs: request.productIDs,
		})
	}
	discovery, err := service.Reconciler.FindCurrentPlans(ctx, CurrentPlanRequest{
		AdvertiserID: request.AdvertiserID, AccessToken: accessToken, Targets: targets,
	})
	if err != nil {
		return BatchResult{}, fmt.Errorf("reconcile Qianchuan batch plans: %w", err)
	}
	presentationRows := make([]map[string]any, 0)
	for _, group := range request.groups {
		groupResult := newBatchGroupResult(group, request.productIDs)
		plans := discovery.Matches[group.creator.AwemeID]
		switch len(plans) {
		case 0:
			groupResult, err = service.executeNewGroup(
				ctx, request, group, groupResult, accessToken, dispatcher, capability,
			)
		case 1:
			groupResult, err = service.executeExistingGroup(
				ctx, request, group, groupResult, plans[0], accessToken, dispatcher, capability,
			)
		default:
			groupResult.Status = "failed"
			groupResult.Error = "multiple current Qianchuan plans match the creator and products"
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
	snapshot, err := service.planMaterials(ctx, request.AdvertiserID, accessToken, plan.AdID)
	if err != nil {
		result.Status, result.Error = "failed", err.Error()
		return result, err
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
				write.Payload, err = buildAddMaterialsPayload(
					request.AdvertiserID, plan.AdID, plan.ProductIDs, batch,
				)
				if err != nil {
					result.Status, result.Error = "failed", err.Error()
					return result, err
				}
			}
			result.Writes = append(result.Writes, write)
		}
		return result, nil
	}
	return service.appendWorks(
		ctx, request, result, plan.AdID, plan.ProductIDs, newWorks,
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
	planName := service.planName(request, group.creator)
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
	if len(remaining) == 0 {
		result.Status = "created"
		return result, nil
	}
	result.Status = "created"
	return service.appendWorks(
		ctx, request, result, created.AdID, request.productIDs, remaining,
		accessToken, dispatcher, capability,
	)
}

func (service BatchService) appendWorks(
	ctx context.Context,
	request normalizedBatchRequest,
	result BatchGroupResult,
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
		receipt := dispatcher.Dispatch(ctx, capability, materialOperationKey(adID, itemIDs), func(ctx context.Context) (any, error) {
			return service.Writer.AddMaterials(ctx, portqianchuan.MaterialWriteRequest{
				AdvertiserID: request.AdvertiserID, AccessToken: accessToken, AdID: adID, Payload: payload,
			})
		})
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
		snapshot, readErr := service.planMaterials(ctx, request.AdvertiserID, accessToken, adID)
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
	rows, err := fetchAllPlanMaterials(ctx, service.Reader, advertiserID, accessToken, adID)
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

func normalizeBatchRequest(request BatchRequest) (normalizedBatchRequest, error) {
	request.AdvertiserID = strings.TrimSpace(request.AdvertiserID)
	request.AuthAccountID = strings.TrimSpace(request.AuthAccountID)
	request.ReadAccessToken = strings.TrimSpace(request.ReadAccessToken)
	request.TemplateID = strings.TrimSpace(request.TemplateID)
	request.TemplateName = strings.TrimSpace(request.TemplateName)
	request.ProductName = strings.TrimSpace(request.ProductName)
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
		work.MatchedProductIDs = matched
		normalizedWorks = append(normalizedWorks, work)
		index, exists := groupIndex[work.Creator.AwemeID]
		if !exists {
			index = len(groups)
			groupIndex[work.Creator.AwemeID] = index
			groups = append(groups, batchGroup{creator: work.Creator, works: []VerifiedWork{}})
		} else if groups[index].creator.VisibleID != work.Creator.VisibleID || groups[index].creator.Name != work.Creator.Name {
			return normalizedBatchRequest{}, errors.New("verified Qianchuan works disagree on creator identity")
		}
		groups[index].works = append(groups[index].works, work)
	}
	request.Works = normalizedWorks
	return normalizedBatchRequest{
		BatchRequest: request, payloadObject: object,
		productIDs: productIDs, groups: groups,
	}, nil
}

func (request normalizedBatchRequest) emptyResult(mode string) BatchResult {
	return BatchResult{
		Mode: mode, Channel: "qianchuan",
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
	return BatchGroupResult{
		AwemeID: group.creator.AwemeID, DouyinID: group.creator.VisibleID,
		CreatorName: group.creator.Name, ProductIDs: append([]string(nil), productIDs...),
		InputItemIDs: verifiedWorkIDs(group.works), CompletedItemIDs: []string{},
		AlreadyPresent: []string{}, Writes: []BatchWrite{}, Status: "ready",
	}
}

func (service BatchService) planName(request normalizedBatchRequest, creator domainqianchuan.AuthorizedCreator) string {
	label := creator.Name
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
	prefix := weightedTruncate(request.ProductName+"-"+label, 84)
	return prefix + "-" + now.Format("20060102150405")
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

func materialOperationKey(adID string, itemIDs []string) string {
	digest := sha256.Sum256([]byte(adID + "\x00" + strings.Join(itemIDs, "\x00")))
	return "qianchuan-material-add-" + hex.EncodeToString(digest[:16])
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

func batchGroupFailed(status string) bool {
	switch status {
	case "would_create", "would_append", "created", "appended", "already_present", "skipped":
		return false
	default:
		return true
	}
}
