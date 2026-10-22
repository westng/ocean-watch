package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	app "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/legacy"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/legacy"
)

func legacyStore(t *testing.T) (*Store, Options) {
	t.Helper()
	options := Options{Path: filepath.Join(t.TempDir(), "state", "state.db"), RuntimeVersion: "current-core"}
	store, err := Initialize(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	var exists int
	if err := store.db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='legacy_mappings'").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists != 1 {
		t.Fatal("Initialize did not apply 004 legacy migration")
	}
	return store, options
}

func legacyMapping(t *testing.T, record string, mutate func(map[string]any)) legacy.Mapping {
	t.Helper()
	value := map[string]any{
		"schema_version": 1, "fingerprint": record, "created_at": "2025-01-01T00:00:00Z",
		"source_version": "v-old", "runtime_build": "build-old", "runtime_digest": strings.Repeat("a", 64),
		"channel": "marketing", "operation_id": record,
		"jobs": map[string]any{"job-1": map[string]any{"status": "project_dispatching", "advertiser_id": "123", "request_id": "request-" + record, "dispatch_key": "dispatch-" + record, "dispatch_state": "unknown"}},
	}
	if mutate != nil {
		mutate(value)
	}
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := legacy.Decode(legacy.Source{RootID: "frozen-root", RecordID: "runs/" + record + ".json", Checksum: legacy.Digest(payload)}, payload,
		[]legacy.RecoveryContract{{ID: "readonly-v1", SourceRuntimeVersion: "v-old", SourceRuntimeBuild: "build-old", SourceRuntimeDigest: strings.Repeat("a", 64), JournalSchema: "1", SnapshotSchema: "none", CompatibleRuntime: "core-tested"}})
	if err != nil {
		t.Fatal(err)
	}
	return mapping
}

func legacyJob(v map[string]any) map[string]any {
	return v["jobs"].(map[string]any)["job-1"].(map[string]any)
}

