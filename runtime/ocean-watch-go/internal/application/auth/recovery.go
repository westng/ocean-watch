package auth

import "context"

// RecoverySession exposes safe lifecycle metadata. Callback codes and credentials
// remain inside the recovery flow and must never be added to this presenter.
type RecoverySession struct {
	SessionID         string `json:"session_id"`
	Channel           string `json:"channel"`
	Operation         string `json:"operation"`
	Status            string `json:"status"`
	AuthorizationURL  string `json:"authorization_url,omitempty"`
	ExpiresAt         string `json:"expires_at"`
	AdvertiserID      string `json:"advertiser_id,omitempty"`
	AuthorizationID   string `json:"authorization_id,omitempty"`
	AccountCount      int    `json:"authorized_account_count"`
	AdvertiserCount   int    `json:"authorized_advertiser_count"`
	CanResume         bool   `json:"can_resume"`
	SyncRequired      bool   `json:"sync_required"`
	ErrorCode         string `json:"error_code,omitempty"`
	FailureStage      string `json:"failure_stage,omitempty"`
	FailureReason     string `json:"failure_reason,omitempty"`
	OfficialCode      int64  `json:"official_error_code,omitempty"`
	OfficialRequestID string `json:"official_request_id,omitempty"`
	HTTPStatus        int    `json:"http_status,omitempty"`
}

type RecoveryRequest struct {
	Channel         string
	AdvertiserID    string
	AuthorizationID string
	// Operation is one of authorize, reauthorize, or sync_accounts. An empty
	// value preserves the legacy inference used by direct callers: an
	// authorization ID means account sync, otherwise a new authorization.
	Operation string
	// RebindExisting must be explicitly requested by the caller. It never
	// defaults to true because an OAuth grant must not silently take ownership
	// of another grant's account scope.
	RebindExisting bool
}

type Recovery interface {
	Start(context.Context, RecoveryRequest) (RecoverySession, error)
	Get(context.Context, string, string) (RecoverySession, error)
	Cancel(context.Context, string, string) (RecoverySession, error)
}

// InvalidRefreshError lets adapters identify a terminal refresh failure without
// coupling the application service to a provider's transport implementation.
type InvalidRefreshError interface {
	error
	RequiresReauthorization() bool
}
