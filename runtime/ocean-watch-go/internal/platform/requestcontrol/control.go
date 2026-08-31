package requestcontrol

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultAuthorizationConcurrency   = 4
	DefaultEndpointConcurrency        = 4
	QianchuanAuthorizationConcurrency = 1
	QianchuanMinimumRequestInterval   = 250 * time.Millisecond
	QianchuanRateLimitCooldown        = 5 * time.Second
	DefaultCommandRequestLimit        = int64(4096)
)

var (
	ErrAuthorizationScopeMissing = errors.New("request authorization scope is missing")
	ErrRequestBudgetMissing      = errors.New("command request budget is missing")
	ErrRequestBudgetExceeded     = errors.New("command request budget is exhausted")
)

type contextKey uint8

const (
	authorizationContextKey contextKey = iota
	budgetContextKey
	metricsContextKey
	advertiserContextKey
)

type AuthorizationScope struct {
	Channel         string
	AuthorizationID string
}

func WithAdvertiser(ctx context.Context, advertiserID string) (context.Context, error) {
	if ctx == nil {
		return nil, errors.New("request context is required")
	}
	advertiserID = strings.TrimSpace(advertiserID)
	if advertiserID == "" || len(advertiserID) > 64 || advertiserID[0] == '0' {
		return nil, errors.New("request advertiser identity is invalid")
	}
	for _, character := range advertiserID {
		if character < '0' || character > '9' {
			return nil, errors.New("request advertiser identity is invalid")
		}
	}
	return context.WithValue(ctx, advertiserContextKey, advertiserID), nil
}

func Advertiser(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	advertiserID, ok := ctx.Value(advertiserContextKey).(string)
	return advertiserID, ok && advertiserID != ""
}

func WithAuthorization(
	ctx context.Context,
	channel string,
	authorizationID string,
) (context.Context, error) {
	if ctx == nil {
		return nil, errors.New("request context is required")
	}
	scope := AuthorizationScope{
		Channel:         strings.TrimSpace(channel),
		AuthorizationID: strings.TrimSpace(authorizationID),
	}
	if scope.Channel != "marketing" && scope.Channel != "qianchuan" {
		return nil, errors.New("request channel must be marketing or qianchuan")
	}
	if scope.AuthorizationID == "" || len(scope.AuthorizationID) > 256 {
		return nil, errors.New("request authorization identity is invalid")
	}
	return context.WithValue(ctx, authorizationContextKey, scope), nil
}

func Authorization(ctx context.Context) (AuthorizationScope, bool) {
	if ctx == nil {
		return AuthorizationScope{}, false
	}
	scope, ok := ctx.Value(authorizationContextKey).(AuthorizationScope)
	return scope, ok && scope.Channel != "" && scope.AuthorizationID != ""
}

type Budget struct {
	limit       int64
	initialized bool
	unbounded   bool
	used        atomic.Int64
}

type BudgetSnapshot struct {
	Limit     int64 `json:"limit"`
	Used      int64 `json:"used"`
	Remaining int64 `json:"remaining"`
	Unbounded bool  `json:"unbounded,omitempty"`
}

type BudgetExceededError struct {
	Limit int64
}

func (err *BudgetExceededError) Error() string {
	return fmt.Sprintf("%s at %d attempts", ErrRequestBudgetExceeded, err.Limit)
}

func (err *BudgetExceededError) Unwrap() error { return ErrRequestBudgetExceeded }

func NewBudget(limit int64) (*Budget, error) {
	if limit < 0 {
		return nil, errors.New("command request budget must be non-negative")
	}
	return &Budget{limit: limit, initialized: true}, nil
}

func NewUnboundedBudget() *Budget {
	return &Budget{initialized: true, unbounded: true}
}

func WithBudget(ctx context.Context, budget *Budget) (context.Context, error) {
	if ctx == nil {
		return nil, errors.New("request context is required")
	}
	if budget == nil || !budget.initialized || budget.limit < 0 {
		return nil, errors.New("command request budget is invalid")
	}
	return context.WithValue(ctx, budgetContextKey, budget), nil
}

func BudgetFrom(ctx context.Context) (*Budget, bool) {
	if ctx == nil {
		return nil, false
	}
	budget, ok := ctx.Value(budgetContextKey).(*Budget)
	return budget, ok && budget != nil && budget.initialized && budget.limit >= 0
}

