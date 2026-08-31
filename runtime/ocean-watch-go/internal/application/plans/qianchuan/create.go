package qianchuan

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

const CreateEndpoint = "/v1.0/qianchuan/uni_aweme/ad/create/"

type CreateRequest struct {
	AdvertiserID  string
	AuthAccountID string
	Submit        bool
	Payload       json.RawMessage
	VisibleID     string
	OperationKey  string
}

type CreateResult struct {
	Mode           string                        `json:"mode"`
	Status         string                        `json:"status"`
	Endpoint       string                        `json:"endpoint"`
	AdvertiserID   string                        `json:"advertiser_id"`
	Payload        map[string]any                `json:"payload"`
	AdID           string                        `json:"ad_id,omitempty"`
	RequestID      string                        `json:"request_id,omitempty"`
	FailureStage   string                        `json:"failure_stage,omitempty"`
	DispatchState  domainplans.DispatchState     `json:"dispatch_state,omitempty"`
	Reconciliation *domainplans.Reconciliation   `json:"reconciliation,omitempty"`
	LastResponse   *domainplans.OfficialResponse `json:"last_response,omitempty"`
}

type CreatePayloadValidator interface {
	ValidateCreatePlan(string, json.RawMessage) error
}

type CreateExecutor struct {
	Guard      sharedplans.GuardedExecutor
	Writer     portqianchuan.Writer
	Reconciler CurrentPlanFinder
}

type normalizedCreateRequest struct {
	CreateRequest
	payloadObject map[string]any
	goal          string
	planName      string
	awemeID       string
	productIDs    []string
	operationKey  string
}

func (executor CreateExecutor) Execute(
	ctx context.Context,
	request CreateRequest,
) (CreateResult, error) {
	normalized, err := normalizeCreateRequest(request)
	if err != nil {
		return CreateResult{}, err
	}
	preview := normalized.preview()
	guarded, err := executor.Guard.Execute(ctx, sharedplans.GuardedMutation{
		Scope: domainplans.WriteScope{
			Channel: domainplans.ChannelQianchuan, AdvertiserID: normalized.AdvertiserID,
			LockFamily: domainplans.LockQianchuanWorks,
		},
		AuthAccountID: normalized.AuthAccountID,
		Submit:        normalized.Submit,
		Validate: func() error {
			if !normalized.Submit {
				return nil
			}
			if executor.Writer == nil {
				return errors.New("Qianchuan plan writer is required")
			}
			if validator, ok := executor.Writer.(CreatePayloadValidator); ok {
				return validator.ValidateCreatePlan(normalized.AdvertiserID, normalized.Payload)
			}
			return nil
		},
		Preview: preview,
	}, func(ctx context.Context, execution sharedplans.MutationExecution) (any, error) {
		return executor.execute(ctx, normalized, execution)
	})
	if guarded.Mode == "dry_run" {
		return preview, err
	}
	result, ok := guarded.Value.(CreateResult)
	if !ok {
		if err != nil {
			return CreateResult{}, err
		}
		return CreateResult{}, errors.New("Qianchuan create executor returned an invalid result")
	}
	return result, err
}

