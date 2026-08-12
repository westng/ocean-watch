package marketing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	portmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/marketing"
)

const (
	ProjectCreateEndpoint   = "/v3.0/project/create/"
	PromotionCreateEndpoint = "/v3.0/promotion/create/"
)

type Request struct {
	AdvertiserID     string
	AuthAccountID    string
	Submit           bool
	ProjectPayload   json.RawMessage
	PromotionPayload json.RawMessage
	ResumeProjectID  string
	RecoverProject   bool
	RecoverPromotion bool
	Checkpoint       func(context.Context, Checkpoint) error
}

type Checkpoint struct {
	Status         string
	ProjectID      string
	PromotionID    string
	FailureStage   string
	DispatchState  domainplans.DispatchState
	Reconciliation *domainplans.Reconciliation
	RequestID      string
	LastResponse   *domainplans.OfficialResponse
}

type Result struct {
	Mode              string                        `json:"mode"`
	Status            string                        `json:"status"`
	ProjectEndpoint   string                        `json:"project_endpoint"`
	PromotionEndpoint string                        `json:"promotion_endpoint"`
	ProjectPayload    map[string]any                `json:"project_payload"`
	PromotionPayload  map[string]any                `json:"promotion_payload"`
	ProjectID         string                        `json:"project_id,omitempty"`
	PromotionID       string                        `json:"promotion_id,omitempty"`
	FailureStage      string                        `json:"failure_stage,omitempty"`
	DispatchState     domainplans.DispatchState     `json:"dispatch_state,omitempty"`
	Reconciliation    *domainplans.Reconciliation   `json:"reconciliation,omitempty"`
	RequestID         string                        `json:"request_id,omitempty"`
	LastResponse      *domainplans.OfficialResponse `json:"last_response,omitempty"`
}

type Executor struct {
	Guard      sharedplans.GuardedExecutor
	Writer     portmarketing.PlanWriter
	Reconciler portmarketing.PlanReconciler
}

type normalizedRequest struct {
	Request
	projectName     string
	promotionName   string
	projectObject   map[string]any
	promotionObject map[string]any
	operationKey    string
}

func (executor Executor) Execute(ctx context.Context, request Request) (Result, error) {
	normalized, err := normalizeRequest(request)
	if err != nil {
		return Result{}, err
	}
	preview := normalized.preview()
	guarded, err := executor.Guard.Execute(ctx, sharedplans.GuardedMutation{
		Scope: domainplans.WriteScope{
			Channel: domainplans.ChannelMarketing, AdvertiserID: normalized.AdvertiserID,
			LockFamily: domainplans.LockMarketingPlans,
		},
		AuthAccountID: normalized.AuthAccountID,
		Submit:        normalized.Submit,
		Validate: func() error {
			if executor.Writer == nil && normalized.Submit {
				return errors.New("Marketing plan writer is required")
			}
			return nil
		},
		Preview: preview,
	}, func(ctx context.Context, execution sharedplans.MutationExecution) (any, error) {
		return executor.executeTransaction(ctx, normalized, execution)
	})
	if guarded.Mode == "dry_run" {
		return preview, err
	}
	result, ok := guarded.Value.(Result)
	if !ok {
		if err != nil {
			return Result{}, err
		}
		return Result{}, errors.New("Marketing plan executor returned an invalid result")
	}
	return result, err
}

