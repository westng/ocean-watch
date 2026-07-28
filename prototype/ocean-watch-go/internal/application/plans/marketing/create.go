package marketing

import (
	"context"
	"errors"
)

type TransactionExecutor interface {
	Execute(context.Context, Request) (Result, error)
}

type CreateRequest struct {
	PrepareRequest
	PromotionOnly bool
}

type CreateResult struct {
	Mode string `json:"mode"`
	PreparedPlan
	SubmitBlocked  bool           `json:"submit_blocked,omitempty"`
	BlockingFields []string       `json:"blocking_fields,omitempty"`
	Status         string         `json:"status,omitempty"`
	Error          string         `json:"error,omitempty"`
	ProjectID      string         `json:"project_id,omitempty"`
	PromotionID    string         `json:"promotion_id,omitempty"`
	FailureStage   string         `json:"failure_stage,omitempty"`
	DispatchState  string         `json:"dispatch_state,omitempty"`
	Reconciliation map[string]any `json:"reconciliation,omitempty"`
	RequestID      string         `json:"request_id,omitempty"`
}

type CreateService struct {
	Preparer Preparer
	Executor TransactionExecutor
}

func (service CreateService) Execute(
	ctx context.Context,
	request CreateRequest,
) (CreateResult, error) {
	prepared, err := service.Preparer.Prepare(ctx, request.PrepareRequest)
	if err != nil {
		return CreateResult{}, err
	}
	result := CreateResult{Mode: "dry_run", PreparedPlan: prepared}
	if !request.Submit {
		return result, nil
	}
	result.Mode = "submit"
	blocking := append([]string(nil), prepared.MissingFields...)
	if request.PromotionOnly && request.ProjectID == "" {
		blocking = appendUniqueString(blocking, "project_id")
	}
	if len(blocking) != 0 {
		result.SubmitBlocked = true
		result.BlockingFields = blocking
		result.Status = "blocked"
		return result, nil
	}
	if service.Executor == nil {
		return CreateResult{}, errors.New("Marketing transaction executor is required for submit")
	}
	executionRequest := Request{
		AdvertiserID: prepared.AdvertiserID, AuthAccountID: prepared.AuthAccountID,
		Submit: true, ProjectPayload: prepared.ProjectJSON, PromotionPayload: prepared.PromotionJSON,
	}
	if request.PromotionOnly {
		executionRequest.ResumeProjectID = request.ProjectID
	}
	execution, err := service.Executor.Execute(ctx, executionRequest)
	result.Status = execution.Status
	result.ProjectID = execution.ProjectID
	result.PromotionID = execution.PromotionID
	result.FailureStage = execution.FailureStage
	result.DispatchState = string(execution.DispatchState)
	result.RequestID = execution.RequestID
	if err != nil {
		result.Error = err.Error()
	}
	if execution.Reconciliation != nil {
		result.Reconciliation = map[string]any{
			"state":      execution.Reconciliation.State,
			"object_id":  execution.Reconciliation.ObjectID,
			"candidates": execution.Reconciliation.Candidates,
			"reason":     execution.Reconciliation.Reason,
		}
	}
	return result, err
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
