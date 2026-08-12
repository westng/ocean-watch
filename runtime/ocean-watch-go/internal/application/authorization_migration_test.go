package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

func TestAuthorizationMigrationResumesEveryCheckpointWithoutDuplicateAuthorization(t *testing.T) {
	checkpoints := []MigrationCheckpoint{
		CheckpointCredentialsPersisted,
		CheckpointCredentialsJournaled,
		CheckpointAuthorizationPersisted,
		CheckpointAuthorizationJournaled,
		CheckpointConfigurationPersisted,
		CheckpointConfigurationJournaled,
		CheckpointMigrationActivated,
	}
	for _, checkpoint := range checkpoints {
		t.Run(string(checkpoint), func(t *testing.T) {
			fixture := newAuthorizationMigrationFixture(t)
			injected := errors.New("injected migration interruption")
			fixture.migration.AfterCheckpoint = func(current MigrationCheckpoint) error {
				if current == checkpoint {
					return injected
				}
				return nil
			}
			if _, err := fixture.migration.Migrate(context.Background(), false); !errors.Is(err, injected) {
				t.Fatalf("checkpoint %s returned %v", checkpoint, err)
			}

			journalBeforeResume := configuration.CloneMap(fixture.journal.value)
			fixture.migration.AfterCheckpoint = nil
			result, err := fixture.migration.Migrate(context.Background(), false)
			if err != nil {
				t.Fatal(err)
			}
			if result["migration_id"] != journalBeforeResume["migration_id"] ||
				fixture.journal.value["authorization_id"] != journalBeforeResume["authorization_id"] {
				t.Fatalf("migration identity changed after resume: before=%#v after=%#v", journalBeforeResume, fixture.journal.value)
			}
			fixture.assertCompleted(t)

			writesBeforeIdempotentRun := fixture.totalWrites()
			resultAgain, err := fixture.migration.Migrate(context.Background(), false)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(result, resultAgain) {
				t.Fatalf("idempotent result changed: first=%#v second=%#v", result, resultAgain)
			}
			if writesAfter := fixture.totalWrites(); writesAfter != writesBeforeIdempotentRun {
				t.Fatalf("completed migration wrote state again: before=%d after=%d", writesBeforeIdempotentRun, writesAfter)
			}
		})
	}
}

type authorizationMigrationFixture struct {
	migration      AuthorizationMigration
	config         *memoryMigrationConfigStore
	credentials    *memoryCredentialStore
	authorizations *memoryAuthorizationMigrationStore
	journal        *memoryMigrationJournalStore
}

func newAuthorizationMigrationFixture(t *testing.T) *authorizationMigrationFixture {
	t.Helper()
	config := &memoryMigrationConfigStore{value: map[string]any{
		"future": map[string]any{"preserved": true},
		"api": map[string]any{
			"base_url":                        "https://api.oceanengine.com/open_api",
			"app_id":                          "fixture-app-id",
			"developer_id":                    "fixture-developer-id",
			"secret":                          "fixture-app-secret",
			"auth_code":                       "fixture-auth-code",
			"access_token":                    "fixture-access-token",
			"refresh_token":                   "fixture-refresh-token",
			"access_token_expires_at":         "2030-01-01T00:00:00Z",
			"refresh_token_expires_at":        "2031-01-01T00:00:00Z",
			"last_token_update_at":            "2026-07-25T00:00:00Z",
			"last_authorized_account_sync_at": "2026-07-25T00:01:00Z",
			"oauth_authorized_accounts": []any{
				map[string]any{
					"account_id": "9000000000000001", "account_role": "ADVERTISER",
					"account_name": "Fixture Advertiser",
				},
			},
			"authorized_advertiser_ids": []any{"9000000000000001", "9000000000000002"},
		},
		"oauth": map[string]any{"redirect_uri": "http://127.0.0.1:8787/oauth/callback"},
		"account": map[string]any{
			"advertiser_id": "9000000000000001",
			"future":        "account-field",
		},
	}}
	credentials := &memoryCredentialStore{entries: map[string]map[string]any{}}
	authorizations := &memoryAuthorizationMigrationStore{channels: map[string]map[string]any{}}
	journal := &memoryMigrationJournalStore{}
	identifiers := []string{"fixture-migration-id", "fixture-authorization-id"}
	identifierIndex := 0
	fixture := &authorizationMigrationFixture{
		config: config, credentials: credentials, authorizations: authorizations, journal: journal,
	}
	fixture.migration = AuthorizationMigration{
		Config: config, Credentials: credentials, Authorizations: authorizations, Journal: journal,
		ConfigPath: t.TempDir() + "/config.json",
		NewID: func() (string, error) {
			if identifierIndex >= len(identifiers) {
				return "", errors.New("migration generated a replacement identity")
			}
			identifier := identifiers[identifierIndex]
			identifierIndex++
			return identifier, nil
		},
	}
	return fixture
}

