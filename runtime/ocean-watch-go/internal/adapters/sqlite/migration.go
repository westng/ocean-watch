package sqlite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrations embed.FS

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type migrationStep struct {
	version int
	payload []byte
}

type MigrationRecord struct {
	ID             string    `json:"id"`
	FromVersion    int       `json:"from_version"`
	ToVersion      int       `json:"to_version"`
	RuntimeVersion string    `json:"runtime_version"`
	Backup         string    `json:"backup_reference"`
	Result         string    `json:"result"`
	CompletedAt    time.Time `json:"completed_at"`
}

func identity(ctx context.Context, db queryer) (int, int, error) {
	var appID, version int
	if err := db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&appID); err != nil {
		return 0, 0, err
	}
	err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
	return appID, version, err
}

func migrationBytes() []byte {
	payload, err := migrations.ReadFile("migrations/001_operations.sql")
	if err != nil {
		panic(err)
	}
	return payload
}

func migrationSteps() ([]migrationStep, error) {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	steps := make([]migrationStep, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if len(name) < 5 || name[3] != '_' {
			return nil, ErrIncompatible
		}
		version, err := strconv.Atoi(name[:3])
		if err != nil || version != len(steps)+1 {
			return nil, ErrIncompatible
		}
		payload, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}
		steps = append(steps, migrationStep{version: version, payload: payload})
	}
	if len(steps) != SchemaVersion {
		return nil, ErrIncompatible
	}
	return steps, nil
}

func checksum(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func (store *Store) initialize(ctx context.Context) error {
	version, err := store.EngineVersion(ctx)
	if err != nil {
		return err
	}
	if !engineSupported(version) {
		return fmt.Errorf("%w: unsupported SQLite engine", ErrIncompatible)
	}
	appID, schema, err := identity(ctx, store.db)
	if err != nil {
		return err
	}
	if appID != 0 || schema != 0 {
		return store.validate(ctx)
	}
	var objects int
	if err := store.db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").Scan(&objects); err != nil {
		return err
	}
	if objects != 0 {
		return ErrIncompatible
	}
	var journal string
	if err := store.db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&journal); err != nil {
		return err
	}
	if !strings.EqualFold(journal, "wal") {
		return fmt.Errorf("%w: WAL unavailable", ErrIncompatible)
	}
	steps, err := migrationSteps()
	if err != nil {
		return err
	}
	if err := store.applyMigrations(ctx, steps, "initial_creation", "initial_creation"); err != nil {
		return err
	}
	return store.validate(ctx)
}

func Upgrade(ctx context.Context, options Options) (*Store, error) {
	if err := stateContext(ctx); err != nil {
		return nil, err
	}
	if err := validateOptions(&options); err != nil {
		return nil, err
	}
	store, err := openValidated(ctx, options, false, true)
	if err != nil {
		return nil, err
	}
	if err := store.Close(); err != nil {
		return nil, err
	}
	return Open(ctx, options)
}