func (executor CreateExecutor) execute(
	ctx context.Context,
	request normalizedCreateRequest,
	execution sharedplans.MutationExecution,
) (CreateResult, error) {
	result := request.preview()
	result.Mode = "submit"
	receipt := execution.Dispatcher.Dispatch(
		ctx, execution.Capability, request.operationKey,
		func(ctx context.Context) (any, error) {
			return executor.Writer.CreatePlan(ctx, portqianchuan.CreatePlanRequest{
				AdvertiserID: request.AdvertiserID, AccessToken: execution.AccessToken,
				Payload: append(json.RawMessage(nil), request.Payload...),
			})
		},
	)
	result.DispatchState = receipt.State
	if receipt.Error == nil {
		written, ok := receipt.Value.(portqianchuan.WriteResult)
		if !ok || !validPositiveID(written.ObjectID) {
			err := errors.New("Qianchuan plan response is missing a valid ad_id")
			result.Status, result.FailureStage = "failed", "plan_create"
			return result, err
		}
		result.Status, result.AdID, result.RequestID = "created", written.ObjectID, written.RequestID
		return result, nil
	}
	result.LastResponse = domainplans.OfficialResponseFromError(receipt.Error)
	if receipt.State != domainplans.DispatchUnknown {
		result.Status, result.FailureStage = "failed", "plan_create"
		return result, receipt.Error
	}
	if request.goal != "VIDEO_PROM_GOODS" || request.awemeID == "" || request.planName == "" ||
		len(request.productIDs) == 0 {
		result.Status, result.FailureStage = "unknown", "plan_create"
		return result, errors.New("Qianchuan plan result is unknown and lacks a stable reconciliation key")
	}
	if executor.Reconciler == nil {
		result.Status, result.FailureStage = "unknown", "plan_reconciliation"
		return result, errors.New("Qianchuan plan result is unknown and no reconciler is configured")
	}
	discovery, err := executor.Reconciler.FindCurrentPlans(ctx, CurrentPlanRequest{
		AdvertiserID: request.AdvertiserID, AccessToken: execution.AccessToken,
		Targets: []CreatorTarget{{
			AwemeID: request.awemeID, VisibleID: request.VisibleID,
			ProductIDs: request.productIDs, PlanName: request.planName,
		}},
	})
	if err != nil {
		result.Status, result.FailureStage = "unknown", "plan_reconciliation"
		return result, fmt.Errorf("reconcile Qianchuan plan creation: %w", err)
	}
	candidateIDs := make([]string, 0, len(discovery.Matches[request.awemeID]))
	for _, candidate := range discovery.Matches[request.awemeID] {
		candidateIDs = append(candidateIDs, candidate.AdID)
	}
	reconciliation, err := domainplans.ReconcileCandidates(candidateIDs)
	if err != nil {
		result.Status, result.FailureStage = "unknown", "plan_reconciliation"
		return result, err
	}
	result.Reconciliation = &reconciliation
	if reconciliation.State != domainplans.ReconciliationApplied {
		result.Status, result.FailureStage = "unknown", "plan_reconciliation"
		return result, fmt.Errorf("Qianchuan plan result is %s after reconciliation", reconciliation.State)
	}
	result.Status, result.AdID = "reconciled", reconciliation.ObjectID
	return result, nil
}

func normalizeCreateRequest(request CreateRequest) (normalizedCreateRequest, error) {
	request.AdvertiserID = strings.TrimSpace(request.AdvertiserID)
	request.AuthAccountID = strings.TrimSpace(request.AuthAccountID)
	request.VisibleID = strings.TrimSpace(request.VisibleID)
	request.OperationKey = strings.TrimSpace(request.OperationKey)
	if !validPositiveID(request.AdvertiserID) {
		return normalizedCreateRequest{}, errors.New("advertiser_id must be a positive decimal ID")
	}
	object, err := decodeCreatePayload(request.Payload)
	if err != nil {
		return normalizedCreateRequest{}, err
	}
	if err := normalizeCreatePayloadIDs(object); err != nil {
		return normalizedCreateRequest{}, err
	}
	if fmt.Sprint(object["advertiser_id"]) != request.AdvertiserID {
		return normalizedCreateRequest{}, errors.New("Qianchuan payload advertiser_id does not match command scope")
	}
	goal, ok := object["marketing_goal"].(string)
	goal = strings.TrimSpace(goal)
	if !ok || (goal != "VIDEO_PROM_GOODS" && goal != "LIVE_PROM_GOODS") {
		return normalizedCreateRequest{}, errors.New("Qianchuan payload marketing_goal is unsupported")
	}
	delivery, ok := object["delivery_setting"].(map[string]any)
	if !ok {
		return normalizedCreateRequest{}, errors.New("Qianchuan payload delivery_setting is required")
	}
	if err := validateDeliverySetting(delivery); err != nil {
		return normalizedCreateRequest{}, err
	}
	name, _ := object["name"].(string)
	name = strings.TrimSpace(name)
	awemeID := payloadID(object["aweme_id"])
	productIDs := []string{}
	if goal == "VIDEO_PROM_GOODS" {
		productIDs, err = payloadIDs(object["product_ids"], "product_ids", 30)
		if err != nil {
			return normalizedCreateRequest{}, err
		}
		if object["name"] != nil {
			name = sanitizePlanName(name)
			object["name"] = name
		}
		if object["name"] != nil && (name == "" || weightedLength(name) > 100) {
			return normalizedCreateRequest{}, errors.New("Qianchuan plan name exceeds 100 weighted characters")
		}
		if object["aweme_id"] != nil && !validPositiveID(awemeID) {
			return normalizedCreateRequest{}, errors.New("Qianchuan payload aweme_id must be a positive decimal ID")
		}
	} else {
		if !validPositiveID(awemeID) {
			return normalizedCreateRequest{}, errors.New("Qianchuan live payload requires a positive aweme_id")
		}
		if name != "" {
			return normalizedCreateRequest{}, errors.New("Qianchuan live payload does not support name")
		}
	}
	normalizedPayload, err := json.Marshal(object)
	if err != nil {
		return normalizedCreateRequest{}, errors.New("Qianchuan plan payload could not be normalized")
	}
	request.Payload = normalizedPayload
	digest := sha256.Sum256(normalizedPayload)
	operationKey := request.OperationKey
	if operationKey == "" {
		operationKey = "qianchuan-plan-" + hex.EncodeToString(digest[:16])
	} else if len(operationKey) > 256 {
		return normalizedCreateRequest{}, errors.New("Qianchuan create operation key is invalid")
	}
	return normalizedCreateRequest{
		CreateRequest: request, payloadObject: object, goal: goal, planName: name,
		awemeID: awemeID, productIDs: productIDs,
		operationKey: operationKey,
	}, nil
}