func (executor Executor) executeTransaction(
	ctx context.Context,
	request normalizedRequest,
	execution sharedplans.MutationExecution,
) (Result, error) {
	result := request.preview()
	result.Mode = "submit"
	projectID := request.ResumeProjectID
	if request.RecoverProject {
		created, reconciliation, err := executor.reconcileProject(ctx, request, execution)
		result.Reconciliation = reconciliation
		if err != nil {
			result.Status = "failed"
			result.FailureStage = "project_reconciliation"
			result.LastResponse = domainplans.OfficialResponseFromError(err)
			if checkpointErr := request.emitCheckpoint(ctx, result); checkpointErr != nil {
				return result, errors.Join(err, checkpointErr)
			}
			return result, err
		}
		projectID = created.ObjectID
		result.ProjectID = projectID
		if err := request.emitCheckpoint(ctx, Result{
			Status: "project_created", ProjectID: projectID,
			Reconciliation: reconciliation,
		}); err != nil {
			result.Status = "failed"
			result.FailureStage = "journal_checkpoint"
			return result, err
		}
	}
	if projectID == "" {
		if err := request.emitCheckpoint(ctx, Result{Status: "project_dispatching"}); err != nil {
			result.Status = "failed"
			result.FailureStage = "journal_checkpoint"
			return result, err
		}
		receipt := execution.Dispatcher.Dispatch(
			ctx, execution.Capability, request.operationKey+"/project",
			func(ctx context.Context) (any, error) {
				return executor.Writer.CreateProject(ctx, portmarketing.ProjectCreateRequest{
					AdvertiserID: request.AdvertiserID,
					AccessToken:  execution.AccessToken,
					Payload:      append(json.RawMessage(nil), request.ProjectPayload...),
				})
			},
		)
		created, reconciliation, err := executor.resolveProject(ctx, request, execution, receipt)
		if reconciliation != nil {
			result.Reconciliation = reconciliation
		}
		result.DispatchState = receipt.State
		if err != nil {
			result.Status = "failed"
			result.FailureStage = "project_create"
			result.LastResponse = domainplans.OfficialResponseFromError(err)
			if checkpointErr := request.emitCheckpoint(ctx, result); checkpointErr != nil {
				return result, errors.Join(err, checkpointErr)
			}
			return result, err
		}
		projectID = created.ObjectID
		result.RequestID = created.RequestID
		result.ProjectID = projectID
		if err := request.emitCheckpoint(ctx, Result{
			Status: "project_created", ProjectID: projectID, RequestID: created.RequestID,
			DispatchState: receipt.State, Reconciliation: reconciliation,
		}); err != nil {
			result.Status = "failed"
			result.FailureStage = "journal_checkpoint"
			return result, err
		}
	}
	result.ProjectID = projectID
	if request.RecoverPromotion {
		created, reconciliation, err := executor.reconcilePromotion(
			ctx, request, execution, projectID,
		)
		result.Reconciliation = reconciliation
		if err != nil {
			result.Status = "failed"
			result.FailureStage = "promotion_reconciliation"
			result.LastResponse = domainplans.OfficialResponseFromError(err)
			if checkpointErr := request.emitCheckpoint(ctx, result); checkpointErr != nil {
				return result, errors.Join(err, checkpointErr)
			}
			return result, err
		}
		result.Status = "completed"
		result.PromotionID = created.ObjectID
		if err := request.emitCheckpoint(ctx, result); err != nil {
			result.Status = "failed"
			result.FailureStage = "journal_checkpoint"
			return result, err
		}
		return result, nil
	}
	if err := request.emitCheckpoint(ctx, Result{
		Status: "promotion_dispatching", ProjectID: projectID,
	}); err != nil {
		result.Status = "failed"
		result.FailureStage = "journal_checkpoint"
		return result, err
	}

	receipt := execution.Dispatcher.Dispatch(
		ctx, execution.Capability, request.operationKey+"/promotion",
		func(ctx context.Context) (any, error) {
			promotionPayload, err := request.promotionPayload(projectID)
			if err != nil {
				return nil, &domainplans.DispatchFailure{
					State: domainplans.DispatchNotSent, Cause: err,
				}
			}
			return executor.Writer.CreatePromotion(ctx, portmarketing.PromotionCreateRequest{
				AdvertiserID: request.AdvertiserID,
				AccessToken:  execution.AccessToken,
				ProjectID:    projectID,
				Payload:      promotionPayload,
			})
		},
	)
	created, reconciliation, err := executor.resolvePromotion(
		ctx, request, execution, projectID, receipt,
	)
	if reconciliation != nil {
		result.Reconciliation = reconciliation
	}
	result.DispatchState = receipt.State
	if err != nil {
		result.Status = "failed"
		result.FailureStage = "promotion_create"
		result.LastResponse = domainplans.OfficialResponseFromError(err)
		if checkpointErr := request.emitCheckpoint(ctx, result); checkpointErr != nil {
			return result, errors.Join(err, checkpointErr)
		}
		return result, err
	}
	result.Status = "completed"
	result.PromotionID = created.ObjectID
	result.RequestID = created.RequestID
	if err := request.emitCheckpoint(ctx, result); err != nil {
		result.Status = "failed"
		result.FailureStage = "journal_checkpoint"
		return result, err
	}
	return result, nil
}

