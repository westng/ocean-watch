package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/operations"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
)

// SQLiteJournalStore persists the legacy JournalStore contract in the
// operation snapshot tables. It is the migration bridge for callers that
// still address preflight state by a human-readable run ID while the runtime
// state model moves to SQLite.
type SQLiteJournalStore struct {
	Path           string
	RuntimeVersion string
	BusyTimeout    time.Duration
	MaxConnections int
	Legacy         sharedplans.JournalStore
}

var _ sharedplans.JournalStore = SQLiteJournalStore{}

func (store SQLiteJournalStore) options() Options {
	runtimeVersion := strings.TrimSpace(store.RuntimeVersion)
	if runtimeVersion == "" {
		runtimeVersion = "unknown"
	}
	return Options{
		Path:           filepath.Clean(store.Path),
		RuntimeVersion: runtimeVersion,
		BusyTimeout:    store.BusyTimeout,
		MaxConnections: store.MaxConnections,
	}
}

func (store SQLiteJournalStore) Load(ctx context.Context, runID string) (domainplans.Journal, error) {
	operationID, err := journalOperationID(runID)
	if err != nil {
		return domainplans.Journal{}, err
	}
	opened, err := Open(ctx, store.options())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, ErrNotInitialized) {
			return store.loadLegacy(ctx, runID)
		}
		return domainplans.Journal{}, err
	}
	defer opened.Close()
	var version int
	if err := opened.db.QueryRowContext(ctx, "SELECT current_version FROM operations WHERE id=?", operationID).Scan(&version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.loadLegacy(ctx, runID)
		}
		return domainplans.Journal{}, err
	}
	snapshot, err := opened.GetSnapshot(ctx, operationID, version)
	if err != nil {
		return domainplans.Journal{}, err
	}
	return decodeJournal(snapshot.Payload)
}

func (store SQLiteJournalStore) loadLegacy(ctx context.Context, runID string) (domainplans.Journal, error) {
	if store.Legacy != nil {
		return store.Legacy.Load(ctx, runID)
	}
	return domainplans.Journal{}, ErrJournalNotFound
}

func (store SQLiteJournalStore) Save(ctx context.Context, runID string, journal domainplans.Journal) error {
	operationID, err := journalOperationID(runID)
	if err != nil {
		return err
	}
	if _, err := domainplans.NewJournal(journal.Fingerprint, journal.Jobs, parseJournalTime(journal.CreatedAt)); err != nil {
		return err
	}
	payload, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	createdAt := parseJournalTime(journal.CreatedAt)
	if createdAt.IsZero() {
		return errors.New("journal created_at is invalid")
	}
	expiresAt := journalExpiry(journal, createdAt)
	digest, err := operations.Digest(payload)
	if err != nil {
		return fmt.Errorf("validate journal snapshot: %w", err)
	}
	subjectID := journalSubject(journal)
	snapshot := operations.Snapshot{
		OperationID: operationID, Kind: journalKind(runID, journal), Channel: journalChannel(runID), SubjectID: subjectID,
		RuntimeVersion: store.options().RuntimeVersion, SchemaVersion: operations.SchemaVersion,
		InputDigest: digest, SnapshotDigest: digest, CreatedAt: createdAt, ExpiresAt: expiresAt, Payload: payload,
	}
	opened, err := Initialize(ctx, store.options())
	if err != nil {
		return err
	}
	defer opened.Close()
	var current int
	err = opened.db.QueryRowContext(ctx, "SELECT current_version FROM operations WHERE id=?", operationID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		snapshot.Version = 1
	} else if err != nil {
		return err
	} else {
		snapshot.Version = current
		previous, loadErr := opened.GetSnapshot(ctx, operationID, current)
		if loadErr != nil {
			return loadErr
		}
		if string(previous.Payload) == string(payload) {
			return nil
		}
		snapshot.Version++
	}
	if err := snapshot.Validate(); err != nil {
		return fmt.Errorf("validate journal snapshot: %w", err)
	}
	return opened.SavePreflight(ctx, snapshot)
}

var ErrJournalNotFound = errors.New("operation journal was not found")

func journalOperationID(runID string) (string, error) {
	if err := domainplans.ValidateJournalID(runID); err != nil {
		return "", err
	}
	return "op_" + strings.TrimSpace(runID), nil
}

func journalChannel(runID string) string {
	if strings.HasPrefix(runID, "qianchuan-preflight-") {
		return string(domainplans.ChannelQianchuan)
	}
	return string(domainplans.ChannelMarketing)
}

func journalKind(runID string, journal domainplans.Journal) string {
	var kind string
	if raw := journal.Extra["kind"]; len(raw) != 0 {
		_ = json.Unmarshal(raw, &kind)
	}
	if strings.TrimSpace(kind) != "" {
		return kind
	}
	if strings.HasPrefix(runID, "qianchuan-preflight-") {
		return "qianchuan_preflight"
	}
	return "operation_journal"
}

func journalSubject(journal domainplans.Journal) string {
	for _, job := range journal.Jobs {
		if subject := strings.TrimSpace(job.AdvertiserID); subject != "" {
			return subject
		}
	}
	return "local"
}

func journalExpiry(journal domainplans.Journal, createdAt time.Time) time.Time {
	var header struct {
		ExpiresAt string `json:"expires_at"`
	}
	if raw := journal.Extra["snapshot"]; len(raw) != 0 && json.Unmarshal(raw, &header) == nil {
		if parsed, err := time.Parse(time.RFC3339Nano, header.ExpiresAt); err == nil && parsed.After(createdAt) {
			return parsed.UTC()
		}
	}
	return createdAt.UTC().Add(24 * time.Hour)
}

func parseJournalTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	return parsed.UTC()
}

func decodeJournal(payload []byte) (domainplans.Journal, error) {
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	var journal domainplans.Journal
	if err := decoder.Decode(&journal); err != nil {
		return domainplans.Journal{}, fmt.Errorf("decode operation journal: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return domainplans.Journal{}, errors.New("operation journal contains trailing JSON")
	} else if !errors.Is(err, io.EOF) {
		return domainplans.Journal{}, fmt.Errorf("decode operation journal: %w", err)
	}
	if _, err := domainplans.NewJournal(journal.Fingerprint, journal.Jobs, parseJournalTime(journal.CreatedAt)); err != nil {
		return domainplans.Journal{}, err
	}
	return journal, nil
}
