package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/authorization"
	credentialport "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/credentials"
)

//go:embed migrations/003_credentials.sql
var credentialFixtureSQL string

func openCredentialTestStore(t *testing.T, path string, initialize bool) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", databaseURI(filepath.ToSlash(path), url.Values{
		"_pragma": {"foreign_keys(1)", "busy_timeout(2000)", "synchronous(FULL)"}, "_txlock": {"immediate"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = db.Close() })
	if initialize {
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			t.Fatal(err)
		}
		if err := LoadCredentialFixture(context.Background(), db, []byte(credentialFixtureSQL)); err != nil {
			t.Fatal(err)
		}
	}
	return &Store{db: db, path: path}
}

func credentialFixtureStore(t *testing.T) *Store {
	t.Helper()
	return openCredentialTestStore(t, filepath.Join(t.TempDir(), "credentials.db"), true)
}

func fixtureVersion(t *testing.T, revision int64) authorization.CredentialVersion {
	t.Helper()
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC).Add(time.Duration(revision) * time.Minute)
	version, err := authorization.NewCredentialVersion(authorization.Key{Channel: "marketing", AuthorizationID: "auth-1"}, revision, now, now.Add(time.Hour), now.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func seedFixture(t *testing.T, store *Store, version authorization.CredentialVersion) {
	t.Helper()
	if err := store.RegisterCredentialVersion(context.Background(), version); err != nil {
		t.Fatal(err)
	}
}

type credentialFakeBackend struct {
	mu           sync.Mutex
	entries      map[string]credentialport.Record
	writeFailure bool
}

func (backend *credentialFakeBackend) Read(_ context.Context, reference string) (credentialport.Record, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	record, ok := backend.entries[reference]
	if !ok {
		return credentialport.Record{}, credentialport.ErrNotFound
	}
	return record, nil
}

func (backend *credentialFakeBackend) Put(_ context.Context, record credentialport.Record) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.writeFailure {
		return errors.New("fake-refresh-secret write failed")
	}
	backend.entries[record.Version.Reference] = record
	return nil
}

type credentialFakeRefresh struct {
	mu          sync.Mutex
	calls       int
	now         time.Time
	fail        bool
	checkIntent func() error
}

func (refresh *credentialFakeRefresh) Refresh(ctx context.Context, _ authorization.Key, _ credentialport.Secrets) (credentialport.RefreshResult, error) {
	if _, bounded := ctx.Deadline(); !bounded {
		return credentialport.RefreshResult{}, errors.New("refresh is not bounded")
	}
	refresh.mu.Lock()
	refresh.calls++
	refresh.mu.Unlock()
	if refresh.checkIntent != nil {
		if err := refresh.checkIntent(); err != nil {
			return credentialport.RefreshResult{}, err
		}
	}
	if refresh.fail {
		return credentialport.RefreshResult{}, errors.New("fake-refresh-secret official result unknown")
	}
	return credentialport.RefreshResult{Secrets: credentialport.Secrets{AccessToken: "fake-access-secret", RefreshToken: "fake-refresh-secret"},
		AccessExpiresAt: refresh.now.Add(time.Hour), RefreshExpiresAt: refresh.now.Add(24 * time.Hour)}, nil
}

func credentialManager(store *Store, backend *credentialFakeBackend, refresh *credentialFakeRefresh, lockRoot string) auth.RotationManager {
	return auth.RotationManager{State: store, Backend: backend, Refresh: refresh,
		Locks: filesystem.RefreshLocker{StateRoot: lockRoot, Timeout: time.Second}, Now: func() time.Time { return refresh.now }}
}

