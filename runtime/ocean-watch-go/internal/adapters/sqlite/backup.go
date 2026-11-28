package sqlite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	driver "modernc.org/sqlite"
)

type BackupRecord struct {
	FormatVersion  int       `json:"format_version"`
	SchemaVersion  int       `json:"schema_version"`
	RuntimeVersion string    `json:"runtime_version"`
	EngineVersion  string    `json:"engine_version"`
	Digest         string    `json:"digest"`
	CreatedAt      time.Time `json:"created_at"`
}

func (store *Store) Backup(ctx context.Context, destination string) (BackupRecord, error) {
	return store.backup(ctx, destination, SchemaVersion)
}

func (store *Store) backup(ctx context.Context, destination string, schema int) (BackupRecord, error) {
	if err := stateContext(ctx); err != nil {
		return BackupRecord{}, err
	}
	if err := store.validateVersion(ctx, schema); err != nil {
		return BackupRecord{}, err
	}
	path, err := newPrivateFile(destination)
	if err != nil {
		return BackupRecord{}, err
	}
	if err := copyDatabase(ctx, store.db, path); err != nil {
		return BackupRecord{}, err
	}
	if _, err := preparePath(path, false); err != nil {
		return BackupRecord{}, err
	}
	copyDB, err := readOnlyDatabase(ctx, path)
	if err != nil {
		return BackupRecord{}, err
	}
	copyStore := &Store{db: copyDB, path: path, runtimeVersion: store.runtimeVersion}
	if copyStore.runtimeVersion == "" {
		copyStore.runtimeVersion = "unknown"
	}
	validationErr := copyStore.validateVersionWithoutJournal(ctx, schema)
	if validationErr == nil {
		validationErr = checkIntegrity(ctx, copyDB)
	}
	engine, engineErr := copyStore.EngineVersion(ctx)
	closeErr := copyDB.Close()
	if err := errors.Join(validationErr, engineErr, closeErr); err != nil {
		return BackupRecord{}, err
	}
	digest, err := privateFileDigest(path)
	if err != nil {
		return BackupRecord{}, err
	}
	record := BackupRecord{FormatVersion: 1, SchemaVersion: schema, RuntimeVersion: copyStore.runtimeVersion,
		EngineVersion: engine, Digest: digest, CreatedAt: time.Now().UTC()}
	if err := writePrivateJSON(path+".manifest.json", record); err != nil {
		return BackupRecord{}, err
	}
	return record, nil
}