func (executor Executor) reconcileProject(
	ctx context.Context,
	request normalizedRequest,
	execution sharedplans.MutationExecution,
) (portmarketing.CreateResult, *domainplans.Reconciliation, error) {
	if executor.Reconciler == nil {
		return portmarketing.CreateResult{}, nil, errors.New("Marketing project recovery requires a reconciler")
	}
	candidates, err := executor.Reconciler.FindProjects(ctx, portmarketing.ProjectReconciliationRequest{
		AdvertiserID: request.AdvertiserID,
		AccessToken:  execution.AccessToken,
		Name:         request.projectName,
	})
	if err != nil {
		return portmarketing.CreateResult{}, nil, fmt.Errorf("recover Marketing project: %w", err)
	}
	reconciliation, err := domainplans.ReconcileCandidates(candidates)
	if err != nil {
		return portmarketing.CreateResult{}, nil, err
	}
	if reconciliation.State != domainplans.ReconciliationApplied {
		return portmarketing.CreateResult{}, &reconciliation, fmt.Errorf(
			"Marketing project recovery is %s", reconciliation.State,
		)
	}
	return portmarketing.CreateResult{ObjectID: reconciliation.ObjectID}, &reconciliation, nil
}

func (executor Executor) reconcilePromotion(
	ctx context.Context,
	request normalizedRequest,
	execution sharedplans.MutationExecution,
	projectID string,
) (portmarketing.CreateResult, *domainplans.Reconciliation, error) {
	if executor.Reconciler == nil {
		return portmarketing.CreateResult{}, nil, errors.New("Marketing promotion recovery requires a reconciler")
	}
	candidates, err := executor.Reconciler.FindPromotions(ctx, portmarketing.PromotionReconciliationRequest{
		AdvertiserID: request.AdvertiserID,
		AccessToken:  execution.AccessToken,
		ProjectID:    projectID,
		Name:         request.promotionName,
	})
	if err != nil {
		return portmarketing.CreateResult{}, nil, fmt.Errorf("recover Marketing promotion: %w", err)
	}
	reconciliation, err := domainplans.ReconcileCandidates(candidates)
	if err != nil {
		return portmarketing.CreateResult{}, nil, err
	}
	if reconciliation.State != domainplans.ReconciliationApplied {
		return portmarketing.CreateResult{}, &reconciliation, fmt.Errorf(
			"Marketing promotion recovery is %s", reconciliation.State,
		)
	}
	return portmarketing.CreateResult{ObjectID: reconciliation.ObjectID}, &reconciliation, nil
}

func (executor Executor) resolveProject(
	ctx context.Context,
	request normalizedRequest,
	execution sharedplans.MutationExecution,
	receipt sharedplans.DispatchReceipt,
) (portmarketing.CreateResult, *domainplans.Reconciliation, error) {
	if receipt.Error == nil {
		return createResult(receipt.Value, "project")
	}
	if receipt.State != domainplans.DispatchUnknown {
		return portmarketing.CreateResult{}, nil, receipt.Error
	}
	if executor.Reconciler == nil {
		return portmarketing.CreateResult{}, nil, errors.New("Marketing project result is unknown and no reconciler is configured")
	}
	candidates, err := executor.Reconciler.FindProjects(ctx, portmarketing.ProjectReconciliationRequest{
		AdvertiserID: request.AdvertiserID,
		AccessToken:  execution.AccessToken,
		Name:         request.projectName,
	})
	if err != nil {
		return portmarketing.CreateResult{}, nil, fmt.Errorf("reconcile Marketing project: %w", err)
	}
	reconciliation, err := domainplans.ReconcileCandidates(candidates)
	if err != nil {
		return portmarketing.CreateResult{}, nil, err
	}
	if reconciliation.State != domainplans.ReconciliationApplied {
		return portmarketing.CreateResult{}, &reconciliation, fmt.Errorf(
			"Marketing project result is %s after reconciliation", reconciliation.State,
		)
	}
	return portmarketing.CreateResult{ObjectID: reconciliation.ObjectID}, &reconciliation, nil
}