func (budget *Budget) Reserve() error {
	if budget == nil || !budget.initialized || budget.limit < 0 {
		return ErrRequestBudgetMissing
	}
	if budget.unbounded {
		budget.used.Add(1)
		return nil
	}
	for {
		used := budget.used.Load()
		if used >= budget.limit {
			return &BudgetExceededError{Limit: budget.limit}
		}
		if budget.used.CompareAndSwap(used, used+1) {
			return nil
		}
	}
}

func (budget *Budget) IsUnbounded() bool {
	return budget != nil && budget.initialized && budget.unbounded
}

func (budget *Budget) Snapshot() BudgetSnapshot {
	if budget == nil {
		return BudgetSnapshot{}
	}
	used := budget.used.Load()
	if budget.unbounded {
		return BudgetSnapshot{Used: used, Unbounded: true}
	}
	remaining := budget.limit - used
	if remaining < 0 {
		remaining = 0
	}
	return BudgetSnapshot{Limit: budget.limit, Used: used, Remaining: remaining}
}

type Metrics struct {
	attempts             atomic.Int64
	retries              atomic.Int64
	limiterAcquisitions  atomic.Int64
	limiterWaits         atomic.Int64
	limiterWaitNanos     atomic.Int64
	limiterCancellations atomic.Int64
}

type MetricsSnapshot struct {
	Attempts             int64         `json:"attempts"`
	Retries              int64         `json:"retries"`
	LimiterAcquisitions  int64         `json:"limiter_acquisitions"`
	LimiterWaits         int64         `json:"limiter_waits"`
	LimiterWaitDuration  time.Duration `json:"limiter_wait_duration"`
	LimiterCancellations int64         `json:"limiter_cancellations"`
}

func WithMetrics(ctx context.Context, metrics *Metrics) (context.Context, error) {
	if ctx == nil {
		return nil, errors.New("request context is required")
	}
	if metrics == nil {
		return nil, errors.New("request metrics are required")
	}
	return context.WithValue(ctx, metricsContextKey, metrics), nil
}

func MetricsFrom(ctx context.Context) (*Metrics, bool) {
	if ctx == nil {
		return nil, false
	}
	metrics, ok := ctx.Value(metricsContextKey).(*Metrics)
	return metrics, ok && metrics != nil
}

func (metrics *Metrics) Snapshot() MetricsSnapshot {
	if metrics == nil {
		return MetricsSnapshot{}
	}
	return MetricsSnapshot{
		Attempts: metrics.attempts.Load(), Retries: metrics.retries.Load(),
		LimiterAcquisitions:  metrics.limiterAcquisitions.Load(),
		LimiterWaits:         metrics.limiterWaits.Load(),
		LimiterWaitDuration:  time.Duration(metrics.limiterWaitNanos.Load()),
		LimiterCancellations: metrics.limiterCancellations.Load(),
	}
}

// RecordRetry records a retry scheduled by a read policy.
func RecordRetry(ctx context.Context) {
	if metrics, ok := MetricsFrom(ctx); ok {
		metrics.retries.Add(1)
	}
}