func Restore(ctx context.Context, backupPath string, options Options) (*Store, error) {
	if err := stateContext(ctx); err != nil {
		return nil, err
	}
	if err := validateOptions(&options); err != nil {
		return nil, err
	}
	path, err := preparePath(backupPath, false)
	if err != nil {
		return nil, err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(path + suffix); err == nil {
			return nil, ErrIncompatible
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	record, err := readBackupRecord(path + ".manifest.json")
	if err != nil {
		return nil, err
	}
	if record.FormatVersion != 1 || (record.SchemaVersion < 1 || record.SchemaVersion > SchemaVersion) || record.CreatedAt.IsZero() || !engineSupported(record.EngineVersion) || record.RuntimeVersion == "" {
		return nil, ErrIncompatible
	}
	digest, err := privateFileDigest(path)
	if err != nil || digest != record.Digest {
		return nil, ErrIncompatible
	}
	source, err := readOnlyDatabase(ctx, path)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	if err := (&Store{db: source}).validateVersionWithoutJournal(ctx, record.SchemaVersion); err != nil {
		return nil, err
	}
	if err := checkIntegrity(ctx, source); err != nil {
		return nil, err
	}
	destination := filepath.Clean(options.Path)
	if _, err := os.Lstat(destination); err == nil {
		return nil, os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	// Finish copying, migrating, and checking integrity before the requested
	// name becomes visible. A crash before publication cannot occupy it.
	stage, err := newPrivateFile(filepath.Join(filepath.Dir(destination), ".restore-"+rand.Text()+".db"))
	if err != nil {
		return nil, err
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal", ".maintenance.lock"} {
		defer os.Remove(stage + suffix)
	}
	if err := copyDatabase(ctx, source, stage); err != nil {
		return nil, err
	}
	if err := enableWAL(ctx, stage); err != nil {
		return nil, err
	}
	stageOptions := options
	stageOptions.Path = stage
	var store *Store
	if record.SchemaVersion < SchemaVersion {
		store, err = Upgrade(ctx, stageOptions)
	} else {
		store, err = Open(ctx, stageOptions)
	}
	if err != nil {
		return nil, err
	}
	if err := errors.Join(checkIntegrity(ctx, store.db), store.Close()); err != nil {
		return nil, err
	}
	return publishRestoredDatabase(ctx, stage, filepath.Join(filepath.Dir(stage), filepath.Base(destination)), options)
}

func publishRestoredDatabase(ctx context.Context, stage, destination string, options Options) (*Store, error) {
	identity, err := captureIdentity(stage)
	if err != nil {
		return nil, err
	}
	release, downgrade, err := acquireMaintenance(ctx, destination, true, options.BusyTimeout)
	if err != nil {
		return nil, err
	}
	transferred := false
	defer func() {
		if !transferred {
			_ = release()
		}
	}()
	if _, err := os.Lstat(destination); err == nil {
		return nil, os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	// Closing the staged store must have checkpointed its WAL. Never pair a
	// newly published main file with an unrelated destination recovery file.
	for _, path := range []string{stage, destination} {
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			if _, err := os.Lstat(path + suffix); err == nil {
				return nil, ErrIncompatible
			} else if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Link is an atomic, no-replace publication, including against concurrent
	// Initialize calls that create the main file before acquiring maintenance.
	if err := os.Link(stage, destination); err != nil {
		return nil, err
	}
	store, err := openRestoredDatabase(ctx, destination, options, identity)
	if err == nil {
		err = downgrade()
		if err == nil {
			store.release = release
			transferred = true
			return store, nil
		}
		err = errors.Join(err, store.Close())
	}
	// No consumer can open our new file until the exclusive lock is released.
	// Check its identity before removing our own publication on a failed open.
	info, statErr := os.Lstat(destination)
	if statErr == nil && os.SameFile(info, identity.file) {
		return nil, errors.Join(err, os.Remove(destination))
	}
	return nil, errors.Join(err, statErr, ErrUnsafePath)
}

// Open the published name while Restore still owns its exclusive maintenance
// lock. All connections need the same write and durability settings as Open.
func openRestoredDatabase(ctx context.Context, path string, options Options, identity *pathIdentity) (*Store, error) {
	query := url.Values{"mode": {"rw"}, "_txlock": {"immediate"}, "_pragma": {
		fmt.Sprintf("busy_timeout(%d)", options.BusyTimeout.Milliseconds()), "foreign_keys(1)", "synchronous(FULL)", "trusted_schema(0)",
	}}
	db, err := sql.Open("sqlite", databaseURI(filepath.ToSlash(path), query))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(options.MaxConnections)
	db.SetMaxIdleConns(options.MaxConnections)
	store := &Store{db: db, path: path, runtimeVersion: options.RuntimeVersion, identity: identity}
	if err := store.ensureIdentity(); err != nil {
		return nil, errors.Join(err, store.Close())
	}
	if err := store.validate(ctx); err != nil {
		return nil, errors.Join(err, store.Close())
	}
	if err := store.ensureIdentity(); err != nil {
		return nil, errors.Join(err, store.Close())
	}
	return store, nil
}

func copyDatabase(ctx context.Context, source *sql.DB, destination string) error {
	connection, err := source.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	return connection.Raw(func(raw any) error {
		backuper, ok := raw.(interface {
			NewBackup(string) (*driver.Backup, error)
		})
		if !ok {
			return ErrIncompatible
		}
		backup, err := backuper.NewBackup(databaseURI(filepath.ToSlash(destination), url.Values{"mode": {"rw"}}))
		if err != nil {
			return err
		}
		for {
			if err := ctx.Err(); err != nil {
				return errors.Join(err, backup.Finish())
			}
			more, err := backup.Step(128)
			if err != nil {
				return errors.Join(err, backup.Finish())
			}
			if !more {
				return backup.Finish()
			}
		}
	})
}

func checkIntegrity(ctx context.Context, db *sql.DB) error {
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return ErrIncompatible
	}
	if result != "ok" {
		return ErrIncompatible
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return ErrIncompatible
	}
	return rows.Err()
}

func enableWAL(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", databaseURI(filepath.ToSlash(path), url.Values{"mode": {"rw"}, "_pragma": {"busy_timeout(2000)", "synchronous(FULL)", "trusted_schema(0)"}}))
	if err != nil {
		return err
	}
	defer db.Close()
	var journal string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&journal); err != nil {
		return err
	}
	if !strings.EqualFold(journal, "wal") {
		return fmt.Errorf("%w: WAL unavailable", ErrIncompatible)
	}
	return db.Close()
}

func readOnlyDatabase(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", databaseURI(filepath.ToSlash(path), url.Values{"mode": {"ro"}, "immutable": {"1"}, "_pragma": {"query_only(1)", "trusted_schema(0)"}}))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func newPrivateFile(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", ErrUnsafePath
	}
	path = filepath.Clean(path)
	parent := filepath.Dir(path)
	if err := checkDirectoryChain(parent); err != nil {
		return "", err
	}
	if err := createStateDirectory(parent); err != nil {
		return "", err
	}
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !privateDirectory(parent, info) {
		return "", ErrUnsafePath
	}
	if err := createStateFile(path); err != nil {
		return "", err
	}
	return preparePath(path, false)
}

func privateFileDigest(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || !private(path, info) {
		return "", ErrUnsafePath
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", ErrUnsafePath
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func writePrivateJSON(path string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	path, err = newPrivateFile(path)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		_, writeErr := file.Write(payload)
		err = errors.Join(writeErr, file.Sync(), file.Close())
	}
	if err != nil {
		return errors.Join(err, os.Remove(path))
	}
	return nil
}

func readBackupRecord(path string) (BackupRecord, error) {
	var record BackupRecord
	err := readPrivateJSON(path, &record)
	return record, err
}

func readPrivateJSON(path string, value any) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || !private(path, info) || info.Size() > 4096 {
		return ErrIncompatible
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return ErrIncompatible
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return ErrIncompatible
	}
	return nil
}
