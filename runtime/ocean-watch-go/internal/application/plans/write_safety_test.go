package plans

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
)

func TestWriteAuthorizationBoundary(t *testing.T) {
	scope := domainplans.WriteScope{
		Channel: domainplans.ChannelMarketing, AdvertiserID: "1234567890123456",
		LockFamily: domainplans.LockMarketingPlans,
	}
	credentials := &credentialSpy{token: "test-access-token"}
	locks := &lockSpy{}
	executor := GuardedExecutor{Credentials: credentials, Locks: locks}
	writes := 0
	action := func(_ context.Context, execution MutationExecution) (any, error) {
		receipt := execution.Dispatcher.Dispatch(
			context.Background(), execution.Capability, "job/project",
			func(context.Context) (any, error) {
				writes++
				return "project-id", nil
			},
		)
		return receipt.Value, receipt.Error
	}

	dryRun, err := executor.Execute(context.Background(), GuardedMutation{
		Scope: scope, Validate: func() error { return nil }, Preview: map[string]any{"endpoint": "/v3.0/project/create/"},
	}, action)
	if err != nil {
		t.Fatal(err)
	}
	if dryRun.Mode != "dry_run" || writes != 0 || credentials.calls != 0 || locks.calls != 0 {
		t.Fatalf("dry-run crossed write boundary: result=%#v writes=%d credentials=%d locks=%d", dryRun, writes, credentials.calls, locks.calls)
	}

	_, err = executor.Execute(context.Background(), GuardedMutation{
		Scope: scope, Submit: true, Validate: func() error { return errors.New("invalid payload") },
	}, action)
	if err == nil || err.Error() != "invalid payload" {
		t.Fatalf("unexpected validation result: %v", err)
	}
	if writes != 0 || credentials.calls != 0 || locks.calls != 0 {
		t.Fatalf("invalid payload reached credentials or write: writes=%d credentials=%d locks=%d", writes, credentials.calls, locks.calls)
	}

	submitted, err := executor.Execute(context.Background(), GuardedMutation{
		Scope: scope, Submit: true, Validate: func() error { return nil },
	}, action)
	if err != nil {
		t.Fatal(err)
	}
	if submitted.Mode != "submit" || submitted.Value != "project-id" {
		t.Fatalf("unexpected submit result: %#v", submitted)
	}
	if writes != 1 || credentials.calls != 1 || locks.calls != 1 || locks.releases != 1 {
		t.Fatalf("submit boundary counts differ: writes=%d credentials=%d locks=%d releases=%d", writes, credentials.calls, locks.calls, locks.releases)
	}
}

func TestUnknownWriteIsDispatchedOnce(t *testing.T) {
	scope := domainplans.WriteScope{
		Channel: domainplans.ChannelQianchuan, AdvertiserID: "9876543210987654",
		LockFamily: domainplans.LockQianchuanWorks,
	}
	capability, err := domainplans.IssueWriteCapability(true, scope, testTime())
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := NewOnceDispatcher(scope)
	calls := 0
	action := func(context.Context) (any, error) {
		calls++
		return nil, &domainplans.DispatchFailure{
			State: domainplans.DispatchUnknown, Cause: errors.New("response lost after request dispatch"),
		}
	}
	first := dispatcher.Dispatch(context.Background(), capability, "creator/create", action)
	second := dispatcher.Dispatch(context.Background(), capability, "creator/create", action)
	if first.State != domainplans.DispatchUnknown || second.State != domainplans.DispatchNotSent || calls != 1 {
		t.Fatalf("unknown write was replayed: first=%#v second=%#v calls=%d", first, second, calls)
	}
}

func TestReconciliationStatesAreClosed(t *testing.T) {
	cases := []struct {
		ids   []string
		state domainplans.ReconciliationState
	}{
		{ids: nil, state: domainplans.ReconciliationNotApplied},
		{ids: []string{"1001"}, state: domainplans.ReconciliationApplied},
		{ids: []string{"1001", "1002"}, state: domainplans.ReconciliationAmbiguous},
	}
	states := make([]domainplans.ReconciliationState, 0, len(cases))
	for _, testCase := range cases {
		result, err := domainplans.ReconcileCandidates(testCase.ids)
		if err != nil {
			t.Fatal(err)
		}
		states = append(states, result.State)
		if result.State != testCase.state {
			t.Fatalf("got %s for %#v", result.State, testCase.ids)
		}
	}
	if !reflect.DeepEqual(states, []domainplans.ReconciliationState{
		domainplans.ReconciliationNotApplied,
		domainplans.ReconciliationApplied,
		domainplans.ReconciliationAmbiguous,
	}) {
		t.Fatalf("unexpected states: %#v", states)
	}
}

type credentialSpy struct {
	mu    sync.Mutex
	token string
	calls int
}

func (spy *credentialSpy) AccessToken(context.Context, domainplans.Channel, string, string) (CredentialLease, error) {
	spy.mu.Lock()
	defer spy.mu.Unlock()
	spy.calls++
	return CredentialLease{AuthorizationID: "fixture-authorization", AccessToken: spy.token}, nil
}

type lockSpy struct {
	mu       sync.Mutex
	calls    int
	releases int
}

func (spy *lockSpy) Acquire(context.Context, domainplans.WriteScope) (func() error, error) {
	spy.mu.Lock()
	spy.calls++
	spy.mu.Unlock()
	return func() error {
		spy.mu.Lock()
		spy.releases++
		spy.mu.Unlock()
		return nil
	}, nil
}

func testTime() time.Time {
	return time.Date(2026, time.July, 26, 0, 0, 0, 0, time.UTC)
}
