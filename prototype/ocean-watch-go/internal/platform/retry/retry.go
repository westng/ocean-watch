package retry

import (
	"context"
	"errors"
	"time"
)

type Classifier func(error) (bool, time.Duration)
type Jitter func(time.Duration) time.Duration

type Policy struct {
	Delays        []time.Duration
	MaxRetryAfter time.Duration
	Jitter        Jitter
	Sleep         func(context.Context, time.Duration) error
}

func Do[T any](
	ctx context.Context,
	policy Policy,
	classify Classifier,
	operation func(context.Context, int) (T, error),
) (T, error) {
	var zero T
	if operation == nil {
		return zero, errors.New("retry operation is required")
	}
	if ctx == nil {
		return zero, errors.New("retry context is required")
	}
	for attempt := 0; ; attempt++ {
		value, err := operation(ctx, attempt)
		if err == nil {
			return value, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) && ctx.Err() != nil {
			return zero, err
		}
		retryable, retryAfter := false, time.Duration(0)
		if classify != nil {
			retryable, retryAfter = classify(err)
		}
		if !retryable || attempt >= len(policy.Delays) {
			return zero, err
		}
		delay := policy.Delays[attempt]
		if policy.Jitter != nil && delay > 0 {
			delay = boundedJitter(delay, policy.Jitter(delay))
		}
		if retryAfter > delay {
			delay = retryAfter
		}
		if policy.MaxRetryAfter > 0 && delay > policy.MaxRetryAfter {
			delay = policy.MaxRetryAfter
		}
		if delay < 0 {
			delay = 0
		}
		if err := sleep(ctx, delay, policy.Sleep); err != nil {
			return zero, err
		}
	}
}

func boundedJitter(base, candidate time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	maximumDelta := base / 5
	minimum, maximum := base-maximumDelta, base+maximumDelta
	if candidate < minimum {
		return minimum
	}
	if candidate > maximum {
		return maximum
	}
	return candidate
}

func sleep(ctx context.Context, delay time.Duration, custom func(context.Context, time.Duration) error) error {
	if custom != nil {
		return custom(ctx, delay)
	}
	if delay == 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
