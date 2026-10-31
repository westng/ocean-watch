package auth

import (
	"errors"
	"regexp"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

const (
	failureAppCredentials       = "app_credentials"
	failureTokenExchange        = "token_exchange"
	failureAuthorizedUser       = "authorized_user"
	failureCredentialStorage    = "credential_storage"
	failureAuthorizationStorage = "authorization_storage"
	failureAccountSync          = "account_sync"
)

// Keep adapter error strings and provider messages out of retained sessions.
// Adapters may supply only classified reasons and official trace metadata.
type authorizationFailureReason interface {
	error
	AuthorizationFailureReason() string
}

type authorizationFailureMetadata interface {
	error
	AuthorizationFailureMetadata() (code int64, requestID string, httpStatus int)
}

type authorizationStepError struct {
	stage string
	cause error
}

func (err *authorizationStepError) Error() string { return "authorization processing failed" }
func (err *authorizationStepError) Unwrap() error { return err.cause }

func atAuthorizationStep(stage string, err error) error {
	if err == nil {
		return nil
	}
	return &authorizationStepError{stage: stage, cause: err}
}

var officialAuthorizationRequestID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func (view *RecoverySession) retainFailure(err error, fallbackStage string) {
	view.FailureStage = fallbackStage
	var step *authorizationStepError
	if errors.As(err, &step) {
		view.FailureStage = step.stage
	}
	view.FailureReason = map[string]string{
		failureAppCredentials:       "APP_CREDENTIAL_READ_FAILED",
		failureTokenExchange:        "TOKEN_EXCHANGE_FAILED",
		failureAuthorizedUser:       "USER_INFO_FAILED",
		failureCredentialStorage:    "CREDENTIAL_WRITE_FAILED",
		failureAuthorizationStorage: "AUTHORIZATION_STORE_FAILED",
		failureAccountSync:          "ACCOUNT_SYNC_FAILED",
	}[view.FailureStage]
	if view.FailureReason == "" {
		view.FailureStage, view.FailureReason = "", ""
		return
	}
	var classified authorizationFailureReason
	if errors.As(err, &classified) {
		if reason := classified.AuthorizationFailureReason(); allowedAuthorizationFailureReason(reason) {
			view.FailureReason = reason
		}
	}
	var domainErr *domain.Error
	if errors.As(err, &domainErr) {
		switch domainErr.Code {
		case "authorized_user_invalid":
			view.FailureReason = "INVALID_USER_INFO_RESPONSE"
		case "authorization_identity_mismatch":
			view.FailureReason = "AUTHORIZATION_IDENTITY_MISMATCH"
		case "authorization_not_found":
			view.FailureReason = "AUTHORIZATION_NOT_FOUND"
		case "authorization_ambiguous":
			view.FailureReason = "AUTHORIZATION_AMBIGUOUS"
		}
	}
	var official authorizationFailureMetadata
	if errors.As(err, &official) {
		code, requestID, status := official.AuthorizationFailureMetadata()
		if code > 0 {
			view.OfficialCode = code
		}
		if officialAuthorizationRequestID.MatchString(requestID) {
			view.OfficialRequestID = requestID
		}
		if status >= 100 && status <= 599 {
			view.HTTPStatus = status
		}
	}
}

func allowedAuthorizationFailureReason(reason string) bool {
	switch reason {
	case "OFFICIAL_API_ERROR", "OFFICIAL_HTTP_ERROR", "REQUEST_FAILED", "INVALID_TOKEN_RESPONSE", "CREDENTIAL_WRITE_FAILED", "CREDENTIAL_VERIFY_FAILED",
		"INVALID_USER_INFO_RESPONSE", "AUTHORIZATION_IDENTITY_MISMATCH", "AUTHORIZATION_NOT_FOUND", "AUTHORIZATION_AMBIGUOUS",
		"ACCOUNT_DISCOVERY_FAILED", "ACCOUNT_SNAPSHOT_INVALID", "ACCOUNT_ACTIVATION_FAILED", "ACCOUNT_MAPPING_CONFLICT":
		return true
	}
	return false
}

// FailureDiagnostics is an allowlist projection for transports whose optional
// metadata is outside their structured output schema.
func (view RecoverySession) FailureDiagnostics() map[string]any {
	defaultReason := map[string]string{
		failureAppCredentials: "APP_CREDENTIAL_READ_FAILED", failureTokenExchange: "TOKEN_EXCHANGE_FAILED", failureAuthorizedUser: "USER_INFO_FAILED",
		failureCredentialStorage: "CREDENTIAL_WRITE_FAILED", failureAuthorizationStorage: "AUTHORIZATION_STORE_FAILED", failureAccountSync: "ACCOUNT_SYNC_FAILED",
	}[view.FailureStage]
	if defaultReason == "" {
		return nil
	}
	reason := defaultReason
	if allowedAuthorizationFailureReason(view.FailureReason) {
		reason = view.FailureReason
	}
	result := map[string]any{"failure_stage": view.FailureStage, "failure_reason": reason}
	if view.OfficialCode > 0 {
		result["official_error_code"] = view.OfficialCode
	}
	if officialAuthorizationRequestID.MatchString(view.OfficialRequestID) {
		result["official_request_id"] = view.OfficialRequestID
	}
	if view.HTTPStatus >= 100 && view.HTTPStatus <= 599 {
		result["http_status"] = view.HTTPStatus
	}
	return result
}