func TestCredentialRotationSQLiteFailureRestart(t *testing.T) {
	for _, stage := range []string{"intent", "candidate_metadata", "activation", "completion"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			path := filepath.Join(root, "credentials.db")
			store := openCredentialTestStore(t, path, true)
			current := fixtureVersion(t, 1)
			seedFixture(t, store, current)
			backend := &credentialFakeBackend{entries: map[string]credentialport.Record{current.Reference: {
				Version: current, Secrets: credentialport.Secrets{AccessToken: "fake-old-access-secret", RefreshToken: "fake-old-refresh-secret"}}}}
			refresh := &credentialFakeRefresh{now: current.IssuedAt.Add(time.Minute)}
			refresh.checkIntent = func() error {
				var count int
				if err := store.db.QueryRow(`SELECT count(*) FROM credential_rotations WHERE state='intent'`).Scan(&count); err != nil {
					return err
				}
				if count != 1 {
					return errors.New("refresh started without durable intent")
				}
				return nil
			}
			triggers := map[string]string{
				"intent":             `BEFORE INSERT ON credential_rotations`,
				"candidate_metadata": `BEFORE INSERT ON credential_versions WHEN NEW.state='candidate'`,
				"activation":         `BEFORE UPDATE OF credential_ref ON credential_authorizations`,
				"completion":         `BEFORE UPDATE OF state ON credential_rotations WHEN NEW.state='completed'`,
			}
			if _, err := store.db.Exec(`CREATE TRIGGER credential_injected_failure ` + triggers[stage] + ` BEGIN SELECT RAISE(ABORT,'injected credential failure'); END`); err != nil {
				t.Fatal(err)
			}
			manager := credentialManager(store, backend, refresh, root)
			if _, err := manager.Rotate(ctx, current.Key, 1); err == nil || strings.Contains(err.Error(), "fake-refresh-secret") {
				t.Fatalf("injected failure not safely reported: %v", err)
			}
			persisted, err := store.LoadCredentialState(ctx, current.Key)
			if err != nil {
				t.Fatal(err)
			}
			if stage == "completion" {
				if persisted.Credential.Revision != 2 || persisted.Rotation == nil || persisted.Rotation.Status != authorization.RotationActivated {
					t.Fatalf("completion failure lost activation: %#v", persisted)
				}
			} else if persisted.Credential.Revision != 1 {
				t.Fatalf("failed transaction activated revision %d", persisted.Credential.Revision)
			}
			if stage == "activation" && (persisted.Rotation == nil || persisted.Rotation.Status != authorization.RotationCandidateStored) {
				t.Fatalf("candidate was lost: %#v", persisted)
			}
			if stage == "candidate_metadata" && (persisted.Rotation == nil || persisted.Rotation.Status != authorization.RotationIntent) {
				t.Fatalf("intent was lost: %#v", persisted)
			}
			if _, err := store.db.Exec("DROP TRIGGER credential_injected_failure"); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = openCredentialTestStore(t, path, false)
			manager = credentialManager(store, backend, refresh, root)
			got, err := manager.Rotate(ctx, current.Key, 1)
			if err != nil || got.Revision != 2 {
				t.Fatalf("reopen recovery = %#v %v", got, err)
			}
			if refresh.calls != 1 {
				t.Fatalf("recovery performed %d official refreshes", refresh.calls)
			}
			final, err := store.LoadCredentialState(ctx, current.Key)
			if err != nil || final.Rotation != nil || final.Credential.Revision != 2 {
				t.Fatalf("final state = %#v %v", final, err)
			}
			assertNoCredentialSecrets(t, store)
		})
	}
}

func TestCredentialRotationSQLiteUnknownAndUnsavedCandidateStayBlockedAfterRestart(t *testing.T) {
	for _, failure := range []string{"official_unknown", "credential_write", "crashed_after_intent"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "credentials.db")
			store := openCredentialTestStore(t, path, true)
			current := fixtureVersion(t, 1)
			seedFixture(t, store, current)
			backend := &credentialFakeBackend{entries: map[string]credentialport.Record{current.Reference: {
				Version: current, Secrets: credentialport.Secrets{AccessToken: "fake-old-access-secret", RefreshToken: "fake-old-refresh-secret"}}}, writeFailure: failure == "credential_write"}
			refresh := &credentialFakeRefresh{now: current.IssuedAt.Add(time.Minute), fail: failure == "official_unknown"}
			manager := credentialManager(store, backend, refresh, root)
			if failure == "crashed_after_intent" {
				intent, err := authorization.NewRotation(current, refresh.now)
				if err != nil {
					t.Fatal(err)
				}
				if err := store.BeginCredentialRotation(context.Background(), intent); err != nil {
					t.Fatal(err)
				}
			} else if _, err := manager.Rotate(context.Background(), current.Key, 1); !errors.Is(err, credentialport.ErrReauthorizationRequired) {
				t.Fatalf("result = %v", err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = openCredentialTestStore(t, path, false)
			manager = credentialManager(store, backend, refresh, root)
			if _, err := manager.Rotate(context.Background(), current.Key, 1); !errors.Is(err, credentialport.ErrReauthorizationRequired) {
				t.Fatalf("restart = %v", err)
			}
			state, err := store.LoadCredentialState(context.Background(), current.Key)
			if err != nil || state.Status != authorization.ReauthorizationRequired || state.Rotation == nil {
				t.Fatalf("not durably blocked: %#v %v", state, err)
			}
			want := 1
			if failure == "crashed_after_intent" {
				want = 0
			}
			if refresh.calls != want {
				t.Fatalf("refresh calls = %d, want %d", refresh.calls, want)
			}
			assertNoCredentialSecrets(t, store)
		})
	}
}

