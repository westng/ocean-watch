package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryPolicyHonorsBoundedRetryAfter(t *testing.T) {
	calls := 0
	delays := []time.Duration{}
	value, err := Do(
		context.Background(),
		Policy{
			Delays: []time.Duration{time.Second, 2 * time.Second}, MaxRetryAfter: 3 * time.Second,
			Sleep: func(_ context.Context, delay time.Duration) error {
				delays = append(delays, delay)
				return nil
			},
		},
		func(error) (bool, time.Duration) { return true, 10 * time.Second },
		func(context.Context, int) (string, error) {
			calls++
			if calls < 3 {
				return "", errors.New("transient")
			}
			return "ok", nil
		},
	)
	if err != nil || value != "ok" {
		t.Fatalf("retry result = %q, %v", value, err)
	}
	if calls != 3 || len(delays) != 2 || delays[0] != 3*time.Second || delays[1] != 3*time.Second {
		t.Fatalf("calls=%d delays=%v", calls, delays)
	}
}

func TestRetryPolicyPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Do(
		ctx,
		Policy{Delays: []time.Duration{0}},
		func(error) (bool, time.Duration) { return true, 0 },
		func(context.Context, int) (struct{}, error) { return struct{}{}, context.Canceled },
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation identity was lost: %v", err)
	}
}

func TestRetryPolicyBoundsInjectedJitterAndDoesNotShortenRetryAfter(t *testing.T) {
	for _, test := range []struct {
		name       string
		candidate  time.Duration
		retryAfter time.Duration
		want       time.Duration
	}{
		{name: "lower-bound", candidate: 0, want: 8 * time.Second},
		{name: "upper-bound", candidate: time.Minute, want: 12 * time.Second},
		{name: "retry-after", candidate: 0, retryAfter: 11 * time.Second, want: 11 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			var delay time.Duration
			calls := 0
			_, err := Do(
				context.Background(),
				Policy{
					Delays: []time.Duration{10 * time.Second},
					Jitter: func(time.Duration) time.Duration { return test.candidate },
					Sleep: func(_ context.Context, value time.Duration) error {
						delay = value
						return nil
					},
				},
				func(error) (bool, time.Duration) { return true, test.retryAfter },
				func(context.Context, int) (struct{}, error) {
					calls++
					if calls == 1 {
						return struct{}{}, errors.New("transient")
					}
					return struct{}{}, nil
				},
			)
			if err != nil || delay != test.want {
				t.Fatalf("retry jitter delay = %s, want %s: %v", delay, test.want, err)
			}
		})
	}
}
