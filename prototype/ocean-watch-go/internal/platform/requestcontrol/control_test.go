package requestcontrol

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestBudgetIsAtomicAndFailsClosed(t *testing.T) {
	budget, err := NewBudget(8)
	if err != nil {
		t.Fatal(err)
	}
	const callers = 32
	var wait sync.WaitGroup
	results := make(chan error, callers)
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results <- budget.Reserve()
		}()
	}
	wait.Wait()
	close(results)
	successes := 0
	for reserveErr := range results {
		if reserveErr == nil {
			successes++
			continue
		}
		if !errors.Is(reserveErr, ErrRequestBudgetExceeded) {
			t.Fatalf("unexpected budget error: %v", reserveErr)
		}
	}
	if successes != 8 || budget.Snapshot() != (BudgetSnapshot{Limit: 8, Used: 8, Remaining: 0}) {
		t.Fatalf("budget did not enforce its exact limit: successes=%d snapshot=%#v", successes, budget.Snapshot())
	}
}

func TestUnboundedBudgetCountsBeyondFormerBatchLimit(t *testing.T) {
	budget := NewUnboundedBudget()
	for range 600 {
		if err := budget.Reserve(); err != nil {
			t.Fatalf("unbounded request counter rejected an attempt: %v", err)
		}
	}
	if snapshot := budget.Snapshot(); snapshot != (BudgetSnapshot{Used: 600, Unbounded: true}) {
		t.Fatalf("unexpected unbounded request snapshot: %#v", snapshot)
	}
}

