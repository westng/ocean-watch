package plans

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
)

type CredentialProvider interface {
	AccessToken(context.Context, domainplans.Channel, string, string) (CredentialLease, error)
}

type CredentialLease struct {
	AuthorizationID string
	AccessToken     string
}

type AdvertiserLocker interface {
	Acquire(context.Context, domainplans.WriteScope) (func() error, error)
}

type JournalStore interface {
	Load(context.Context, string) (domainplans.Journal, error)
	Save(context.Context, string, domainplans.Journal) error
}

type JournalCatalog interface {
	List(context.Context, string) ([]domainplans.JournalRecord, error)
}

type JournalScopeLocker interface {
	AcquireScope(context.Context, string) (func() error, error)
}

type advertiserLockContextKey struct{}

func WithAdvertiserLock(
	ctx context.Context,
	locks AdvertiserLocker,
	scope domainplans.WriteScope,
	action func(context.Context) error,
) error {
	if ctx == nil {
		return errors.New("advertiser lock context is required")
	}
	if locks == nil || action == nil {
		return errors.New("advertiser lock dependencies are incomplete")
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	if advertiserLockHeld(ctx, scope) {
		return action(ctx)
	}
	release, err := locks.Acquire(ctx, scope)
	if err != nil {
		return err
	}
	defer func() { _ = release() }()
	locked := context.WithValue(ctx, advertiserLockContextKey{}, scope)
	return action(locked)
}

func advertiserLockHeld(ctx context.Context, scope domainplans.WriteScope) bool {
	if ctx == nil {
		return false
	}
	held, ok := ctx.Value(advertiserLockContextKey{}).(domainplans.WriteScope)
	return ok && held.Channel == scope.Channel && held.AdvertiserID == scope.AdvertiserID
}

type GuardedMutation struct {
	Scope         domainplans.WriteScope
	AuthAccountID string
	Submit        bool
	Validate      func() error
	Preview       any
}

type MutationExecution struct {
	Capability      domainplans.WriteCapability
	AuthorizationID string
	AccessToken     string
	Dispatcher      *OnceDispatcher
}

type MutationResult struct {
	Mode  string `json:"mode"`
	Value any    `json:"value,omitempty"`
}

type GuardedExecutor struct {
	Credentials CredentialProvider
	Locks       AdvertiserLocker
	Now         func() time.Time
}

func (executor GuardedExecutor) Execute(
	ctx context.Context,
	request GuardedMutation,
	action func(context.Context, MutationExecution) (any, error),
) (MutationResult, error) {
	if ctx == nil {
		return MutationResult{}, errors.New("write context is required")
	}
	if request.Validate == nil {
		return MutationResult{}, errors.New("write validation is required")
	}
	if err := request.Validate(); err != nil {
		return MutationResult{}, err
	}
	if err := request.Scope.Validate(); err != nil {
		return MutationResult{}, err
	}
	if !request.Submit {
		return MutationResult{Mode: "dry_run", Value: request.Preview}, nil
	}
	if executor.Credentials == nil || executor.Locks == nil || action == nil {
		return MutationResult{}, errors.New("write executor dependencies are incomplete")
	}
	now := time.Now().UTC()
	if executor.Now != nil {
		now = executor.Now().UTC()
	}
	capability, err := domainplans.IssueWriteCapability(true, request.Scope, now)
	if err != nil {
		return MutationResult{}, err
	}
	lease, err := executor.Credentials.AccessToken(
		ctx, request.Scope.Channel, request.Scope.AdvertiserID,
		strings.TrimSpace(request.AuthAccountID),
	)
	if err != nil {
		return MutationResult{}, err
	}
	if strings.TrimSpace(lease.AuthorizationID) == "" {
		return MutationResult{}, errors.New("credential provider returned an empty authorization identity")
	}
	if strings.TrimSpace(lease.AccessToken) == "" {
		return MutationResult{}, errors.New("credential provider returned an empty access token")
	}
	ctx, err = requestcontrol.WithAuthorization(
		ctx, string(request.Scope.Channel), strings.TrimSpace(lease.AuthorizationID),
	)
	if err != nil {
		return MutationResult{}, err
	}
	ctx, err = requestcontrol.WithAdvertiser(ctx, request.Scope.AdvertiserID)
	if err != nil {
		return MutationResult{}, err
	}
	var value any
	err = WithAdvertiserLock(ctx, executor.Locks, request.Scope, func(locked context.Context) error {
		var actionErr error
		value, actionErr = action(locked, MutationExecution{
			Capability: capability, AuthorizationID: lease.AuthorizationID,
			AccessToken: lease.AccessToken, Dispatcher: NewOnceDispatcher(request.Scope),
		})
		return actionErr
	})
	if err != nil {
		return MutationResult{Mode: "submit", Value: value}, err
	}
	return MutationResult{Mode: "submit", Value: value}, nil
}

type DispatchReceipt struct {
	State domainplans.DispatchState
	Value any
	Error error
}

type OnceDispatcher struct {
	scope     domainplans.WriteScope
	mu        sync.Mutex
	attempted map[string]struct{}
}

func NewOnceDispatcher(scope domainplans.WriteScope) *OnceDispatcher {
	return &OnceDispatcher{scope: scope, attempted: map[string]struct{}{}}
}

func (dispatcher *OnceDispatcher) Dispatch(
	ctx context.Context,
	capability domainplans.WriteCapability,
	operationKey string,
	action func(context.Context) (any, error),
) DispatchReceipt {
	if dispatcher == nil {
		return DispatchReceipt{State: domainplans.DispatchNotSent, Error: errors.New("write dispatcher is required")}
	}
	operationKey = strings.TrimSpace(operationKey)
	if operationKey == "" || len(operationKey) > 256 {
		return DispatchReceipt{State: domainplans.DispatchNotSent, Error: errors.New("write operation key is invalid")}
	}
	if !capability.Authorizes(dispatcher.scope) {
		return DispatchReceipt{State: domainplans.DispatchNotSent, Error: errors.New("write capability does not authorize this scope")}
	}
	if action == nil {
		return DispatchReceipt{State: domainplans.DispatchNotSent, Error: errors.New("write action is required")}
	}
	dispatcher.mu.Lock()
	if _, exists := dispatcher.attempted[operationKey]; exists {
		dispatcher.mu.Unlock()
		return DispatchReceipt{
			State: domainplans.DispatchNotSent,
			Error: fmt.Errorf("write operation %q was already dispatched", operationKey),
		}
	}
	dispatcher.attempted[operationKey] = struct{}{}
	dispatcher.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return DispatchReceipt{State: domainplans.DispatchNotSent, Error: err}
	}
	value, err := action(ctx)
	return DispatchReceipt{
		State: domainplans.ClassifyDispatchError(err), Value: value, Error: err,
	}
}

func (dispatcher *OnceDispatcher) AttemptCount() int {
	if dispatcher == nil {
		return 0
	}
	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()
	return len(dispatcher.attempted)
}
