package marketing

import "context"

type MutationKind string

const (
	MutationProjectStatus   MutationKind = "project_status"
	MutationPromotionStatus MutationKind = "promotion_status"
	MutationPromotionBudget MutationKind = "promotion_budget"
	MutationPromotionBid    MutationKind = "promotion_bid"
	MutationProjectROI      MutationKind = "project_roi"
)

type MutationRequest struct {
	AdvertiserID string
	AccessToken  string
	Kind         MutationKind
	ObjectIDs    []string
	Status       string
	Value        string
}

type MutationWriteResult struct {
	RequestID string
	RowErrors map[string]string
}

type MutationSnapshot struct {
	ObjectID string
	Status   string
	Value    string
}

type PlanMutationWriter interface {
	ApplyMutation(context.Context, MutationRequest) (MutationWriteResult, error)
}

type PlanMutationReader interface {
	ReadMutation(context.Context, MutationRequest) ([]MutationSnapshot, error)
}
