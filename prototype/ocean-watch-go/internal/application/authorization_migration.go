package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/configuration"
	domaintemplates "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/templates"
)

const (
	MigrationJournalSchemaVersion = 1
	MigrationActive               = "schema_v2_active"
	MigrationPending              = "pending"
	MigrationCommitted            = "committed"
)

type MigrationCheckpoint string

const (
	CheckpointCredentialsPersisted   MigrationCheckpoint = "credentials_persisted"
	CheckpointCredentialsJournaled   MigrationCheckpoint = "credentials_journaled"
	CheckpointAuthorizationPersisted MigrationCheckpoint = "authorization_persisted"
	CheckpointAuthorizationJournaled MigrationCheckpoint = "authorization_journaled"
	CheckpointConfigurationPersisted MigrationCheckpoint = "configuration_persisted"
	CheckpointConfigurationJournaled MigrationCheckpoint = "configuration_journaled"
	CheckpointMigrationActivated     MigrationCheckpoint = "migration_activated"
)

type AuthorizationMigration struct {
	Config          MigrationConfigStore
	Credentials     CredentialStore
	Authorizations  AuthorizationMigrationStore
	Journal         MigrationJournalStore
	ConfigPath      string
	NewID           func() (string, error)
	AfterCheckpoint func(MigrationCheckpoint) error
}

func (migration AuthorizationMigration) Migrate(
	ctx context.Context,
	confirmRemoveLegacyMaterials bool,
) (map[string]any, error) {
	if migration.Config == nil || migration.Credentials == nil || migration.Authorizations == nil || migration.Journal == nil {
		return nil, errors.New("authorization migration dependencies are incomplete")
	}
	release, err := migration.Journal.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = release() }()

	raw, revision, err := migration.Config.ReadWithRevision(ctx)
	if err != nil {
		return nil, err
	}
	prepared, legacyMaterialError, err := prepareMigrationConfig(raw, confirmRemoveLegacyMaterials)
	if err != nil {
		return nil, err
	}
	if legacyMaterialError != nil {
		return nil, legacyMaterialError
	}
	configPath, err := filepath.Abs(filepath.Clean(migration.ConfigPath))
	if err != nil {
		return nil, fmt.Errorf("resolve migration config path: %w", err)
	}
	journal, err := migration.loadOrCreateJournal(ctx, configPath)
	if err != nil {
		return nil, err
	}
	extracted, err := configuration.ExtractLegacyCredentials(raw, "marketing")
	if err != nil {
		return nil, err
	}
	legacy, err := migration.Credentials.Read(ctx, "oceanengine-oauth")
	if err != nil {
		return nil, fmt.Errorf("read legacy credentials: %w", err)
	}
	mergedLegacy := mergeMigrationCredentials(legacy, extracted)
	currentAuthorization, err := migration.Authorizations.LoadChannel(ctx, "marketing")
	if err != nil {
		return nil, err
	}
	authorizationID, err := journalRequiredString(journal, "authorization_id")
	if err != nil {
		return nil, err
	}
	plan, err := domain.PrepareLegacyMarketingAuthorization(currentAuthorization, mergedLegacy, authorizationID)
	if err != nil {
		return nil, err
	}

	if journalString(journal, "credentials") != MigrationCommitted || len(extracted) != 0 {
		if len(extracted) != 0 {
			if _, err := migration.Credentials.Write(ctx, "oceanengine-oauth", mergedLegacy); err != nil {
				return nil, fmt.Errorf("persist legacy credential compatibility entry: %w", err)
			}
		}
		if err := migration.persistSplitCredentials(ctx, mergedLegacy, authorizationID, plan); err != nil {
			return nil, err
		}
		if err := migration.checkpoint(CheckpointCredentialsPersisted); err != nil {
			return nil, err
		}
		journal["credential_result"] = cloneMigrationValue(plan.Result)
		journal["config_sensitive_fields_migrated"] = migrationSortedKeys(extracted)
		journal["credentials"] = MigrationCommitted
		if err := migration.Journal.Write(ctx, journal); err != nil {
			return nil, err
		}
		if err := migration.checkpoint(CheckpointCredentialsJournaled); err != nil {
			return nil, err
		}
	}

	if journalString(journal, "authorization") != MigrationCommitted {
		if plan.CommitAuthorization {
			if err := migration.Authorizations.CommitChannel(ctx, "marketing", plan.State); err != nil {
				return nil, err
			}
		}
		if err := migration.checkpoint(CheckpointAuthorizationPersisted); err != nil {
			return nil, err
		}
		journal["authorization"] = MigrationCommitted
		if _, exists := journal["credential_result"]; !exists {
			journal["credential_result"] = cloneMigrationValue(plan.Result)
		}
		if err := migration.Journal.Write(ctx, journal); err != nil {
			return nil, err
		}
		if err := migration.checkpoint(CheckpointAuthorizationJournaled); err != nil {
			return nil, err
		}
	}

	if journalString(journal, "config") != MigrationCommitted || !configuration.Equal(raw, prepared) {
		if !configuration.Equal(raw, prepared) {
			if err := migration.Config.CommitMigration(ctx, revision, prepared); err != nil {
				return nil, err
			}
		}
		if err := migration.checkpoint(CheckpointConfigurationPersisted); err != nil {
			return nil, err
		}
		journal["config"] = MigrationCommitted
		if err := migration.Journal.Write(ctx, journal); err != nil {
			return nil, err
		}
		if err := migration.checkpoint(CheckpointConfigurationJournaled); err != nil {
			return nil, err
		}
	}

	if journalString(journal, "activation") != MigrationActive {
		journal["activation"] = MigrationActive
		if err := migration.Journal.Write(ctx, journal); err != nil {
			return nil, err
		}
		if err := migration.checkpoint(CheckpointMigrationActivated); err != nil {
			return nil, err
		}
	}
	return migrationResult(configPath, prepared, journal), nil
}

