package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	SchemaVersion = 5
	applicationID = 0x4f574348
)

var (
	ErrNotInitialized = errors.New("state database is not initialized")
	ErrIncompatible   = errors.New("state database is incompatible")
	ErrUnsafePath     = errors.New("state database path is not private")
)

type Options struct {
	Path           string
	BusyTimeout    time.Duration
	MaxConnections int
	RuntimeVersion string
}

type Store struct {
	db             *sql.DB
	path           string
	runtimeVersion string
	release        func() error
	identity       *pathIdentity
}

func Open(ctx context.Context, options Options) (*Store, error) {
	return open(ctx, options, false)
}

func Initialize(ctx context.Context, options Options) (*Store, error) {
	return open(ctx, options, true)
}

func open(ctx context.Context, options Options, initialize bool) (*Store, error) {
	if err := stateContext(ctx); err != nil {
		return nil, err
	}
	if err := validateOptions(&options); err != nil {
		return nil, err
	}
	return openValidated(ctx, options, initialize, false)
}

func stateContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("state context is required")
	}
	return ctx.Err()
}

func validateOptions(options *Options) error {
	if !filepath.IsAbs(options.Path) || strings.TrimSpace(options.Path) == "" {
		return ErrUnsafePath
	}
	if options.BusyTimeout == 0 {
		options.BusyTimeout = 2 * time.Second
	}
	if options.BusyTimeout < time.Millisecond || options.BusyTimeout > 30*time.Second {
		return errors.New("state busy timeout must be between 1ms and 30s")
	}
	if options.MaxConnections == 0 {
		options.MaxConnections = 4
	}
	if options.MaxConnections < 1 || options.MaxConnections > 8 {
		return errors.New("state connection limit must be between 1 and 8")
	}
	if options.RuntimeVersion == "" {
		options.RuntimeVersion = "unknown"
	}
	if len(options.RuntimeVersion) > 128 || strings.ContainsAny(options.RuntimeVersion, "\r\n\x00") {
		return errors.New("state runtime version is invalid")
	}
	return nil
}

func openValidated(ctx context.Context, options Options, initialize, upgrading bool) (*Store, error) {
	path, err := preparePath(options.Path, initialize)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("mode", "rw")
	query.Add("_pragma", "busy_timeout("+strconv.FormatInt(options.BusyTimeout.Milliseconds(), 10)+")")
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "synchronous(FULL)")
	query.Add("_pragma", "trusted_schema(0)")
	query.Set("_txlock", "immediate")
	boundIdentity, err := captureIdentity(path)
	if err != nil {
		return nil, err
	}
	preexistingRecoverySidecar := false
	if info.Size() == 0 {
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			if _, sidecarErr := os.Lstat(path + suffix); sidecarErr == nil {
				preexistingRecoverySidecar = true
			} else if !errors.Is(sidecarErr, os.ErrNotExist) {
				return nil, sidecarErr
			}
		}
		if !initialize {
			if preexistingRecoverySidecar {
				return nil, ErrIncompatible
			}
			return nil, ErrNotInitialized
		}
	}
	lockPath := path + ".maintenance.lock"
	_, lockStatErr := os.Lstat(lockPath)
	lockExisted := lockStatErr == nil
	exclusive := upgrading || (initialize && info.Size() == 0)
	release, downgrade, err := acquireMaintenance(ctx, path, exclusive, options.BusyTimeout)
	if err != nil {
		return nil, err
	}
	current, err := os.Lstat(path)
	if err != nil {
		_ = release()
		return nil, err
	}
	if current.Size() == 0 {
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			if _, sidecarErr := os.Lstat(path + suffix); sidecarErr == nil {
				if !lockExisted && preexistingRecoverySidecar {
					_ = release()
					_ = os.Remove(lockPath)
				}
				return nil, ErrIncompatible
			} else if !errors.Is(sidecarErr, os.ErrNotExist) {
				_ = release()
				return nil, sidecarErr
			}
		}
		if !initialize {
			_ = release()
			return nil, ErrNotInitialized
		}
	}
	db, err := sql.Open("sqlite", databaseURI(filepath.ToSlash(path), query))
	if err != nil {
		_ = release()
		return nil, fmt.Errorf("open state database: %w", err)
	}
	db.SetMaxOpenConns(options.MaxConnections)
	db.SetMaxIdleConns(options.MaxConnections)
	store := &Store{db: db, path: path, runtimeVersion: options.RuntimeVersion, release: release, identity: boundIdentity}
	if err := store.ensureIdentity(); err != nil {
		_ = store.Close()
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("connect state database: %w", err)
	}
	if err = store.ensureIdentity(); err != nil {
		_ = store.Close()
		return nil, err
	}
	if upgrading {
		err = store.upgrade(ctx)
	} else if initialize {
		err = store.initialize(ctx)
	} else {
		err = store.validate(ctx)
	}
	if err == nil {
		err = store.ensureIdentity()
	}
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	if initialize && !upgrading && info.Size() == 0 {
		if err := downgrade(); err != nil {
			_ = store.Close()
			return nil, err
		}
	}
	return store, nil
}

func databaseURI(path string, query url.Values) string {
	if len(path) >= 3 && path[1] == ':' && path[2] == '/' {
		path = "/" + path
	}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}
	return uri.String()
}

func (store *Store) Close() error {
	err := store.db.Close()
	if store.release != nil {
		err = errors.Join(err, store.release())
	}
	return err
}

func (store *Store) EngineVersion(ctx context.Context) (string, error) {
	var version string
	err := store.db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version)
	return version, err
}

func engineSupported(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	values := [3]int{}
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return false
		}
		values[i] = value
	}
	minimum := [3]int{3, 51, 3}
	for i, value := range values {
		if value != minimum[i] {
			return value > minimum[i]
		}
	}
	return true
}