func normalizeCreatePayloadIDs(object map[string]any) error {
	if err := normalizePayloadID(object, "advertiser_id", "advertiser_id", true); err != nil {
		return err
	}
	if err := normalizePayloadID(object, "aweme_id", "aweme_id", false); err != nil {
		return err
	}
	if rawProductIDs, exists := object["product_ids"]; exists {
		productIDs, err := payloadIDs(rawProductIDs, "product_ids", 30)
		if err != nil {
			return err
		}
		object["product_ids"] = numberIDs(productIDs)
	}
	if err := normalizePayloadObjectList(
		object["product_channel_info"], "product_channel_info",
		func(item map[string]any, field string) error {
			if err := normalizePayloadID(item, "product_id", field+".product_id", true); err != nil {
				return err
			}
			return normalizePayloadID(item, "channel_id", field+".channel_id", true)
		},
	); err != nil {
		return err
	}
	if err := normalizePayloadObjectList(
		object["multi_product_creative_list"], "multi_product_creative_list",
		func(item map[string]any, field string) error {
			if err := normalizePayloadID(item, "product_id", field+".product_id", true); err != nil {
				return err
			}
			if err := normalizeAwemeItemIDList(item["video_material"], field+".video_material", false); err != nil {
				return err
			}
			return normalizeAwemeItemIDList(item["block_video_material"], field+".block_video_material", true)
		},
	); err != nil {
		return err
	}
	programmatic, exists := object["programmatic_creative_media_list"].(map[string]any)
	if object["programmatic_creative_media_list"] != nil && !exists {
		return errors.New("Qianchuan programmatic_creative_media_list must be an object")
	}
	if exists {
		if err := normalizeAwemeItemIDList(
			programmatic["video_material"],
			"programmatic_creative_media_list.video_material",
			false,
		); err != nil {
			return err
		}
		if err := normalizeAwemeItemIDList(
			programmatic["block_video_material"],
			"programmatic_creative_media_list.block_video_material",
			true,
		); err != nil {
			return err
		}
	}
	return nil
}

func normalizePayloadObjectList(
	value any,
	field string,
	normalize func(map[string]any, string) error,
) error {
	if value == nil {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return fmt.Errorf("Qianchuan %s must be an array", field)
	}
	for index, value := range items {
		item, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("Qianchuan %s[%d] must be an object", field, index)
		}
		if err := normalize(item, fmt.Sprintf("%s[%d]", field, index)); err != nil {
			return err
		}
	}
	return nil
}

func normalizeAwemeItemIDList(value any, field string, required bool) error {
	return normalizePayloadObjectList(value, field, func(item map[string]any, itemField string) error {
		return normalizePayloadID(item, "aweme_item_id", itemField+".aweme_item_id", required)
	})
}

func normalizePayloadID(object map[string]any, key, field string, required bool) error {
	value, exists := object[key]
	if !exists || value == nil || strings.TrimSpace(fmt.Sprint(value)) == "" {
		if required {
			return fmt.Errorf("Qianchuan %s must be a positive decimal ID", field)
		}
		return nil
	}
	identifier := payloadID(value)
	if !validPositiveID(identifier) {
		return fmt.Errorf("Qianchuan %s must be a positive decimal ID", field)
	}
	object[key] = json.Number(identifier)
	return nil
}

func numberIDs(values []string) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = json.Number(value)
	}
	return result
}

