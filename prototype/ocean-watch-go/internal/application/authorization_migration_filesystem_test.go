package application_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/configuration"
)

func TestAuthorizationMigrationFilesystemRoundTripIsIdempotentAndRedacted(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateRoot := filepath.Join(root, "state")
	legacyConfig := map[string]any{
		"future": map[string]any{"preserved": "unknown-field"},
		"api": map[string]any{
			"base_url":                        "https://api.oceanengine.com/open_api",
			"app_id":                          "fixture-app-id",
			"developer_id":                    "fixture-developer-id",
			"secret":                          "fixture-app-secret",
			"auth_code":                       "fixture-auth-code",
			"access_token":                    "fixture-access-token",
			"refresh_token":                   "fixture-refresh-token",
			"last_authorized_account_sync_at": "2026-07-25T00:01:00Z",
			"oauth_authorized_accounts": []any{
				map[string]any{
					"account_id": "9000000000000001", "account_role": "ADVERTISER",
					"account_name": "Fixture Advertiser",
				},
			},
			"authorized_advertiser_ids": []any{"9000000000000001"},
		},
		"account": map[string]any{
			"advertiser_id": "9000000000000001",
			"future":        map[string]any{"preserved": true},
		},
	}
	payload, err := json.MarshalIndent(legacyConfig, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, append(payload, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	credentials := &migrationMemoryCredentials{entries: map[string]map[string]any{}}
	identifiers := []string{"fixture-migration-id", "fixture-authorization-id"}
	identifierIndex := 0
	migration := application.AuthorizationMigration{
		Config:         filesystem.ConfigStore{Path: configPath},
		Credentials:    credentials,
		Authorizations: filesystem.AuthorizationStore{Root: stateRoot},
		Journal:        filesystem.MigrationJournalStore{Root: stateRoot},
		ConfigPath:     configPath,
		NewID: func() (string, error) {
			if identifierIndex >= len(identifiers) {
				return "", errors.New("migration generated a replacement identity")
			}
			identifier := identifiers[identifierIndex]
			identifierIndex++
			return identifier, nil
		},
	}

	first, err := migration.Migrate(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	durablePaths := []string{
		configPath,
		filepath.Join(stateRoot, "migration", "journal.json"),
		filepath.Join(stateRoot, "channels", "marketing", "current.json"),
		filepath.Join(stateRoot, "channels", "marketing", "manifest-1.json"),
	}
	before := fileDigests(t, durablePaths)

	second, err := migration.Migrate(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	after := fileDigests(t, durablePaths)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("second migration result changed: first=%#v second=%#v", first, second)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("second migration changed durable files: before=%#v after=%#v", before, after)
	}

	migrated, err := (filesystem.ConfigStore{Path: configPath}).Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Value(migrated, "future.preserved") != "unknown-field" ||
		configuration.Value(migrated, "account.future.preserved") != true {
		t.Fatalf("unknown fields were lost: %#v", migrated)
	}
	if fields := configuration.SensitiveFields(migrated); len(fields) != 0 {
		t.Fatalf("sensitive config fields survived: %#v", fields)
	}

	journal, err := os.ReadFile(filepath.Join(stateRoot, "migration", "journal.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, sensitive := range []string{
		"fixture-app-id", "fixture-developer-id", "fixture-app-secret", "fixture-auth-code",
		"fixture-access-token", "fixture-refresh-token",
	} {
		if strings.Contains(string(journal), sensitive) {
			t.Fatalf("migration journal contains sensitive value %q: %s", sensitive, journal)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(stateRoot, "channels", "marketing", "manifest-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), "fixture-access-token") || strings.Contains(string(manifest), "fixture-refresh-token") {
		t.Fatalf("authorization manifest contains credentials: %s", manifest)
	}
	if credentials.writes != 3 {
		t.Fatalf("credential writes = %d, want one compatibility, app, and authorization write", credentials.writes)
	}
}

type migrationMemoryCredentials struct {
	entries map[string]map[string]any
	writes  int
}

func (*migrationMemoryCredentials) BackendName() string { return "memory" }

func (store *migrationMemoryCredentials) Read(_ context.Context, account string) (map[string]any, error) {
	return configuration.CloneMap(store.entries[account]), nil
}

func (store *migrationMemoryCredentials) Write(_ context.Context, account string, value map[string]any) (string, error) {
	store.entries[account] = configuration.CloneMap(value)
	store.writes++
	return "memory", nil
}

func fileDigests(t *testing.T, paths []string) map[string]string {
	t.Helper()
	result := make(map[string]string, len(paths))
	for _, path := range paths {
		payload, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(payload)
		result[path] = hex.EncodeToString(digest[:])
	}
	return result
}