func PrepareCommandContext(
	ctx context.Context,
	limit int64,
) (context.Context, *Budget, *Metrics, error) {
	if ctx == nil {
		return nil, nil, nil, errors.New("command context is required")
	}
	budget, ok := BudgetFrom(ctx)
	if !ok {
		var err error
		budget, err = NewBudget(limit)
		if err != nil {
			return nil, nil, nil, err
		}
		ctx, err = WithBudget(ctx, budget)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	metrics, ok := MetricsFrom(ctx)
	if !ok {
		metrics = &Metrics{}
		var err error
		ctx, err = WithMetrics(ctx, metrics)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return ctx, budget, metrics, nil
}

func PrepareUnboundedCommandContext(
	ctx context.Context,
) (context.Context, *Budget, *Metrics, error) {
	if ctx == nil {
		return nil, nil, nil, errors.New("command context is required")
	}
	budget, ok := BudgetFrom(ctx)
	if !ok || !budget.IsUnbounded() {
		budget = NewUnboundedBudget()
		var err error
		ctx, err = WithBudget(ctx, budget)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	metrics, ok := MetricsFrom(ctx)
	if !ok {
		metrics = &Metrics{}
		var err error
		ctx, err = WithMetrics(ctx, metrics)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return ctx, budget, metrics, nil
}

func ReserveAttempt(ctx context.Context) error {
	budget, ok := BudgetFrom(ctx)
	if !ok {
		return ErrRequestBudgetMissing
	}
	if err := budget.Reserve(); err != nil {
		return err
	}
	if metrics, ok := MetricsFrom(ctx); ok {
		metrics.attempts.Add(1)
	}
	return nil
}

type Limits struct {
	AuthorizationConcurrency          int
	EndpointConcurrency               int
	QianchuanAuthorizationConcurrency int
}

type authorizationKey struct {
	channel         string
	authorizationID string
}

type endpointKey struct {
	authorizationKey
	endpoint string
}

type Governor struct {
	mu             sync.Mutex
	limits         Limits
	authorizations map[authorizationKey]chan struct{}
	endpoints      map[endpointKey]chan struct{}
}

func NewGovernor(limits Limits) (*Governor, error) {
	if limits.AuthorizationConcurrency == 0 {
		limits.AuthorizationConcurrency = DefaultAuthorizationConcurrency
	}
	if limits.EndpointConcurrency == 0 {
		limits.EndpointConcurrency = DefaultEndpointConcurrency
	}
	if limits.QianchuanAuthorizationConcurrency == 0 {
		limits.QianchuanAuthorizationConcurrency = QianchuanAuthorizationConcurrency
	}
	if limits.AuthorizationConcurrency < 1 || limits.EndpointConcurrency < 1 ||
		limits.QianchuanAuthorizationConcurrency < 1 {
		return nil, errors.New("request concurrency limits must be positive")
	}
	return &Governor{
		limits: limits, authorizations: map[authorizationKey]chan struct{}{},
		endpoints: map[endpointKey]chan struct{}{},
	}, nil
}

func (governor *Governor) Acquire(
	ctx context.Context,
	scope AuthorizationScope,
	endpoint string,
) (func(), error) {
	if governor == nil {
		return nil, errors.New("request governor is required")
	}
	if ctx == nil {
		return nil, errors.New("request context is required")
	}
	if scope.Channel != "marketing" && scope.Channel != "qianchuan" ||
		strings.TrimSpace(scope.AuthorizationID) == "" {
		return nil, errors.New("request authorization scope is invalid")
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" || len(endpoint) > 512 {
		return nil, errors.New("request endpoint family is invalid")
	}
	authorization := authorizationKey{
		channel: scope.Channel, authorizationID: scope.AuthorizationID,
	}
	endpointScope := endpointKey{authorizationKey: authorization, endpoint: endpoint}
	authorizationSlot, endpointSlot := governor.slots(authorization, endpointScope)
	metrics, _ := MetricsFrom(ctx)
	if err := acquireSlot(ctx, authorizationSlot, metrics); err != nil {
		return nil, err
	}
	if err := acquireSlot(ctx, endpointSlot, metrics); err != nil {
		releaseSlot(authorizationSlot)
		return nil, err
	}
	if metrics != nil {
		metrics.limiterAcquisitions.Add(1)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			releaseSlot(endpointSlot)
			releaseSlot(authorizationSlot)
		})
	}, nil
}

func (governor *Governor) slots(
	authorization authorizationKey,
	endpoint endpointKey,
) (chan struct{}, chan struct{}) {
	governor.mu.Lock()
	defer governor.mu.Unlock()
	authorizationSlot := governor.authorizations[authorization]
	if authorizationSlot == nil {
		capacity := governor.limits.AuthorizationConcurrency
		if authorization.channel == "qianchuan" {
			capacity = governor.limits.QianchuanAuthorizationConcurrency
		}
		authorizationSlot = make(chan struct{}, capacity)
		governor.authorizations[authorization] = authorizationSlot
	}
	endpointSlot := governor.endpoints[endpoint]
	if endpointSlot == nil {
		endpointSlot = make(chan struct{}, governor.limits.EndpointConcurrency)
		governor.endpoints[endpoint] = endpointSlot
	}
	return authorizationSlot, endpointSlot
}

func acquireSlot(ctx context.Context, slot chan struct{}, metrics *Metrics) error {
	select {
	case slot <- struct{}{}:
		return nil
	default:
	}
	started := time.Now()
	if metrics != nil {
		metrics.limiterWaits.Add(1)
	}
	select {
	case slot <- struct{}{}:
		if metrics != nil {
			metrics.limiterWaitNanos.Add(time.Since(started).Nanoseconds())
		}
		return nil
	case <-ctx.Done():
		if metrics != nil {
			metrics.limiterWaitNanos.Add(time.Since(started).Nanoseconds())
			metrics.limiterCancellations.Add(1)
		}
		return ctx.Err()
	}
}

func releaseSlot(slot chan struct{}) { <-slot }