func decodeCreatePayload(payload json.RawMessage) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil || object == nil {
		return nil, errors.New("Qianchuan plan payload must be a JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("Qianchuan plan payload contains trailing JSON")
	}
	allowed := map[string]struct{}{
		"advertiser_id": {}, "name": {}, "aweme_id": {}, "marketing_goal": {},
		"product_ids": {}, "product_channel_info": {}, "delivery_setting": {},
		"creative_setting": {}, "programmatic_creative_media_list": {},
		"multi_product_creative_list": {},
	}
	for field := range object {
		if _, ok := allowed[field]; !ok {
			return nil, fmt.Errorf("Qianchuan plan payload contains unknown field %q", field)
		}
	}
	return object, nil
}

func validateDeliverySetting(delivery map[string]any) error {
	bidType, _ := delivery["smart_bid_type"].(string)
	if bidType != "SMART_BID_CUSTOM" && bidType != "SMART_BID_CONSERVATIVE" {
		return errors.New("Qianchuan delivery_setting.smart_bid_type is unsupported")
	}
	if _, err := positivePayloadDecimal(delivery["budget"], "delivery_setting.budget"); err != nil {
		return err
	}
	if bidType == "SMART_BID_CUSTOM" {
		if _, err := positivePayloadDecimal(delivery["roi2_goal"], "delivery_setting.roi2_goal"); err != nil {
			return err
		}
	} else if delivery["roi2_goal"] != nil {
		return errors.New("Qianchuan conservative bidding rejects roi2_goal")
	}
	return nil
}

func positivePayloadDecimal(value any, field string) (string, error) {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return "", fmt.Errorf("Qianchuan %s is required", field)
	}
	parts := strings.Split(text, ".")
	if strings.ContainsAny(text, "eE") || len(parts) > 2 || parts[0] == "" ||
		(len(parts) == 2 && (parts[1] == "" || len(parts[1]) > 2)) {
		return "", fmt.Errorf("Qianchuan %s must be a positive decimal with at most two decimal places", field)
	}
	for _, part := range parts {
		for _, character := range part {
			if character < '0' || character > '9' {
				return "", fmt.Errorf("Qianchuan %s must be a positive decimal", field)
			}
		}
	}
	allZero := true
	for _, character := range text {
		if character >= '1' && character <= '9' {
			allZero = false
			break
		}
	}
	if allZero {
		return "", fmt.Errorf("Qianchuan %s must be greater than zero", field)
	}
	return text, nil
}

func payloadIDs(value any, field string, maximum int) ([]string, error) {
	values, ok := value.([]any)
	if !ok || len(values) == 0 || len(values) > maximum {
		return nil, fmt.Errorf("Qianchuan %s requires 1 to %d IDs", field, maximum)
	}
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		identifier := payloadID(value)
		if !validPositiveID(identifier) {
			return nil, fmt.Errorf("Qianchuan %s contains an invalid ID", field)
		}
		if _, duplicate := seen[identifier]; duplicate {
			return nil, fmt.Errorf("Qianchuan %s contains duplicate IDs", field)
		}
		seen[identifier] = struct{}{}
		result = append(result, identifier)
	}
	return result, nil
}

func payloadID(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func normalizeIDSet(values []string, field string, maximum int) (map[string]struct{}, []string, error) {
	if len(values) == 0 || len(values) > maximum {
		return nil, nil, fmt.Errorf("Qianchuan %s list requires 1 to %d IDs", field, maximum)
	}
	set := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !validPositiveID(value) {
			return nil, nil, fmt.Errorf("Qianchuan %s must be a positive decimal ID", field)
		}
		if _, duplicate := set[value]; duplicate {
			return nil, nil, fmt.Errorf("Qianchuan %s IDs must be unique", field)
		}
		set[value] = struct{}{}
		result = append(result, value)
	}
	return set, result, nil
}

func validPositiveID(value string) bool {
	if value == "" || value[0] == '0' || len(value) > 19 ||
		(len(value) == 19 && value > "9223372036854775807") {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func weightedLength(value string) int {
	length := 0
	for _, character := range value {
		if character < 128 {
			length++
		} else {
			length += 2
		}
	}
	return length
}

func (request normalizedCreateRequest) preview() CreateResult {
	return CreateResult{
		Mode: "dry_run", Status: "ready", Endpoint: CreateEndpoint,
		AdvertiserID: request.AdvertiserID, Payload: cloneMap(request.payloadObject),
	}
}

func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
