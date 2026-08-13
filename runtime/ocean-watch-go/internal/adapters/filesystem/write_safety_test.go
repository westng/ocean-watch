package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
)

func TestOperationJournalIsAtomicPrivateAndCompatible(t *testing.T) {
	root := t.TempDir()
	store := OperationJournalStore{Root: root}
	journal, err := domainplans.NewJournal(
		"fixture-fingerprint",
		map[string]domainplans.JournalJob{
			"creator-1": {Status: "project_created", AdvertiserID: "123", ProjectID: "456"},
		},
		time.Date(2026, time.July, 26, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), "creator-batch-fixture", journal); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "runs", "creator-batch-fixture.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("journal mode is %o", info.Mode().Perm())
	}
	loaded, err := store.Load(context.Background(), "creator-batch-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Jobs["creator-1"].ProjectID != "456" {
		t.Fatalf("unexpected journal: %#v", loaded)
	}
	rows, err := (RunStore{Root: filepath.Join(root, "runs")}).List(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].StatusCounts["project_created"] != 1 {
		t.Fatalf("run store did not read operation journal: %#v", rows)
	}
}

func TestOperationJournalPreservesLegacyExtensions(t *testing.T) {
	root := t.TempDir()
	runs := filepath.Join(root, "runs")
	if err := os.MkdirAll(runs, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(runs, "creator-batch-python.json")
	payload := `{"schema_version":1,"fingerprint":"fixture","created_at":"2026-07-26T00:00:00Z","batch_note":{"owner":"python"},"jobs":{"creator-1":{"status":"promotion_failed","advertiser_id":"123","project_id":"456","failure_stage":"promotion_create","last_response":{"code":40000,"message":"fixture rejection","request_id":"request-1"},"aweme_id":"789","item_ids":["9007199254740993"]}}}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	store := OperationJournalStore{Root: root}
	journal, err := store.Load(context.Background(), "creator-batch-python")
	if err != nil {
		t.Fatal(err)
	}
	job := journal.Jobs["creator-1"]
	job.Status = "completed"
	journal.Jobs["creator-1"] = job
	if err := store.Save(context.Background(), "creator-batch-python", journal); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(written)))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["batch_note"].(map[string]any)["owner"] != "python" {
		t.Fatalf("top-level extension was lost: %s", written)
	}
	decodedJob := decoded["jobs"].(map[string]any)["creator-1"].(map[string]any)
	if decodedJob["aweme_id"] != "789" || decodedJob["item_ids"].([]any)[0] != "9007199254740993" {
		t.Fatalf("job extensions were lost: %s", written)
	}
	lastResponse := decodedJob["last_response"].(map[string]any)
	if decodedJob["failure_stage"] != "promotion_create" || lastResponse["code"] != json.Number("40000") ||
		lastResponse["message"] != "fixture rejection" || lastResponse["request_id"] != "request-1" {
		t.Fatalf("official response diagnostics changed: %s", written)
	}
}

func TestOperationJournalAcceptsLegacyCreatorBatchJobKeys(t *testing.T) {
	root := t.TempDir()
	store := OperationJournalStore{Root: root}
	jobKey := "marketing:9007199254740993:巨量营销-模板:creator-one:8101,8102"
	journal, err := domainplans.NewJournal(
		"creator-batch-fingerprint",
		map[string]domainplans.JournalJob{
			jobKey: {
				Status:       "promotion_failed",
				AdvertiserID: "9007199254740993",
				ProjectID:    "9007199254740995",
				Extra: map[string]json.RawMessage{
					"aweme_id":      json.RawMessage(`"creator-one"`),
					"item_ids":      json.RawMessage(`["8101","8102"]`),
					"plan_template": json.RawMessage(`"巨量营销-模板"`),
				},
			},
		},
		time.Date(2026, 7, 26, 1, 2, 3, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), "creator-batch-20260726", journal); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background(), "creator-batch-20260726")
	if err != nil {
		t.Fatal(err)
	}
	job, ok := loaded.Jobs[jobKey]
	if !ok || job.ProjectID != "9007199254740995" {
		t.Fatalf("legacy creator journal job was not preserved: %#v", loaded.Jobs)
	}
	if string(job.Extra["plan_template"]) != `"巨量营销-模板"` {
		t.Fatalf("legacy journal extension field changed: %s", job.Extra["plan_template"])
	}
}

func TestOperationJournalRejectsManagedRootSymbolicLink(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	outside := t.TempDir()
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(stateRoot, "runs")); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	store := OperationJournalStore{Root: stateRoot}
	if _, err := store.ManagedPath("creator-batch-fixture"); err == nil ||
		!strings.Contains(err.Error(), "runs root must be a managed directory") {
		t.Fatalf("unexpected managed-root error: %v", err)
	}
	journal, err := domainplans.NewJournal(
		"fixture-fingerprint", map[string]domainplans.JournalJob{"creator-1": {Status: "pending"}}, time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), "creator-batch-fixture", journal); err == nil ||
		!strings.Contains(err.Error(), "runs root must be a managed directory") {
		t.Fatalf("unexpected save error: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, "creator-batch-fixture.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("journal escaped through runs symlink: %v", statErr)
	}
}

func TestOperationJournalRejectsJournalSymbolicLink(t *testing.T) {
	stateRoot := t.TempDir()
	runsRoot := filepath.Join(stateRoot, "runs")
	if err := os.MkdirAll(runsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(target, []byte(`{"sentinel":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(runsRoot, "creator-batch-link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	store := OperationJournalStore{Root: stateRoot}
	if _, err := store.ManagedPath("creator-batch-link"); err == nil ||
		err.Error() != "operation journal must be a regular managed file" {
		t.Fatalf("unexpected managed-path error: %v", err)
	}
	if _, err := store.Load(context.Background(), "creator-batch-link"); err == nil ||
		err.Error() != "operation journal must be a regular managed file" {
		t.Fatalf("unexpected load error: %v", err)
	}
	journal, err := domainplans.NewJournal(
		"fixture-fingerprint", map[string]domainplans.JournalJob{"creator-1": {Status: "pending"}}, time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), "creator-batch-link", journal); err == nil ||
		err.Error() != "operation journal must be a regular managed file" {
		t.Fatalf("unexpected save error: %v", err)
	}
	written, err := os.ReadFile(target)
	if err != nil || string(written) != `{"sentinel":true}` {
		t.Fatalf("symbolic-link target changed: %q, %v", written, err)
	}
}

func TestOperationJournalManagedPathAcceptsManagedMissingFile(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	store := OperationJournalStore{Root: stateRoot}
	want := filepath.Join(stateRoot, "runs", "creator-batch-fixture.json")
	got, err := store.ManagedPath("creator-batch-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("managed path = %q, want %q", got, want)
	}
	runID, err := store.RunIDFromManagedPath(want)
	if err != nil {
		t.Fatal(err)
	}
	if runID != "creator-batch-fixture" {
		t.Fatalf("run ID = %q", runID)
	}
}

func TestOperationJournalListIsPrefixBoundedAndRejectsSymbolicLinks(t *testing.T) {
	stateRoot := t.TempDir()
	store := OperationJournalStore{Root: stateRoot}
	journal, err := domainplans.NewJournal(
		"fixture-fingerprint",
		map[string]domainplans.JournalJob{"job": {Status: "pending", AdvertiserID: "123"}},
		time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, runID := range []string{
		"marketing-upload-two", "creator-batch-fixture", "marketing-upload-one",
	} {
		if err := store.Save(context.Background(), runID, journal); err != nil {
			t.Fatal(err)
		}
	}
	records, err := store.List(context.Background(), "marketing-upload-")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].RunID != "marketing-upload-one" || records[1].RunID != "marketing-upload-two" {
		t.Fatalf("journal prefix listing escaped or lost sort order: %#v", records)
	}
	for _, prefix := range []string{"marketing-upload", "../marketing-upload-", "marketing/upload-", ""} {
		if _, err := store.List(context.Background(), prefix); err == nil {
			t.Fatalf("unsafe journal prefix %q was accepted", prefix)
		}
	}

	target := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(target, []byte(`{"sentinel":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(stateRoot, "runs", "marketing-upload-link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if _, err := store.List(context.Background(), "marketing-upload-"); err == nil ||
		err.Error() != "operation journal must be a regular managed file" {
		t.Fatalf("journal listing followed a symbolic link: %v", err)
	}
}

func TestOperationJournalScopeLockValidatesAndSerializesFingerprint(t *testing.T) {
	stateRoot := t.TempDir()
	store := OperationJournalStore{Root: stateRoot, LockTimeout: 75 * time.Millisecond}
	firstScope := strings.Repeat("0a", 32)
	secondScope := strings.Repeat("0b", 32)
	for _, scope := range []string{"", "not-hex", strings.ToUpper(firstScope), firstScope + "00"} {
		if _, err := store.AcquireScope(context.Background(), scope); err == nil {
			t.Fatalf("invalid scope fingerprint %q was accepted", scope)
		}
	}

	releaseFirst, err := store.AcquireScope(context.Background(), firstScope)
	if err != nil {
		t.Fatal(err)
	}
	releaseOther, err := store.AcquireScope(context.Background(), secondScope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcquireScope(context.Background(), firstScope); err == nil ||
		!strings.Contains(err.Error(), "timed out waiting for process lock") {
		t.Fatalf("concurrent use of one upload scope was not serialized: %v", err)
	}
	if err := releaseOther(); err != nil {
		t.Fatal(err)
	}
	if err := releaseFirst(); err != nil {
		t.Fatal(err)
	}
	releaseAgain, err := store.AcquireScope(context.Background(), firstScope)
	if err != nil {
		t.Fatalf("released upload scope stayed locked: %v", err)
	}
	if err := releaseAgain(); err != nil {
		t.Fatal(err)
	}
}

func TestOperationJournalScopeLockRejectsManagedRootSymbolicLinks(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	outside := t.TempDir()
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(stateRoot, "locks")); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	store := OperationJournalStore{Root: stateRoot}
	fingerprint := strings.Repeat("0a", 32)
	if _, err := store.AcquireScope(context.Background(), fingerprint); err == nil ||
		!strings.Contains(err.Error(), "locks root must be a managed directory") {
		t.Fatalf("unexpected scope-lock root error: %v", err)
	}
	name := "marketing-upload-scope-" + fingerprint + ".lock"
	if _, err := os.Stat(filepath.Join(outside, name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scope lock escaped through locks symlink: %v", err)
	}
}

func TestOperationJournalScopeLockRejectsSymbolicLinkFile(t *testing.T) {
	stateRoot := t.TempDir()
	locksRoot := filepath.Join(stateRoot, "locks")
	if err := os.MkdirAll(locksRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside.lock")
	if err := os.WriteFile(target, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	fingerprint := strings.Repeat("0b", 32)
	name := "marketing-upload-scope-" + fingerprint + ".lock"
	if err := os.Symlink(target, filepath.Join(locksRoot, name)); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	store := OperationJournalStore{Root: stateRoot}
	if _, err := store.AcquireScope(context.Background(), fingerprint); err == nil ||
		err.Error() != "operation journal scope lock must be a regular managed file" {
		t.Fatalf("unexpected scope-lock file error: %v", err)
	}
	written, err := os.ReadFile(target)
	if err != nil || string(written) != "sentinel" {
		t.Fatalf("scope-lock symbolic-link target changed: %q, %v", written, err)
	}
}

func TestAdvertiserLockNamesMatchStableProtocol(t *testing.T) {
	tests := []struct {
		scope domainplans.WriteScope
		name  string
	}{
		{
			scope: domainplans.WriteScope{Channel: domainplans.ChannelMarketing, AdvertiserID: "123", LockFamily: domainplans.LockMarketingPlans},
			name:  "marketing-plans-123.lock",
		},
		{
			scope: domainplans.WriteScope{Channel: domainplans.ChannelQianchuan, AdvertiserID: "456", LockFamily: domainplans.LockQianchuanWorks},
			name:  "qianchuan-advertiser-456.lock",
		},
		{
			scope: domainplans.WriteScope{Channel: domainplans.ChannelQianchuan, AdvertiserID: "789", LockFamily: domainplans.LockPlanSettings},
			name:  "qianchuan-advertiser-789.lock",
		},
	}
	for _, testCase := range tests {
		name, err := advertiserLockName(testCase.scope)
		if err != nil {
			t.Fatal(err)
		}
		if name != testCase.name {
			t.Fatalf("got %q, want %q", name, testCase.name)
		}
	}
}

func TestQianchuanWriteFamiliesShareAdvertiserLock(t *testing.T) {
	works, err := advertiserLockName(domainplans.WriteScope{
		Channel: domainplans.ChannelQianchuan, AdvertiserID: "456", LockFamily: domainplans.LockQianchuanWorks,
	})
	if err != nil {
		t.Fatal(err)
	}
	settings, err := advertiserLockName(domainplans.WriteScope{
		Channel: domainplans.ChannelQianchuan, AdvertiserID: "456", LockFamily: domainplans.LockPlanSettings,
	})
	if err != nil {
		t.Fatal(err)
	}
	if works != settings || works != "qianchuan-advertiser-456.lock" {
		t.Fatalf("Qianchuan lock families diverged: works=%q settings=%q", works, settings)
	}
}

func TestAdvertiserLockRejectsManagedRootSymbolicLinks(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	outside := t.TempDir()
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(stateRoot, "locks")); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	store := AdvertiserLockStore{Root: stateRoot}
	scope := domainplans.WriteScope{
		Channel: domainplans.ChannelMarketing, AdvertiserID: "123",
		LockFamily: domainplans.LockMarketingPlans,
	}
	if _, err := store.Acquire(context.Background(), scope); err == nil ||
		!strings.Contains(err.Error(), "advertiser locks root must be a managed directory") {
		t.Fatalf("unexpected advertiser-lock root error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "marketing-plans-123.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("advertiser lock escaped through locks symlink: %v", err)
	}
}

func TestAdvertiserLockRejectsSymbolicLinkFile(t *testing.T) {
	stateRoot := t.TempDir()
	locksRoot := filepath.Join(stateRoot, "locks")
	if err := os.MkdirAll(locksRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside.lock")
	if err := os.WriteFile(target, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	name := "marketing-plans-123.lock"
	if err := os.Symlink(target, filepath.Join(locksRoot, name)); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	store := AdvertiserLockStore{Root: stateRoot}
	scope := domainplans.WriteScope{
		Channel: domainplans.ChannelMarketing, AdvertiserID: "123",
		LockFamily: domainplans.LockMarketingPlans,
	}
	if _, err := store.Acquire(context.Background(), scope); err == nil ||
		err.Error() != "advertiser lock must be a regular managed file" {
		t.Fatalf("unexpected advertiser-lock file error: %v", err)
	}
	written, err := os.ReadFile(target)
	if err != nil || string(written) != "sentinel" {
		t.Fatalf("advertiser-lock symbolic-link target changed: %q, %v", written, err)
	}
}
