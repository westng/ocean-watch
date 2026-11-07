package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestMaintenanceDowngradeAllowsReadersButExcludesMigration(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "private")
	if err := createStateDirectory(directory); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "state.db")
	ctx := context.Background()
	release, downgrade, err := acquireMaintenance(ctx, path, true, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := downgrade(); err != nil {
		t.Fatal(err)
	}
	otherRelease, _, err := acquireMaintenance(ctx, path, false, time.Second)
	if err != nil {
		t.Fatalf("downgrade retained the exclusive lock: %v", err)
	}
	defer otherRelease()
	assertMigrationBlocked := func() {
		t.Helper()
		unexpectedRelease, _, err := acquireMaintenance(ctx, path, true, 20*time.Millisecond)
		if unexpectedRelease != nil {
			_ = unexpectedRelease()
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("migration was not blocked by shared users: %v", err)
		}
	}
	assertMigrationBlocked()
	if err := release(); err != nil {
		t.Fatal(err)
	}
	assertMigrationBlocked()
	if err := otherRelease(); err != nil {
		t.Fatal(err)
	}
	finalRelease, _, err := acquireMaintenance(ctx, path, true, time.Second)
	if err != nil {
		t.Fatalf("shared users left a lock behind: %v", err)
	}
	if err := finalRelease(); err != nil {
		t.Fatal(err)
	}
}