func prepareMigrationConfig(
	raw map[string]any,
	confirmRemoveLegacyMaterials bool,
) (map[string]any, *domaintemplates.LegacyMaterialError, error) {
	marketing, legacyError, err := domaintemplates.MigrateMarketing(raw, confirmRemoveLegacyMaterials)
	if err != nil || legacyError != nil {
		return nil, legacyError, err
	}
	product, _, _, err := domaintemplates.MigrateQianchuanProduct(marketing)
	if err != nil {
		return nil, nil, err
	}
	live, _, _, err := domaintemplates.MigrateQianchuanLive(product)
	if err != nil {
		return nil, nil, err
	}
	prepared, err := configuration.MigrateChannels(live)
	return prepared, nil, err
}

func (migration AuthorizationMigration) loadOrCreateJournal(ctx context.Context, configPath string) (map[string]any, error) {
	journal, exists, err := migration.Journal.Read(ctx)
	if err != nil {
		return nil, err
	}
	if exists {
		version, versionErr := configuration.Integer(journal["schema_version"])
		if versionErr != nil || version != MigrationJournalSchemaVersion {
			return nil, errors.New("unsupported migration journal schema")
		}
		journalPath, pathErr := journalRequiredString(journal, "config_path")
		if pathErr != nil {
			return nil, pathErr
		}
		if filepath.Clean(journalPath) == filepath.Clean(configPath) {
			return journal, nil
		}
		if journalString(journal, "activation") != MigrationActive {
			return nil, fmt.Errorf("another channel migration is incomplete for %s", journalPath)
		}
	}
	newID := migration.NewID
	if newID == nil {
		newID = secureMigrationID
	}
	migrationID, err := newID()
	if err != nil {
		return nil, err
	}
	authorizationID, err := newID()
	if err != nil {
		return nil, err
	}
	journal = map[string]any{
		"schema_version":   MigrationJournalSchemaVersion,
		"migration_id":     migrationID,
		"authorization_id": authorizationID,
		"config_path":      configPath,
		"config":           MigrationPending,
		"credentials":      MigrationPending,
		"authorization":    MigrationPending,
		"activation":       MigrationPending,
	}
	if err := migration.Journal.Write(ctx, journal); err != nil {
		return nil, err
	}
	return journal, nil
}

func (migration AuthorizationMigration) persistSplitCredentials(
	ctx context.Context,
	legacy map[string]any,
	authorizationID string,
	plan domain.LegacyAuthorizationPlan,
) error {
	if !configuration.Missing(legacy["app_id"]) && !configuration.Missing(legacy["secret"]) {
		account, err := domain.AppCredentialAccount("marketing")
		if err != nil {
			return err
		}
		if _, err := migration.Credentials.Write(ctx, account, map[string]any{
			"app_id": cloneMigrationValue(legacy["app_id"]),
			"secret": cloneMigrationValue(legacy["secret"]),
		}); err != nil {
			return fmt.Errorf("persist Marketing app credentials: %w", err)
		}
	}
	if plan.WriteAuthorizationSlot {
		account, err := domain.AuthorizationCredentialAccount("marketing", authorizationID, 1)
		if err != nil {
			return err
		}
		if _, err := migration.Credentials.Write(ctx, account, plan.Credential); err != nil {
			return fmt.Errorf("persist Marketing authorization credentials: %w", err)
		}
	}
	return nil
}

func (migration AuthorizationMigration) checkpoint(step MigrationCheckpoint) error {
	if migration.AfterCheckpoint == nil {
		return nil
	}
	return migration.AfterCheckpoint(step)
}

func migrationResult(configPath string, prepared map[string]any, journal map[string]any) map[string]any {
	credentialResult, _ := journal["credential_result"].(map[string]any)
	fields := migrationStringList(journal["config_sensitive_fields_migrated"])
	return map[string]any{
		"config":                           configPath,
		"migration_id":                     journal["migration_id"],
		"config_schema_version":            prepared["config_schema_version"],
		"default_channel":                  prepared["default_channel"],
		"credential_migration":             cloneMigrationValue(credentialResult),
		"config_sensitive_fields_migrated": fields,
		"activation":                       journal["activation"],
	}
}

func mergeMigrationCredentials(existing map[string]any, extracted map[string]any) map[string]any {
	result, _ := cloneMigrationValue(existing).(map[string]any)
	if result == nil {
		result = map[string]any{}
	}
	for key, value := range extracted {
		result[key] = cloneMigrationValue(value)
	}
	return result
}

func migrationSortedKeys(value map[string]any) []any {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]any, len(keys))
	for index, key := range keys {
		result[index] = key
	}
	return result
}

func migrationStringList(value any) []string {
	result := []string{}
	switch values := value.(type) {
	case []any:
		for _, item := range values {
			result = append(result, fmt.Sprint(item))
		}
	case []string:
		result = append(result, values...)
	}
	return result
}

func journalString(journal map[string]any, key string) string {
	return strings.TrimSpace(fmt.Sprint(journal[key]))
}

func journalRequiredString(journal map[string]any, key string) (string, error) {
	value := journalString(journal, key)
	if value == "" || value == "<nil>" {
		return "", fmt.Errorf("migration journal field is missing: %s", key)
	}
	return value, nil
}

func secureMigrationID() (string, error) {
	payload := make([]byte, 12)
	if _, err := rand.Read(payload); err != nil {
		return "", fmt.Errorf("generate migration ID: %w", err)
	}
	return hex.EncodeToString(payload), nil
}

func cloneMigrationValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = cloneMigrationValue(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = cloneMigrationValue(item)
		}
		return result
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}
