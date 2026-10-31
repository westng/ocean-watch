package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
)

func TestSQLiteJournalStoreRoundTripsQianchuanPreflight(t *testing.T) {
	created := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	expires := created.Add(30 * time.Minute)
	kind, _ := json.Marshal("qianchuan_batch_preflight")
	snapshot, _ := json.Marshal(map[string]any{
		"schema_version": 2,
		"expires_at":     expires.Format(time.RFC3339Nano),
	})
	journal, err := domainplans.NewJournal("sha256:fixture", map[string]domainplans.JournalJob{
		"group-a": {Status: "prepared", AdvertiserID: "1001", Extra: map[string]json.RawMessage{}},
	}, created)
	if err != nil {
		t.Fatal(err)
	}
	journal.Extra = map[string]json.RawMessage{"kind": kind, "snapshot": snapshot}
	store := SQLiteJournalStore{Path: filepath.Join(t.TempDir(), "state", "ocean-watch.db"), RuntimeVersion: "1.0.9"}
	if err := store.Save(context.Background(), "qianchuan-preflight-20261022t080000-abcdef123456", journal); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(context.Background(), "qianchuan-preflight-20261022t080000-abcdef123456")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, journal) {
		t.Fatalf("round-trip journal = %#v, want %#v", got, journal)
	}
	opened, err := Open(context.Background(), store.options())
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	state, err := opened.GetOperationState(context.Background(), "op_qianchuan-preflight-20261022t080000-abcdef123456", 1)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "preflight_ready" {
		t.Fatalf("operation state = %s, want preflight_ready", state.Status)
	}
}

func TestSQLiteJournalStoreRejectsMissingJournal(t *testing.T) {
	store := SQLiteJournalStore{Path: filepath.Join(t.TempDir(), "state", "ocean-watch.db")}
	if _, err := store.Load(context.Background(), "qianchuan-preflight-20261022t080000-abcdef123456"); err == nil {
		t.Fatal("missing journal unexpectedly loaded")
	}
}
