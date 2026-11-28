package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
)

func TestUpgradeAndUseLegacyLeaseAcrossFractionalSecondBoundary(t *testing.T) {
	ctx := context.Background()
	old := versionOneStore(t)
	steps, err := migrationSteps()
	if err != nil {
		t.Fatal(err)
	}
	if err := old.applyMigrations(ctx, steps[:6], "fixture_v6", "fixture_backup"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := old.GetSnapshot(ctx, "op_sqlite_test", 1)
	if err != nil {
		t.Fatal(err)
	}
	base := snapshot.CreatedAt
	// Seed exactly the variable-width TEXT representation produced by schema 6.
	_, err = old.db.ExecContext(ctx, `INSERT INTO operation_states(operation_id,operation_version,status,snapshot_digest,created_at,updated_at) VALUES(?,1,'executing',?,?,?)`, snapshot.OperationID, snapshot.SnapshotDigest, operationTime(base), operationTime(base))
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.db.ExecContext(ctx, `INSERT INTO execution_leases(operation_id,operation_version,owner_id,fencing_epoch,acquired_at,expires_at,renewed_at) VALUES(?,1,'old-worker',1,?,?,?)`, snapshot.OperationID, operationTime(base), operationTime(base.Add(time.Second+900*time.Millisecond)), operationTime(base))
	if err != nil {
		t.Fatal(err)
	}
	path := old.path
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Upgrade(ctx, Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	lease, err := readLease(ctx, store.db, snapshot.OperationID, 1)
	if err != nil {
		t.Fatal(err)
	}
	now := base.Add(time.Second)
	if _, err := store.TakeOverExecution(ctx, snapshot.OperationID, 1, "next-worker", now, time.Minute); !errors.Is(err, domain.ErrLeaseHeld) {
		t.Fatalf("early takeover: %v", err)
	}
	renewed, err := store.RenewExecutionLease(ctx, lease, now, time.Second)
	if err != nil || !renewed.ExpiresAt.Equal(base.Add(2*time.Second)) {
		t.Fatalf("renewal = %+v, %v", renewed, err)
	}
	if _, err := store.RenewExecutionLease(ctx, lease, renewed.ExpiresAt, time.Minute); !errors.Is(err, domain.ErrLeaseExpired) {
		t.Fatalf("renewal at exact expiry: %v", err)
	}
	later := renewed.ExpiresAt.Add(time.Nanosecond)
	taken, err := store.TakeOverExecution(ctx, snapshot.OperationID, 1, "next-worker", later, time.Minute)
	if err != nil || taken.FencingEpoch != lease.FencingEpoch+1 {
		t.Fatalf("takeover = %+v, %v", taken, err)
	}
	if _, err := store.RenewExecutionLease(ctx, lease, later, time.Minute); !errors.Is(err, domain.ErrStaleEpoch) {
		t.Fatalf("stale renewal: %v", err)
	}
}