func (executor Executor) resolvePromotion(
	ctx context.Context,
	request normalizedRequest,
	execution sharedplans.MutationExecution,
	projectID string,
	receipt sharedplans.DispatchReceipt,
) (portmarketing.CreateResult, *domainplans.Reconciliation, error) {
	if receipt.Error == nil {
		return createResult(receipt.Value, "promotion")
	}
	if receipt.State != domainplans.DispatchUnknown {
		return portmarketing.CreateResult{}, nil, receipt.Error
	}
	if executor.Reconciler == nil {
		return portmarketing.CreateResult{}, nil, errors.New("Marketing promotion result is unknown and no reconciler is configured")
	}
	candidates, err := executor.Reconciler.FindPromotions(ctx, portmarketing.PromotionReconciliationRequest{
		AdvertiserID: request.AdvertiserID,
		AccessToken:  execution.AccessToken,
		ProjectID:    projectID,
		Name:         request.promotionName,
	})
	if err != nil {
		return portmarketing.CreateResult{}, nil, fmt.Errorf("reconcile Marketing promotion: %w", err)
	}
	reconciliation, err := domainplans.ReconcileCandidates(candidates)
	if err != nil {
		return portmarketing.CreateResult{}, nil, err
	}
	if reconciliation.State != domainplans.ReconciliationApplied {
		return portmarketing.CreateResult{}, &reconciliation, fmt.Errorf(
			"Marketing promotion result is %s after reconciliation", reconciliation.State,
		)
	}
	return portmarketing.CreateResult{ObjectID: reconciliation.ObjectID}, &reconciliation, nil
}

func createResult(value any, object string) (portmarketing.CreateResult, *domainplans.Reconciliation, error) {
	result, ok := value.(portmarketing.CreateResult)
	if !ok || !validPositiveID(result.ObjectID) {
		return portmarketing.CreateResult{}, nil, fmt.Errorf("Marketing %s response is missing a valid object ID", object)
	}
	return result, nil, nil
}

func normalizeRequest(request Request) (normalizedRequest, error) {
	request.AdvertiserID = strings.TrimSpace(request.AdvertiserID)
	request.AuthAccountID = strings.TrimSpace(request.AuthAccountID)
	request.ResumeProjectID = strings.TrimSpace(request.ResumeProjectID)
	if !validPositiveID(request.AdvertiserID) {
		return normalizedRequest{}, errors.New("advertiser_id must be a positive decimal ID")
	}
	if request.ResumeProjectID != "" && !validPositiveID(request.ResumeProjectID) {
		return normalizedRequest{}, errors.New("resume project_id must be a positive decimal ID")
	}
	if request.RecoverProject && request.ResumeProjectID != "" {
		return normalizedRequest{}, errors.New("project recovery cannot include a resume project_id")
	}
	if request.RecoverPromotion && request.ResumeProjectID == "" {
		return normalizedRequest{}, errors.New("promotion recovery requires a resume project_id")
	}
	project, err := decodePayload(request.ProjectPayload, "project")
	if err != nil {
		return normalizedRequest{}, err
	}
	promotion, err := decodePayload(request.PromotionPayload, "promotion")
	if err != nil {
		return normalizedRequest{}, err
	}
	projectName, err := requiredPayloadString(project, "name", "project")
	if err != nil {
		return normalizedRequest{}, err
	}
	promotionName, err := requiredPayloadString(promotion, "name", "promotion")
	if err != nil {
		return normalizedRequest{}, err
	}
	if err := requirePayloadID(project, "advertiser_id", request.AdvertiserID, "project"); err != nil {
		return normalizedRequest{}, err
	}
	if err := requirePayloadID(promotion, "advertiser_id", request.AdvertiserID, "promotion"); err != nil {
		return normalizedRequest{}, err
	}
	if request.ResumeProjectID != "" {
		value := strings.TrimSpace(fmt.Sprint(promotion["project_id"]))
		if value != "" && value != "<nil>" && value != "{{project_id}}" && value != request.ResumeProjectID {
			return normalizedRequest{}, errors.New("promotion project_id does not match resume project_id")
		}
	}
	digest := sha256.Sum256([]byte(request.AdvertiserID + "\x00" + projectName + "\x00" + promotionName))
	return normalizedRequest{
		Request: request, projectName: projectName, promotionName: promotionName,
		projectObject: project, promotionObject: promotion,
		operationKey: "marketing-plan-" + hex.EncodeToString(digest[:16]),
	}, nil
}

