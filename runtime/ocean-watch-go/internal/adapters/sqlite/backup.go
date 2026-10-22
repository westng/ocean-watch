package sqlite

import (
	"context"
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
	destination, err := newPrivateFile(options.Path)
	if err != nil {
		return nil, err
	}
	if err := copyDatabase(ctx, source, destination); err != nil {
		return nil, err
	}
	if err := enableWAL(ctx, destination); err != nil {
		return nil, err
	}
	var store *Store
	if record.SchemaVersion < SchemaVersion {
		store, err = Upgrade(ctx, options)
	} else {
		store, err = Open(ctx, options)
	}
	if err != nil {
		return nil, err
	}
	if err := checkIntegrity(ctx, store.db); err != nil {
		_ = store.Close()
		return nil, err
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
	if err != nil {
		return err
	}
	_, writeErr := file.Write(payload)
	return errors.Join(writeErr, file.Sync(), file.Close())
}

func readBackupRecord(path string) (BackupRecord, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || !private(path, info) || info.Size() > 4096 {
		return BackupRecord{}, ErrIncompatible
	}
	file, err := os.Open(path)
	if err != nil {
		return BackupRecord{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 4097))
	decoder.DisallowUnknownFields()
	var record BackupRecord
	if err := decoder.Decode(&record); err != nil {
		return BackupRecord{}, ErrIncompatible
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return BackupRecord{}, ErrIncompatible
	}
	return record, nil
}
