package approvalbroker

import (
	"context"
	"errors"
	"testing"
	"time"

	application "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/operations"
	domain "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/approval"
)

func TestReconfirmLostOrExpiredApproval(t *testing.T) {
	for _, expired := range []bool{false, true} {
		name := "lost"
		if expired {
			name = "expired"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			decisions := 0
			var onDecision func() error
			service, broker, _, snapshot := testService(t, func(context.Context, approval.Request) error {
				decisions++
				if onDecision != nil {
					return onDecision()
				}
				return nil
			})
			now := time.Now().UTC()
			service.Now = func() time.Time { return now }
			broker.now = service.Now
			confirm := func() (application.ConfirmationResult, error) {
				return service.Confirm(ctx, snapshot.OperationID, 1, snapshot.SnapshotDigest)
			}
			execute := func(id string) error {
				_, err := service.Execute(ctx, application.ExecuteRequest{OperationID: snapshot.OperationID, Version: 1, SnapshotDigest: snapshot.SnapshotDigest, ApprovalID: id, OwnerID: "worker-1", TTL: time.Minute})
				return err
			}
			first, err := confirm()
			if err != nil {
				t.Fatal(err)
			}
			if expired {
				now = first.ExpiresAt
				if err := execute(first.ApprovalID); !errors.Is(err, domain.ErrApprovalExpired) {
					t.Fatalf("expired approval: %v", err)
				}
			}
			onDecision = func() error {
				if err := execute(first.ApprovalID); !errors.Is(err, domain.ErrOperationNotExecutable) {
					t.Fatalf("old approval while dialog open: %v", err)
				}
				return approval.ErrNotApproved
			}
			if _, err := confirm(); !errors.Is(err, approval.ErrNotApproved) {
				t.Fatalf("rejected reconfirmation: %v", err)
			}
			if err := execute(first.ApprovalID); !errors.Is(err, domain.ErrOperationNotExecutable) {
				t.Fatalf("old approval after rejection: %v", err)
			}
			onDecision = nil
			fresh, err := confirm()
			if err != nil || fresh.ApprovalID == first.ApprovalID || decisions != 3 {
				t.Fatalf("replacement = %+v, dialogs = %d, err = %v", fresh, decisions, err)
			}
			if err := execute(first.ApprovalID); !errors.Is(err, domain.ErrApprovalExpired) {
				t.Fatalf("superseded approval: %v", err)
			}
			if err := execute(fresh.ApprovalID); err != nil {
				t.Fatal(err)
			}
			if _, err := confirm(); !errors.Is(err, domain.ErrInvalidTransition) || decisions != 3 {
				t.Fatalf("new dialog after execution: %d, %v", decisions, err)
			}
		})
	}
}

func TestSupersededDialogCannotSaveApproval(t *testing.T) {
	ctx := context.Background()
	older, broker, store, snapshot := testService(t, nil)
	second := &Broker{now: time.Now, exchange: func(context.Context, approval.Request) error { return approval.ErrNotApproved }}
	verifier, err := approval.NewVerifier(second)
	if err != nil {
		t.Fatal(err)
	}
	newer := application.Service{Repository: store, Snapshots: store, Provider: second, Evidence: second, Verifier: verifier}
	broker.exchange = func(context.Context, approval.Request) error {
		if _, err := newer.Confirm(ctx, snapshot.OperationID, 1, snapshot.SnapshotDigest); !errors.Is(err, approval.ErrNotApproved) {
			t.Fatalf("second dialog: %v", err)
		}
		return nil
	}
	if _, err := older.Confirm(ctx, snapshot.OperationID, 1, snapshot.SnapshotDigest); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("delayed decision: %v", err)
	}
	state, err := store.GetOperationState(ctx, snapshot.OperationID, 1)
	if err != nil || state.Status != domain.StatusAwaitingApproval {
		t.Fatalf("state = %+v, err = %v", state, err)
	}
}