func TestCredentialRotationSQLiteConcurrentManagersAndRepeatedRotation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "credentials.db")
	store := openCredentialTestStore(t, path, true)
	other := openCredentialTestStore(t, path, false)
	current := fixtureVersion(t, 1)
	seedFixture(t, store, current)
	backend := &credentialFakeBackend{entries: map[string]credentialport.Record{current.Reference: {
		Version: current, Secrets: credentialport.Secrets{AccessToken: "fake-old-access-secret", RefreshToken: "fake-old-refresh-secret"}}}}
	refresh := &credentialFakeRefresh{now: current.IssuedAt.Add(time.Minute)}
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for index := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			selected := store
			if index%2 == 1 {
				selected = other
			}
			got, err := credentialManager(selected, backend, refresh, root).Rotate(context.Background(), current.Key, 1)
			if err == nil && got.Revision != 2 {
				err = fmt.Errorf("wrong revision: %d", got.Revision)
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if refresh.calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", refresh.calls)
	}
	refresh.now = refresh.now.Add(time.Minute)
	got, err := credentialManager(other, backend, refresh, root).Rotate(context.Background(), current.Key, 2)
	if err != nil || got.Revision != 3 || refresh.calls != 2 {
		t.Fatalf("second rotation = %#v %v, calls=%d", got, err, refresh.calls)
	}
	var count int
	if err := store.db.QueryRow("SELECT count(*) FROM credential_rotations WHERE state='completed'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("completed rotation history = %d, want 2", count)
	}
	assertNoCredentialSecrets(t, store)
}

func TestCredentialRotationSQLiteRejectsTamperedCandidateMetadata(t *testing.T) {
	store := credentialFixtureStore(t)
	current := fixtureVersion(t, 1)
	seedFixture(t, store, current)
	rotation, err := authorization.NewRotation(current, current.IssuedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	candidate := fixtureVersion(t, 2)
	if err := store.BeginCredentialRotation(context.Background(), rotation); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordCredentialCandidate(context.Background(), current.Key, 2, candidate); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE credential_versions SET credential_digest=? WHERE revision=2`, "sha256:"+strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	if err := store.ActivateCredentialCandidate(context.Background(), current.Key, 2, candidate.IssuedAt); !errors.Is(err, credentialport.ErrInvalidState) {
		t.Fatalf("tampered candidate activation = %v", err)
	}
	var revision int64
	if err := store.db.QueryRow("SELECT token_revision FROM credential_authorizations").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if revision != 1 {
		t.Fatalf("tampered candidate activated: %d", revision)
	}
}

func TestCredentialRotationNeverPersistsOrFormatsSecrets(t *testing.T) {
	secret := credentialport.Secrets{AccessToken: "fake-access-secret", RefreshToken: "fake-refresh-secret"}
	for _, value := range []any{secret, credentialport.Record{Secrets: secret}, credentialport.RefreshResult{Secrets: secret}} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			if got := fmt.Sprintf(format, value); strings.Contains(got, "fake-") {
				t.Fatal("secret escaped redacted formatting")
			}
		}
		payload, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(payload, []byte("fake-")) {
			t.Fatal("secret escaped JSON redaction")
		}
	}
}

func assertNoCredentialSecrets(t *testing.T, store *Store) {
	t.Helper()
	for _, table := range []string{"credential_authorizations", "credential_versions", "credential_rotations"} {
		rows, err := store.db.Query("SELECT * FROM " + table)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(fmt.Sprint(values), "fake-") {
				t.Fatal("secret was written to SQLite")
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		payload, err := os.ReadFile(store.path + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"fake-access-secret", "fake-refresh-secret", "fake-old-access-secret", "fake-old-refresh-secret"} {
			if bytes.Contains(payload, []byte(secret)) {
				t.Fatal("secret was written to SQLite files")
			}
		}
	}
}