func (request normalizedRequest) emitCheckpoint(ctx context.Context, result Result) error {
	if request.Checkpoint == nil {
		return nil
	}
	status := result.Status
	if status == "failed" {
		status = checkpointFailureStatus(result)
	}
	return request.Checkpoint(ctx, Checkpoint{
		Status: status, ProjectID: result.ProjectID, PromotionID: result.PromotionID,
		FailureStage: result.FailureStage, DispatchState: result.DispatchState,
		Reconciliation: result.Reconciliation, RequestID: result.RequestID,
		LastResponse: result.LastResponse,
	})
}

func checkpointFailureStatus(result Result) string {
	if result.Reconciliation != nil && result.Reconciliation.State == domainplans.ReconciliationAmbiguous {
		return "ambiguous"
	}
	if strings.HasPrefix(result.FailureStage, "promotion_") || result.ProjectID != "" {
		return "promotion_failed"
	}
	return "project_failed"
}

func (request normalizedRequest) preview() Result {
	promotion := cloneObject(request.promotionObject)
	if request.ResumeProjectID != "" {
		promotion["project_id"] = request.ResumeProjectID
	}
	return Result{
		Mode: "dry_run", Status: "ready",
		ProjectEndpoint: ProjectCreateEndpoint, PromotionEndpoint: PromotionCreateEndpoint,
		ProjectPayload: cloneObject(request.projectObject), PromotionPayload: promotion,
		ProjectID: request.ResumeProjectID,
	}
}

func (request normalizedRequest) promotionPayload(projectID string) (json.RawMessage, error) {
	promotion := cloneObject(request.promotionObject)
	promotion["project_id"] = json.Number(projectID)
	payload, err := json.Marshal(promotion)
	if err != nil {
		return nil, fmt.Errorf("encode Marketing promotion payload: %w", err)
	}
	return payload, nil
}

func decodePayload(payload json.RawMessage, label string) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%s payload must be a JSON object: %w", label, err)
	}
	if value == nil {
		return nil, fmt.Errorf("%s payload must be a JSON object", label)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return nil, fmt.Errorf("%s payload contains trailing JSON", label)
	}
	return value, nil
}

func requiredPayloadString(payload map[string]any, field, label string) (string, error) {
	value, ok := payload[field].(string)
	value = strings.TrimSpace(value)
	if !ok || value == "" || len(value) > 256 {
		return "", fmt.Errorf("%s payload %s is required", label, field)
	}
	return value, nil
}

func requirePayloadID(payload map[string]any, field, expected, label string) error {
	value := strings.TrimSpace(fmt.Sprint(payload[field]))
	if value != expected {
		return fmt.Errorf("%s payload %s does not match command scope", label, field)
	}
	return nil
}

func validPositiveID(value string) bool {
	if value == "" || value[0] == '0' {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	return err == nil && parsed > 0
}

func cloneObject(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}
