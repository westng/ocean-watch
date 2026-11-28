package auth

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

// Old manifests have no intent. An older client can also advance the grant's
// revision without clearing this field; that revision supersedes the intent.
func pendingRefresh(row map[string]any, revision int) string {
	attempt, _ := row["pending_refresh_attempt"].(string)
	if strings.HasPrefix(attempt, strconv.Itoa(revision)+":") {
		return attempt
	}
	return ""
}

// Only a definite rejection or a local pre-dispatch failure proves that the
// provider did not rotate the token. After an ambiguous transport failure a
// replay could consume a one-use credential a second time.
func refreshWasRejected(err error) bool {
	var official authorizationFailureMetadata
	if errors.As(err, &official) {
		code, _, status := official.AuthorizationFailureMetadata()
		if code != 0 || status == 429 {
			return true
		}
	}
	var request interface{ Dispatched() bool }
	return errors.As(err, &request) && !request.Dispatched()
}

// Both transitions run under the grant's refresh lock and compare the active
// manifest inside UpdateChannel's cross-process transaction. Only the opaque
// attempt ID is stored in the manifest; the tokens stay in the credential store.
func (manager TokenManager) setRefreshState(ctx context.Context, binding domain.AuthorizationBinding, expected, next string, activate bool) error {
	return manager.Authorizations.UpdateChannel(ctx, binding.Channel, func(state map[string]any) error {
		row, exists := configuration.Object(state["authorizations"])[binding.AuthorizationID].(map[string]any)
		revision, err := positiveInteger(row["token_revision"], 1)
		if !exists || err != nil || revision != binding.TokenRevision || pendingRefresh(row, revision) != expected {
			return errors.New("authorization changed during credential refresh")
		}
		if next != "" {
			row["pending_refresh_attempt"] = next
		} else {
			delete(row, "pending_refresh_attempt")
		}
		if activate {
			row["token_revision"] = revision + 1
		}
		state["generation"] = nextGeneration(state["generation"])
		return nil
	})
}

func (manager TokenManager) recoverRefresh(ctx context.Context, binding *domain.AuthorizationBinding, attempt string) (map[string]any, error) {
	nextAccount, err := domain.AuthorizationCredentialAccount(binding.Channel, binding.AuthorizationID, binding.TokenRevision+1)
	if err != nil {
		return nil, err
	}
	candidate, err := manager.Credentials.Read(ctx, nextAccount)
	if err != nil {
		return nil, fmt.Errorf("read pending refreshed credential: %w", err)
	}
	if candidate["refresh_attempt_id"] != attempt || credentialString(candidate, "access_token") == "" || credentialString(candidate, "refresh_token") == "" ||
		(binding.AppID != "" && credentialString(candidate, "app_id") != binding.AppID) ||
		(binding.AuthorizedUserID != "" && credentialString(candidate, "authorized_user_id") != binding.AuthorizedUserID) {
		// OAuth may already have consumed the old token. Retain the intent so
		// retrying after a temporarily unavailable vault can still recover it.
		return nil, domain.NewError("reauthorization_required", "interrupted OAuth refresh has no matching stored credential; authorize again", 1, nil)
	}
	if err := manager.setRefreshState(ctx, *binding, attempt, "", true); err != nil {
		return nil, fmt.Errorf("activate pending refreshed credential: %w", err)
	}
	binding.TokenRevision++
	binding.CredentialAccount = nextAccount
	return candidate, nil
}
