package auth

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type RecoveryCoordinator struct {
	mu       sync.Mutex
	view     RecoverySession
	input    RecoveryRequest
	state    string
	flow     Service
	ctx      context.Context
	cancel   context.CancelFunc
	started  bool
	stopping string
	stopCode string
	result   AuthorizationResult
	done     chan struct{}
}

func NewRecoveryCoordinator(parent context.Context, input RecoveryRequest, view RecoverySession, state string, lifetime time.Duration, flow Service) *RecoveryCoordinator {
	ctx, cancel := context.WithTimeout(parent, lifetime)
	coordinator := &RecoveryCoordinator{view: view, input: input, state: state, flow: flow, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	go func() {
		select {
		case <-ctx.Done():
			coordinator.RequestCancel()
		case <-coordinator.done:
		}
	}()
	return coordinator
}

func (coordinator *RecoveryCoordinator) View() RecoverySession {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	return coordinator.view
}

func (coordinator *RecoveryCoordinator) Result() AuthorizationResult {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	result := coordinator.result
	result.AdvertiserIDs = append([]string(nil), result.AdvertiserIDs...)
	result.DiscoveryIssues = append([]domain.AccountDiscoveryIssue(nil), result.DiscoveryIssues...)
	return result
}

func (coordinator *RecoveryCoordinator) Done() <-chan struct{} { return coordinator.done }

func (coordinator *RecoveryCoordinator) AcceptCallback(state, code, providerError string) error {
	if err := domain.ValidateOAuthCallbackState(state, coordinator.state, coordinator.input.Channel); err != nil {
		return err
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if coordinator.started || coordinator.stopping != "" || coordinator.ctx.Err() != nil || coordinator.view.Status != "awaiting_callback" {
		return domain.NewError("authorization_callback_used", "authorization callback is already claimed or expired", 1, nil)
	}
	coordinator.view.AuthorizationURL = ""
	if code == "" || len(code) > 2048 || providerError != "" {
		coordinator.finishLocked(AuthorizationResult{}, domain.NewError("oauth_rejected", "official authorization was not completed", 1, nil))
		return nil
	}
	coordinator.started, coordinator.view.Status = true, "authorizing"
	go func() {
		defer close(coordinator.done)
		coordinator.authorize(code)
	}()
	return nil
}

func (coordinator *RecoveryCoordinator) StartSync() error {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if coordinator.started || coordinator.stopping != "" || coordinator.ctx.Err() != nil || coordinator.view.Status != "awaiting_user" || coordinator.input.Operation != "sync_accounts" {
		return domain.NewError("authorization_callback_used", "account synchronization is already claimed or expired", 1, nil)
	}
	coordinator.started, coordinator.view.Status, coordinator.view.AuthorizationURL = true, "syncing", ""
	go func() {
		defer close(coordinator.done)
		result, err := coordinator.flow.Sync(coordinator.ctx, coordinator.input)
		coordinator.mu.Lock()
		defer coordinator.mu.Unlock()
		coordinator.finishLocked(result, err)
	}()
	return nil
}

func (coordinator *RecoveryCoordinator) RequestCancel() { coordinator.stop("") }

func (coordinator *RecoveryCoordinator) FailService() {
	coordinator.stop("AUTHORIZATION_SERVICE_FAILED")
}

func (coordinator *RecoveryCoordinator) stop(code string) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if RecoveryTerminal(coordinator.view.Status) || coordinator.stopping != "" {
		return
	}
	coordinator.stopping, coordinator.stopCode = "cancelled", code
	if errors.Is(coordinator.ctx.Err(), context.DeadlineExceeded) {
		coordinator.stopping = "expired"
	}
	if code != "" {
		coordinator.stopping = "failed"
	}
	coordinator.view.AuthorizationURL = ""
	coordinator.cancel()
	if !coordinator.started {
		coordinator.finishLocked(AuthorizationResult{}, coordinator.ctx.Err())
	}
}

func (coordinator *RecoveryCoordinator) authorize(code string) {
	result, err := coordinator.flow.Authorize(coordinator.ctx, coordinator.input, code, func(phase string) {
		coordinator.mu.Lock()
		defer coordinator.mu.Unlock()
		if coordinator.stopping == "" {
			coordinator.view.Status = phase
		}
	})
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	coordinator.finishLocked(result, err)
}

func (coordinator *RecoveryCoordinator) finishLocked(result AuthorizationResult, err error) {
	if RecoveryTerminal(coordinator.view.Status) {
		return
	}
	coordinator.result = result
	if result.AuthorizationID != "" {
		coordinator.view.AuthorizationID = result.AuthorizationID
	}
	coordinator.view.AuthorizationURL, coordinator.view.CanResume = "", false
	coordinator.view.SyncRequired = result.PendingAccountSync
	coordinator.view.Status, coordinator.view.ErrorCode = "failed", "AUTHORIZATION_FAILED"
	if err == nil {
		coordinator.view.AccountCount, coordinator.view.AdvertiserCount = result.AuthorizedAccounts, len(result.AdvertiserIDs)
		coordinator.view.DiscoveryIssueCount = len(result.DiscoveryIssues)
		coordinator.view.CanResume = coordinator.input.AdvertiserID == ""
		for _, id := range result.AdvertiserIDs {
			if id == coordinator.input.AdvertiserID {
				coordinator.view.CanResume = true
			}
		}
		coordinator.view.Status, coordinator.view.ErrorCode = "completed", ""
		if !coordinator.view.CanResume {
			coordinator.view.Status, coordinator.view.ErrorCode = "failed", "AUTHORIZATION_SCOPE_MISSING"
		}
	} else if coordinator.stopping != "" || coordinator.ctx.Err() != nil {
		coordinator.view.Status, coordinator.view.ErrorCode = coordinator.stopping, coordinator.stopCode
		if coordinator.view.Status == "" {
			coordinator.view.Status = "cancelled"
			if errors.Is(coordinator.ctx.Err(), context.DeadlineExceeded) {
				coordinator.view.Status = "expired"
			}
		}
		if coordinator.view.ErrorCode == "" {
			coordinator.view.ErrorCode = "AUTHORIZATION_CANCELLED"
			if coordinator.view.Status == "expired" {
				coordinator.view.ErrorCode = "AUTHORIZATION_EXPIRED"
			}
		}
	} else {
		coordinator.view.retainFailure(err, "")
		if coordinator.input.Operation == "sync_accounts" || coordinator.view.SyncRequired {
			coordinator.view.ErrorCode = "ACCOUNT_SYNC_FAILED"
		}
		var invalidToken InvalidRefreshError
		if errors.As(err, &invalidToken) && invalidToken.RequiresReauthorization() {
			coordinator.view.ErrorCode = "REAUTHORIZATION_REQUIRED"
		}
		var domainErr *domain.Error
		if errors.As(err, &domainErr) {
			switch domainErr.Code {
			case "reauthorization_required":
				coordinator.view.ErrorCode = "REAUTHORIZATION_REQUIRED"
			case "oauth_rejected":
				coordinator.view.ErrorCode = "OAUTH_REJECTED"
			case "authorization_identity_mismatch":
				coordinator.view.ErrorCode = "AUTHORIZATION_IDENTITY_MISMATCH"
			case "authorization_not_found":
				coordinator.view.ErrorCode = "AUTHORIZATION_NOT_FOUND"
			case "authorization_ambiguous":
				coordinator.view.ErrorCode = "AUTHORIZATION_AMBIGUOUS"
			case "oauth_app_missing":
				coordinator.view.ErrorCode = "OAUTH_APP_MISSING"
			}
		}
	}
	if !coordinator.started {
		close(coordinator.done)
	}
	coordinator.cancel()
}

func RecoveryTerminal(status string) bool {
	switch status {
	case "completed", "failed", "cancelled", "expired":
		return true
	}
	return false
}