func (store *Store) upgrade(ctx context.Context) error {
	appID, from, err := identity(ctx, store.db)
	if err != nil || appID != applicationID || from < 1 || from > SchemaVersion {
		return ErrIncompatible
	}
	if err := store.validateVersion(ctx, from); err != nil {
		return err
	}
	if from == SchemaVersion {
		return nil
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	id := "migration_" + hex.EncodeToString(nonce[:])
	backup := filepath.Join(filepath.Dir(store.path), "backups", id+".db")
	if _, err := store.backup(ctx, backup, from); err != nil {
		return err
	}
	steps, err := migrationSteps()
	if err != nil {
		return err
	}
	err = store.applyMigrations(ctx, steps, id, filepath.Base(backup))
	result := "succeeded"
	if err != nil {
		result = "failed_rolled_back"
	}
	record := MigrationRecord{ID: id, FromVersion: from, ToVersion: SchemaVersion, RuntimeVersion: store.runtimeVersion,
		Backup: filepath.Base(backup), Result: result, CompletedAt: time.Now().UTC()}
	recordErr := writePrivateJSON(backup+".migration.json", record)
	return errors.Join(err, recordErr)
}

func (store *Store) migrate(ctx context.Context, payload []byte) error {
	return store.applyMigrations(ctx, []migrationStep{{version: 1, payload: payload}}, "initial_creation", "initial_creation")
}

func (store *Store) applyMigrations(ctx context.Context, steps []migrationStep, id, backup string) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	appID, from, err := identity(ctx, tx)
	if err != nil {
		return err
	}
	if from < 0 || from > SchemaVersion || (from == 0 && appID != 0) || (from > 0 && appID != applicationID) {
		return ErrIncompatible
	}
	if from > 0 {
		if err := checkMigrationVersion(ctx, tx, from); err != nil {
			return err
		}
	} else {
		var objects int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").Scan(&objects); err != nil {
			return err
		}
		if objects != 0 {
			return ErrIncompatible
		}
	}
	if from == SchemaVersion {
		return nil
	}
	appliedAt := time.Now().UTC().Format(time.RFC3339Nano)
	to := from
	for _, step := range steps {
		if step.version <= from {
			continue
		}
		if step.version != to+1 {
			return ErrIncompatible
		}
		if _, err := tx.ExecContext(ctx, string(step.payload)); err != nil {
			return fmt.Errorf("apply state migration: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version, checksum, applied_at) VALUES (?, ?, ?)", step.version, checksum(step.payload), appliedAt); err != nil {
			return err
		}
		to = step.version
	}
	if to == SchemaVersion {
		if _, err := tx.ExecContext(ctx, "UPDATE schema_migrations SET source_runtime=?,backup_reference=? WHERE version>?", store.runtimeVersion, backup, from); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO migration_attempts(id,from_version,to_version,source_runtime,backup_reference,result,completed_at) VALUES(?,?,?,?,?,'succeeded',?)", id, from, to, store.runtimeVersion, backup, appliedAt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=%d", applicationID, to)); err != nil {
		return err
	}
	return tx.Commit()
}

func checkMigration(ctx context.Context, db queryer) error {
	return checkMigrationVersion(ctx, db, SchemaVersion)
}

func checkMigrationVersion(ctx context.Context, db queryer, version int) error {
	steps, err := migrationSteps()
	if err != nil || version < 1 || version > len(steps) {
		return ErrIncompatible
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != version {
		return fmt.Errorf("%w: unexpected migration history", ErrIncompatible)
	}
	for _, step := range steps[:version] {
		var actual string
		if err := db.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE version=?", step.version).Scan(&actual); err != nil || actual != checksum(step.payload) {
			return fmt.Errorf("%w: migration checksum mismatch", ErrIncompatible)
		}
	}
	return nil
}

func (store *Store) validate(ctx context.Context) error {
	return store.validateVersion(ctx, SchemaVersion)
}

func (store *Store) validateVersion(ctx context.Context, expected int) error {
	return store.validateVersionMode(ctx, expected, true)
}

func (store *Store) validateVersionWithoutJournal(ctx context.Context, expected int) error {
	return store.validateVersionMode(ctx, expected, false)
}

func (store *Store) validateVersionMode(ctx context.Context, expected int, requireWAL bool) error {
	version, err := store.EngineVersion(ctx)
	if err != nil {
		return err
	}
	if !engineSupported(version) {
		return ErrIncompatible
	}
	appID, schema, err := identity(ctx, store.db)
	if err != nil {
		return err
	}
	if appID != applicationID || schema != expected {
		return ErrIncompatible
	}
	if requireWAL {
		var journal string
		if err := store.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil {
			return err
		}
		if !strings.EqualFold(journal, "wal") {
			return fmt.Errorf("%w: WAL is required", ErrIncompatible)
		}
	}
	return checkMigrationVersion(ctx, store.db, expected)
}