func TestLegacyImportRestartRepeatAndChangedSource(t *testing.T) {
	ctx := context.Background()
	store, options := legacyStore(t)
	mapping := legacyMapping(t, "source-a", nil)
	first, created, err := store.ImportLegacyMapping(ctx, mapping)
	if err != nil || !created || first.State != legacy.PendingReadOnly {
		t.Fatalf("first=%+v created=%v err=%v", first, created, err)
	}
	second, created, err := store.ImportLegacyMapping(ctx, mapping)
	if err != nil || created || first.OperationID != second.OperationID || first.Version != second.Version {
		t.Fatalf("repeat created=%v err=%v", created, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	again, created, err := reopened.ImportLegacyMapping(ctx, mapping)
	if err != nil || created || again.OperationID != first.OperationID || again.Items[0].TargetItemID != first.Items[0].TargetItemID {
		t.Fatalf("restart created=%v err=%v", created, err)
	}
	changed := legacyMapping(t, "source-a", func(v map[string]any) { legacyJob(v)["request_id"] = "changed-request" })
	if _, _, err := reopened.ImportLegacyMapping(ctx, changed); !errors.Is(err, legacy.ErrMappingConflict) {
		t.Fatalf("changed source: %v", err)
	}
	var count int
	if err := reopened.db.QueryRow("SELECT count(*) FROM legacy_mappings").Scan(&count); err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func TestLegacyUnknownScopeBlocksRootAndKnownScopeBlocksConservatively(t *testing.T) {
	ctx := context.Background()
	store, _ := legacyStore(t)
	mapping := legacyMapping(t, "known", nil)
	if _, _, err := store.ImportLegacyMapping(ctx, mapping); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []legacy.Scope{{Channel: "marketing", AdvertiserID: "123"}, {}} {
		if err := app.CheckWrites(ctx, store, scope); !errors.Is(err, legacy.ErrWriteBlocked) {
			t.Fatalf("known blocking %+v: %v", scope, err)
		}
	}
	for _, scope := range []legacy.Scope{{Channel: "marketing", AdvertiserID: "999"}, {Channel: "qianchuan", AdvertiserID: "123"}} {
		if err := app.CheckWrites(ctx, store, scope); err != nil {
			t.Fatalf("unrelated scope %+v: %v", scope, err)
		}
	}
	unknown := legacyMapping(t, "unknown", func(v map[string]any) { delete(v, "channel") })
	if unknown.State != legacy.QuarantinedUnknown {
		t.Fatal("unknown not quarantined")
	}
	if _, _, err := store.ImportLegacyMapping(ctx, unknown); err != nil {
		t.Fatal(err)
	}
	if err := app.CheckWrites(ctx, store, legacy.Scope{Channel: "qianchuan", AdvertiserID: "999"}); !errors.Is(err, legacy.ErrWriteBlocked) {
		t.Fatalf("root blocker not applied: %v", err)
	}
}

func TestLegacyCrossOperationDuplicatesRemainQuarantinedAfterRestart(t *testing.T) {
	for _, kind := range []string{"request", "dispatch", "business", "source-operation", "object"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			store, options := legacyStore(t)
			mutate := func(v map[string]any) {
				switch kind {
				case "request":
					legacyJob(v)["request_id"] = "shared-request"
				case "dispatch":
					legacyJob(v)["dispatch_key"] = "shared-dispatch"
				case "business":
					v["fingerprint"] = "shared-fingerprint"
				case "source-operation":
					v["operation_id"] = "shared-source-operation"
				case "object":
					legacyJob(v)["promotion_id"] = "456"
				}
			}
			first := legacyMapping(t, "op-a", mutate)
			second := legacyMapping(t, "op-b", mutate)
			if _, _, err := store.ImportLegacyMapping(ctx, first); err != nil {
				t.Fatal(err)
			}
			result, created, err := store.ImportLegacyMapping(ctx, second)
			if err != nil || !created || result.State != legacy.QuarantinedUnknown {
				t.Fatalf("duplicate state=%s created=%v err=%v", result.State, created, err)
			}
			prior, err := store.GetLegacyMapping(ctx, first.Source.RootID, first.Source.RecordID)
			if err != nil || prior.State != legacy.QuarantinedUnknown {
				t.Fatalf("prior=%+v err=%v", prior, err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(ctx, options)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			for _, original := range []legacy.Mapping{first, second} {
				result, created, err := reopened.ImportLegacyMapping(ctx, original)
				if err != nil || created || result.State != legacy.QuarantinedUnknown || result.Items[0].Reason != "duplicate_evidence" {
					t.Fatalf("repeat state=%s created=%v err=%v", result.State, created, err)
				}
			}
		})
	}
}

func TestLegacyClosedHistoryAndUnknownSourceVersion(t *testing.T) {
	ctx := context.Background()
	store, _ := legacyStore(t)
	history := legacyMapping(t, "closed", func(v map[string]any) {
		j := legacyJob(v)
		j["status"] = "completed"
		j["promotion_id"] = "456"
		j["dispatch_state"] = "acknowledged"
	})
	result, _, err := store.ImportLegacyMapping(ctx, history)
	if err != nil || result.State != legacy.HistoryReadOnly {
		t.Fatalf("history state=%s err=%v", result.State, err)
	}
	if err := app.CheckWrites(ctx, store, legacy.Scope{Channel: "marketing", AdvertiserID: "123"}); err != nil {
		t.Fatalf("history blocks: %v", err)
	}
	if err := app.CheckWrites(ctx, store, legacy.Scope{}); !errors.Is(err, legacy.ErrWriteBlocked) {
		t.Fatalf("unknown scope was not blocked: %v", err)
	}
	unknown := legacyMapping(t, "no-version", func(v map[string]any) { delete(v, "source_version"); delete(legacyJob(v), "request_id") })
	result, _, err = store.ImportLegacyMapping(ctx, unknown)
	if err != nil || result.State != legacy.QuarantinedUnknown || result.RuntimeVersion != legacy.Unknown || result.Items[0].RequestID != legacy.Unknown {
		t.Fatalf("unknown %+v err=%v", result, err)
	}
}

func TestLegacyImportAtomicRollback(t *testing.T) {
	store, _ := legacyStore(t)
	ctx := context.Background()
	if _, err := store.db.Exec(`CREATE TRIGGER legacy_injected_failure BEFORE INSERT ON legacy_items BEGIN SELECT RAISE(ABORT,'synthetic failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ImportLegacyMapping(ctx, legacyMapping(t, "atomic", nil)); err == nil {
		t.Fatal("injected failure ignored")
	}
	for _, table := range []string{"legacy_mappings", "legacy_items", "legacy_evidence_keys"} {
		var count int
		if err := store.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
}

func TestLegacySyntheticFileImporterDoesNotPersistPayloadOrApproval(t *testing.T) {
	store, _ := legacyStore(t)
	ctx := context.Background()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "runs"), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"schema_version":1,"fingerprint":"fixture","channel":"marketing","approval":{"confirmed":true},"access_token":"SYNTHETIC_SECRET","jobs":{"https://synthetic.invalid/private":{"status":"project_dispatching","advertiser_id":"123","request_id":"request","payload":{"token":"SYNTHETIC_SECRET"},"last_response":{"message":"RAW_OFFICIAL_RESPONSE"}}}}`)
	file := filepath.Join(root, "runs", "synthetic.json")
	if err := os.WriteFile(file, payload, 0o400); err != nil {
		t.Fatal(err)
	}
	importer := app.Importer{Source: filesystem.LegacyJournalReader{Root: root}, Mappings: store}
	source := legacy.Source{RootID: "synthetic-root", RecordID: "runs/synthetic.json", Checksum: legacy.Digest(payload)}
	result, created, err := importer.Import(ctx, source)
	if err != nil || !created || result.State != legacy.QuarantinedUnknown {
		t.Fatalf("result=%+v created=%v err=%v", result, created, err)
	}
	if _, created, err := importer.Import(ctx, source); err != nil || created {
		t.Fatalf("repeat created=%v err=%v", created, err)
	}
	persisted, err := store.GetLegacyMapping(ctx, source.RootID, source.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(persisted)
	for _, forbidden := range []string{"SYNTHETIC_SECRET", "https:", "RAW_OFFICIAL_RESPONSE", "confirmed", "approval", "payload"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("mapping contains %q", forbidden)
		}
	}
	var online int
	if err := store.db.QueryRow("SELECT count(*) FROM operations").Scan(&online); err != nil || online != 0 {
		t.Fatalf("legacy importer populated executable operations: %d %v", online, err)
	}
	for _, suffix := range []string{"", "-wal"} {
		bytes, err := os.ReadFile(store.path + suffix)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"SYNTHETIC_SECRET", "https://synthetic.invalid/private", "RAW_OFFICIAL_RESPONSE"} {
			if strings.Contains(string(bytes), forbidden) {
				t.Fatalf("DB contains %q", forbidden)
			}
		}
	}
	unchanged, err := os.ReadFile(file)
	if err != nil || string(unchanged) != string(payload) {
		t.Fatal("source file changed")
	}
}

func TestLegacyBlockersFailClosedOnCorruptOrMissingEvidence(t *testing.T) {
	for _, mutation := range []string{
		"DELETE FROM legacy_items",
		"UPDATE legacy_items SET advertiser_id='999'",
		"UPDATE legacy_items SET channel='unknown'",
		"UPDATE legacy_items SET recovery_state='history_read_only'",
		"UPDATE legacy_mappings SET recovery_state='history_read_only'",
	} {
		t.Run(mutation, func(t *testing.T) {
			ctx := context.Background()
			store, _ := legacyStore(t)
			if _, _, err := store.ImportLegacyMapping(ctx, legacyMapping(t, "corrupt", nil)); err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.ExecContext(ctx, mutation); err != nil {
				t.Fatal(err)
			}
			if err := app.CheckWrites(ctx, store, legacy.Scope{Channel: "marketing", AdvertiserID: "123"}); err == nil {
				t.Fatal("corrupt evidence allowed writes")
			}
		})
	}
}

func TestLegacyImportChecksBoundDatabaseIdentity(t *testing.T) {
	ctx := context.Background()
	store, _ := legacyStore(t)
	mapping := legacyMapping(t, "identity", nil)
	if err := os.Rename(store.path, store.path+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.path, []byte("synthetic replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ImportLegacyMapping(ctx, mapping); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("identity replacement = %v", err)
	}
}

func TestLegacyConcurrentSameSourceImportsOnce(t *testing.T) {
	ctx := context.Background()
	store, options := legacyStore(t)
	other, err := Open(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	mapping := legacyMapping(t, "concurrent", nil)
	type result struct {
		created bool
		err     error
	}
	results := make(chan result, 8)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			repo := store
			if i%2 == 1 {
				repo = other
			}
			_, created, err := repo.ImportLegacyMapping(ctx, mapping)
			results <- result{created, err}
		}(i)
	}
	workers.Wait()
	close(results)
	created := 0
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("created %d mappings", created)
	}
}