func (fixture *authorizationMigrationFixture) assertCompleted(t *testing.T) {
	t.Helper()
	if fixture.journal.value["activation"] != MigrationActive ||
		fixture.journal.value["credentials"] != MigrationCommitted ||
		fixture.journal.value["authorization"] != MigrationCommitted ||
		fixture.journal.value["config"] != MigrationCommitted {
		t.Fatalf("migration journal is incomplete: %#v", fixture.journal.value)
	}
	if fixture.config.commits != 1 {
		t.Fatalf("configuration commit count = %d, want 1", fixture.config.commits)
	}
	if fixture.authorizations.commits != 1 {
		t.Fatalf("authorization commit count = %d, want 1", fixture.authorizations.commits)
	}
	state := fixture.authorizations.channels["marketing"]
	authorizations, _ := state["authorizations"].(map[string]any)
	if len(authorizations) != 1 || authorizations["fixture-authorization-id"] == nil {
		t.Fatalf("authorization was duplicated or replaced: %#v", state)
	}
	if state["generation"] != 1 {
		t.Fatalf("authorization generation = %#v, want 1", state["generation"])
	}

	legacy := fixture.credentials.entries["oceanengine-oauth"]
	for _, field := range []string{"developer_id", "auth_code", "access_token", "refresh_token"} {
		if legacy[field] == nil {
			t.Fatalf("legacy compatibility credential lost %s: %#v", field, legacy)
		}
	}
	if app := fixture.credentials.entries["oceanengine-app-marketing"]; !reflect.DeepEqual(app, map[string]any{
		"app_id": "fixture-app-id", "secret": "fixture-app-secret",
	}) {
		t.Fatalf("unexpected app credential projection: %#v", app)
	}
	authorizationCredential := fixture.credentials.entries["oceanengine-auth-marketing-fixture-authorization-id-r1"]
	wantAuthorizationCredential := map[string]any{
		"access_token":             "fixture-access-token",
		"refresh_token":            "fixture-refresh-token",
		"access_token_expires_at":  "2030-01-01T00:00:00Z",
		"refresh_token_expires_at": "2031-01-01T00:00:00Z",
		"last_token_update_at":     "2026-07-25T00:00:00Z",
	}
	if !reflect.DeepEqual(authorizationCredential, wantAuthorizationCredential) {
		t.Fatalf("unexpected authorization credential projection: %#v", authorizationCredential)
	}

	if configuration.Value(fixture.config.value, "future.preserved") != true ||
		configuration.Value(fixture.config.value, "account.future") != "account-field" {
		t.Fatalf("unknown configuration field was lost: %#v", fixture.config.value)
	}
	if fields := configuration.SensitiveFields(fixture.config.value); len(fields) != 0 {
		t.Fatalf("sensitive configuration fields survived: %#v", fields)
	}
	journalPayload, err := json.Marshal(fixture.journal.value)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{
		"fixture-app-id", "fixture-developer-id", "fixture-app-secret", "fixture-auth-code",
		"fixture-access-token", "fixture-refresh-token",
	} {
		if strings.Contains(string(journalPayload), secret) {
			t.Fatalf("migration journal contains sensitive value %q: %s", secret, journalPayload)
		}
	}
}

func (fixture *authorizationMigrationFixture) totalWrites() int {
	return fixture.config.commits + fixture.credentials.writes + fixture.authorizations.commits + fixture.journal.writes
}

type memoryMigrationConfigStore struct {
	value    map[string]any
	revision int
	commits  int
}

func (store *memoryMigrationConfigStore) ReadWithRevision(context.Context) (map[string]any, string, error) {
	return configuration.CloneMap(store.value), fmt.Sprint(store.revision), nil
}

func (store *memoryMigrationConfigStore) CommitMigration(_ context.Context, revision string, value map[string]any) error {
	if revision != fmt.Sprint(store.revision) {
		return errors.New("stale in-memory configuration revision")
	}
	store.value = configuration.CloneMap(value)
	store.revision++
	store.commits++
	return nil
}

type memoryCredentialStore struct {
	entries map[string]map[string]any
	writes  int
}

func (*memoryCredentialStore) BackendName() string { return "memory" }

func (store *memoryCredentialStore) Read(_ context.Context, account string) (map[string]any, error) {
	return configuration.CloneMap(store.entries[account]), nil
}

func (store *memoryCredentialStore) Write(_ context.Context, account string, value map[string]any) (string, error) {
	store.entries[account] = configuration.CloneMap(value)
	store.writes++
	return "memory", nil
}

type memoryAuthorizationMigrationStore struct {
	channels map[string]map[string]any
	commits  int
}

func (store *memoryAuthorizationMigrationStore) LoadChannel(_ context.Context, channel string) (map[string]any, error) {
	return configuration.CloneMap(store.channels[channel]), nil
}

func (store *memoryAuthorizationMigrationStore) CommitChannel(_ context.Context, channel string, value map[string]any) error {
	store.channels[channel] = configuration.CloneMap(value)
	store.commits++
	return nil
}

type memoryMigrationJournalStore struct {
	value  map[string]any
	writes int
	locked bool
}

func (store *memoryMigrationJournalStore) Acquire(context.Context) (func() error, error) {
	if store.locked {
		return nil, errors.New("migration journal lock is already held")
	}
	store.locked = true
	return func() error {
		store.locked = false
		return nil
	}, nil
}

func (store *memoryMigrationJournalStore) Read(context.Context) (map[string]any, bool, error) {
	if store.value == nil {
		return nil, false, nil
	}
	return configuration.CloneMap(store.value), true, nil
}

func (store *memoryMigrationJournalStore) Write(_ context.Context, value map[string]any) error {
	store.value = configuration.CloneMap(value)
	store.writes++
	return nil
}
