package approvalbroker

import (
	"context"
	"errors"
	"os/exec"
	"testing"
)

func TestZenityDecision(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		result   error
		approved bool
		wantErr  error
	}{
		{"approved", nil, true, nil},
		{"cancelled", exec.Command("sh", "-c", "exit 1").Run(), false, nil},
		{"failed", exec.Command("sh", "-c", "exit 255").Run(), false, ErrUnavailable},
		{"unavailable", exec.ErrNotFound, false, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			approved, err := zenityDecision(ctx, tc.result)
			if approved != tc.approved || !errors.Is(err, tc.wantErr) {
				t.Fatalf("decision = (%v, %v), want (%v, %v)", approved, err, tc.approved, tc.wantErr)
			}
		})
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if yes, err := zenityDecision(cancelled, nil); yes || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled process approved: %v, %v", yes, err)
	}
}
