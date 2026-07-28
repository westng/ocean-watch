package qianchuan

import (
	"context"
	"encoding/json"
)

type CreatePlanRequest struct {
	AdvertiserID string
	AccessToken  string
	Payload      json.RawMessage
}

type MaterialWriteRequest struct {
	AdvertiserID string
	AccessToken  string
	AdID         string
	Payload      json.RawMessage
}

type DeleteMaterialsRequest struct {
	AdvertiserID string
	AccessToken  string
	AdID         string
	MaterialIDs  []string
}

type MutationKind string

const (
	MutationStatus MutationKind = "status"
	MutationBudget MutationKind = "budget"
	MutationROI    MutationKind = "roi"
)

type MutationRequest struct {
	AdvertiserID       string
	AccessToken        string
	Kind               MutationKind
	AdIDs              []string
	Status             string
	Value              string
	DeepExternalAction string
}

type RowError struct {
	ObjectID string `json:"object_id"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message"`
}

type WriteResult struct {
	ObjectID  string     `json:"object_id,omitempty"`
	RequestID string     `json:"request_id,omitempty"`
	RowErrors []RowError `json:"row_errors"`
}

type Writer interface {
	CreatePlan(context.Context, CreatePlanRequest) (WriteResult, error)
	AddMaterials(context.Context, MaterialWriteRequest) (WriteResult, error)
	DeleteMaterials(context.Context, DeleteMaterialsRequest) (WriteResult, error)
	UpdatePlan(context.Context, MutationRequest) (WriteResult, error)
}