func TestZeroBudgetBlocksNetworkAttempts(t *testing.T) {
	budget, err := NewBudget(0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := WithBudget(context.Background(), budget)
	if err != nil {
		t.Fatal(err)
	}
	if err := ReserveAttempt(ctx); !errors.Is(err, ErrRequestBudgetExceeded) {
		t.Fatalf("zero request budget did not fail closed: %v", err)
	}
	if snapshot := budget.Snapshot(); snapshot != (BudgetSnapshot{Limit: 0, Used: 0, Remaining: 0}) {
		t.Fatalf("zero request budget changed after rejection: %#v", snapshot)
	}
}

func TestNegativeAndUninitializedBudgetsAreRejected(t *testing.T) {
	if _, err := NewBudget(-1); err == nil {
		t.Fatal("negative request budget was accepted")
	}
	if _, err := WithBudget(context.Background(), &Budget{}); err == nil {
		t.Fatal("uninitialized request budget was accepted")
	}
}

func TestAdvertiserScopeRejectsLeadingZero(t *testing.T) {
	if _, err := WithAdvertiser(context.Background(), "0123"); err == nil {
		t.Fatal("advertiser scope accepted a leading-zero ID")
	}
}

func TestGovernorSeparatesAuthorizationAndEndpointScopes(t *testing.T) {
	governor, err := NewGovernor(Limits{AuthorizationConcurrency: 2, EndpointConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, _, metrics, err := PrepareCommandContext(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	scopeA := AuthorizationScope{Channel: "marketing", AuthorizationID: "authorization-a"}
	scopeB := AuthorizationScope{Channel: "marketing", AuthorizationID: "authorization-b"}
	releaseA, err := governor.Acquire(ctx, scopeA, "reports")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseA()

	differentEndpoint, err := governor.Acquire(ctx, scopeA, "materials")
	if err != nil {
		t.Fatalf("different endpoint should use the remaining authorization slot: %v", err)
	}
	differentEndpoint()
	differentAuthorization, err := governor.Acquire(ctx, scopeB, "reports")
	if err != nil {
		t.Fatalf("different authorization should not share the endpoint slot: %v", err)
	}
	differentAuthorization()
	if metrics.Snapshot().LimiterAcquisitions != 3 {
		t.Fatalf("unexpected limiter metrics: %#v", metrics.Snapshot())
	}
}

func TestGovernorWaitIsCancelableAndReleasesPartialAcquisition(t *testing.T) {
	governor, err := NewGovernor(Limits{AuthorizationConcurrency: 2, EndpointConcurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, _, metrics, err := PrepareCommandContext(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	scope := AuthorizationScope{Channel: "marketing", AuthorizationID: "authorization-a"}
	release, err := governor.Acquire(ctx, scope, "plans")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := governor.Acquire(waitCtx, scope, "plans"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("limiter wait did not preserve its deadline: %v", err)
	}

	otherEndpoint, err := governor.Acquire(ctx, scope, "materials")
	if err != nil {
		t.Fatalf("canceled endpoint wait leaked its authorization slot: %v", err)
	}
	otherEndpoint()
	snapshot := metrics.Snapshot()
	if snapshot.LimiterWaits == 0 || snapshot.LimiterCancellations != 1 || snapshot.LimiterWaitDuration <= 0 {
		t.Fatalf("limiter wait was not observable: %#v", snapshot)
	}
}

func TestGovernorSerializesQianchuanAcrossEndpointFamilies(t *testing.T) {
	governor, err := NewGovernor(Limits{AuthorizationConcurrency: 4, EndpointConcurrency: 4})
	if err != nil {
		t.Fatal(err)
	}
	ctx, _, metrics, err := PrepareCommandContext(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	scope := AuthorizationScope{Channel: "qianchuan", AuthorizationID: "authorization-a"}
	release, err := governor.Acquire(ctx, scope, "plans")
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := governor.Acquire(waitCtx, scope, "materials"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Qianchuan authorization allowed concurrent endpoint traffic: %v", err)
	}
	release()
	second, err := governor.Acquire(ctx, scope, "materials")
	if err != nil {
		t.Fatal(err)
	}
	second()
	if metrics.Snapshot().LimiterCancellations != 1 {
		t.Fatalf("Qianchuan limiter wait was not observed: %#v", metrics.Snapshot())
	}
}

func TestPrepareCommandContextPreservesInjectedBudget(t *testing.T) {
	budget, err := NewBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := WithBudget(context.Background(), budget)
	if err != nil {
		t.Fatal(err)
	}
	ctx, prepared, metrics, err := PrepareCommandContext(ctx, DefaultCommandRequestLimit)
	if err != nil {
		t.Fatal(err)
	}
	if prepared != budget || metrics == nil {
		t.Fatal("command context replaced injected request controls")
	}
	if err := ReserveAttempt(ctx); err != nil {
		t.Fatal(err)
	}
	if err := ReserveAttempt(ctx); !errors.Is(err, ErrRequestBudgetExceeded) {
		t.Fatalf("injected budget was not preserved: %v", err)
	}
	if metrics.Snapshot().Attempts != 1 {
		t.Fatalf("attempt metrics counted rejected traffic: %#v", metrics.Snapshot())
	}
}

func TestPrepareUnboundedCommandContextTracksWithoutRejecting(t *testing.T) {
	ctx, budget, metrics, err := PrepareUnboundedCommandContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for range 600 {
		if err := ReserveAttempt(ctx); err != nil {
			t.Fatalf("unbounded command context rejected an attempt: %v", err)
		}
	}
	if snapshot := budget.Snapshot(); snapshot != (BudgetSnapshot{Used: 600, Unbounded: true}) {
		t.Fatalf("unexpected unbounded command budget: %#v", snapshot)
	}
	if snapshot := metrics.Snapshot(); snapshot.Attempts != 600 {
		t.Fatalf("unexpected unbounded command metrics: %#v", snapshot)
	}
}

func TestPrepareUnboundedCommandContextReplacesInjectedHardLimit(t *testing.T) {
	bounded, err := NewBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := WithBudget(context.Background(), bounded)
	if err != nil {
		t.Fatal(err)
	}
	ctx, prepared, _, err := PrepareUnboundedCommandContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if prepared == bounded || !prepared.IsUnbounded() {
		t.Fatal("unbounded command preserved an injected hard request limit")
	}
	for range 600 {
		if err := ReserveAttempt(ctx); err != nil {
			t.Fatalf("replacement request counter rejected an attempt: %v", err)
		}
	}
}
