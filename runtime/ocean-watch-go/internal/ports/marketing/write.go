package marketing

import (
	"context"
	"encoding/json"
)

type ProjectCreateRequest struct {
	AdvertiserID string
	AccessToken  string
	Payload      json.RawMessage
}

type PromotionCreateRequest struct {
	AdvertiserID string
	AccessToken  string
	ProjectID    string
	Payload      json.RawMessage
}

type CreateResult struct {
	ObjectID  string
	RequestID string
}

type ProjectReconciliationRequest struct {
	AdvertiserID string
	AccessToken  string
	Name         string
}

type PromotionReconciliationRequest struct {
	AdvertiserID string
	AccessToken  string
	ProjectID    string
	Name         string
}

type PlanWriter interface {
	CreateProject(context.Context, ProjectCreateRequest) (CreateResult, error)
	CreatePromotion(context.Context, PromotionCreateRequest) (CreateResult, error)
}

type PlanReconciler interface {
	FindProjects(context.Context, ProjectReconciliationRequest) ([]string, error)
	FindPromotions(context.Context, PromotionReconciliationRequest) ([]string, error)
}
