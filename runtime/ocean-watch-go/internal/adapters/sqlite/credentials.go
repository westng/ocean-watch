package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/authorization"
	credentialport "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/credentials"
)

var _ credentialport.Rotations = (*Store)(nil)

type credentialQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (store *Store) LoadCredentialState(ctx context.Context, key authorization.Key) (authorization.CredentialState, error) {
	if key.Validate() != nil {
		return authorization.CredentialState{}, credentialport.ErrInvalidState
	}
	var result authorization.CredentialState
	err := store.credentialTransaction(ctx, func(tx *sql.Tx) error {
		var err error
		result, err = readCredentialState(ctx, tx, key)
		return err
	})
	if err != nil {
		return authorization.CredentialState{}, err
	}
	return result, nil
}

func (store *Store) SeedCredentialVersion(ctx context.Context, version authorization.CredentialVersion) error {
	return store.RegisterCredentialVersion(ctx, version)
}

func (store *Store) RegisterCredentialVersion(ctx context.Context, version authorization.CredentialVersion) error {
	if version.Validate() != nil {
		return credentialport.ErrInvalidState
	}
	return store.credentialTransaction(ctx, func(tx *sql.Tx) error {
		previous, err := readCredentialState(ctx, tx, version.Key)
		if err == nil {
			if previous.Credential.Equal(version) && previous.Status == authorization.Active && previous.Rotation == nil {
				return nil
			}
			return credentialport.ErrConflict
		}
		if !errors.Is(err, credentialport.ErrNotFound) {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO credential_authorizations
			(channel,authorization_id,credential_ref,token_revision,state,updated_at) VALUES(?,?,?,?,'active',?)`,
			version.Key.Channel, version.Key.AuthorizationID, version.Reference, version.Revision, credentialTimestamp(version.IssuedAt))
		if err != nil {
			return credentialport.ErrUnavailable
		}
		return insertCredentialVersion(ctx, tx, version, "active")
	})
}

func (store *Store) BeginCredentialRotation(ctx context.Context, rotation authorization.Rotation) error {
	if rotation.Validate() != nil || rotation.Status != authorization.RotationIntent {
		return credentialport.ErrInvalidState
	}
	return store.credentialTransaction(ctx, func(tx *sql.Tx) error {
		current, err := readCredentialState(ctx, tx, rotation.Key)
		if err != nil {
			return err
		}
		if current.Status != authorization.Active {
			return credentialport.ErrReauthorizationRequired
		}
		if current.Rotation != nil || current.Credential.Revision != rotation.OldRevision ||
			current.Credential.Reference != rotation.OldReference || rotation.StartedAt.Before(current.Credential.IssuedAt) {
			return credentialport.ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO credential_rotations
			(channel,authorization_id,old_revision,new_revision,old_ref,candidate_ref,state,started_at)
			VALUES(?,?,?,?,?,?,'intent',?)`, rotation.Key.Channel, rotation.Key.AuthorizationID,
			rotation.OldRevision, rotation.NewRevision, rotation.OldReference, rotation.CandidateReference, credentialTimestamp(rotation.StartedAt))
		if err != nil {
			return credentialport.ErrUnavailable
		}
		return nil
	})
}

func (store *Store) RecordCredentialCandidate(ctx context.Context, key authorization.Key, revision int64, candidate authorization.CredentialVersion) error {
	if key.Validate() != nil || candidate.Validate() != nil || candidate.Key != key || candidate.Revision != revision {
		return credentialport.ErrInvalidState
	}
	return store.credentialTransaction(ctx, func(tx *sql.Tx) error {
		current, err := readCredentialState(ctx, tx, key)
		if err != nil {
			return err
		}
		rotation := current.Rotation
		if current.Status != authorization.Active || rotation == nil || rotation.NewRevision != revision || !rotation.Accepts(candidate) {
			return credentialport.ErrConflict
		}
		if rotation.Status == authorization.RotationCandidateStored && rotation.Candidate.Equal(candidate) {
			return nil
		}
		if rotation.Status != authorization.RotationIntent {
			return credentialport.ErrConflict
		}
		if err := insertCredentialVersion(ctx, tx, candidate, "candidate"); err != nil {
			return err
		}
		return credentialExecOne(ctx, tx, `UPDATE credential_rotations SET candidate_digest=?,candidate_issued_at=?,
			candidate_access_expires_at=?,candidate_refresh_expires_at=?,state='candidate_stored'
			WHERE channel=? AND authorization_id=? AND new_revision=? AND state='intent'`,
			candidate.Digest, credentialTimestamp(candidate.IssuedAt), credentialTimestamp(candidate.AccessExpiresAt),
			credentialTimestamp(candidate.RefreshExpiresAt), key.Channel, key.AuthorizationID, revision)
	})
}

func (store *Store) ActivateCredentialCandidate(ctx context.Context, key authorization.Key, revision int64, at time.Time) error {
	if key.Validate() != nil || revision < 1 || !validCredentialTimestamp(at) {
		return credentialport.ErrInvalidState
	}
	return store.credentialTransaction(ctx, func(tx *sql.Tx) error {
		current, err := readCredentialState(ctx, tx, key)
		if err != nil {
			return err
		}
		rotation := current.Rotation
		if current.Status != authorization.Active || rotation == nil || rotation.NewRevision != revision {
			return credentialport.ErrConflict
		}
		if rotation.Status == authorization.RotationActivated {
			return nil
		}
		if rotation.Status != authorization.RotationCandidateStored || rotation.Candidate == nil ||
			at.Before(rotation.Candidate.IssuedAt) || !rotation.Candidate.RefreshExpiresAt.After(at) {
			return credentialport.ErrConflict
		}
		version, err := readCredentialVersion(ctx, tx, key, revision, rotation.CandidateReference, "candidate")
		if err != nil || !version.Equal(*rotation.Candidate) {
			return credentialport.ErrInvalidState
		}
		if err := credentialExecOne(ctx, tx, `UPDATE credential_authorizations SET credential_ref=?,token_revision=?,updated_at=?
			WHERE channel=? AND authorization_id=? AND credential_ref=? AND token_revision=? AND state='active'`,
			version.Reference, revision, credentialTimestamp(at), key.Channel, key.AuthorizationID, rotation.OldReference, rotation.OldRevision); err != nil {
			return err
		}
		if err := credentialExecOne(ctx, tx, `UPDATE credential_versions SET state='retired'
			WHERE channel=? AND authorization_id=? AND revision=? AND state='active'`, key.Channel, key.AuthorizationID, rotation.OldRevision); err != nil {
			return err
		}
		if err := credentialExecOne(ctx, tx, `UPDATE credential_versions SET state='active'
			WHERE channel=? AND authorization_id=? AND revision=? AND state='candidate'`, key.Channel, key.AuthorizationID, revision); err != nil {
			return err
		}
		return credentialExecOne(ctx, tx, `UPDATE credential_rotations SET state='activated',activated_at=?
			WHERE channel=? AND authorization_id=? AND new_revision=? AND state='candidate_stored'`,
			credentialTimestamp(at), key.Channel, key.AuthorizationID, revision)
	})
}

func (store *Store) CompleteCredentialRotation(ctx context.Context, key authorization.Key, revision int64, at time.Time) error {
	if key.Validate() != nil || revision < 1 || !validCredentialTimestamp(at) {
		return credentialport.ErrInvalidState
	}
	return store.credentialTransaction(ctx, func(tx *sql.Tx) error {
		rotation, err := readCredentialRotation(ctx, tx, key, revision)
		if err != nil {
			return err
		}
		if rotation.Status == authorization.RotationCompleted {
			return nil
		}
		current, err := readCredentialState(ctx, tx, key)
		if err != nil {
			return err
		}
		if current.Status != authorization.Active || rotation.Status != authorization.RotationActivated ||
			rotation.Candidate == nil || !current.Credential.Equal(*rotation.Candidate) || at.Before(rotation.ActivatedAt) {
			return credentialport.ErrConflict
		}
		return credentialExecOne(ctx, tx, `UPDATE credential_rotations SET state='completed',completed_at=?
			WHERE channel=? AND authorization_id=? AND new_revision=? AND state='activated'`,
			credentialTimestamp(at), key.Channel, key.AuthorizationID, revision)
	})
}

func (store *Store) RequireCredentialReauthorization(ctx context.Context, key authorization.Key, revision int64, code string, at time.Time) error {
	if key.Validate() != nil || revision < 1 || !authorization.ValidFailureCode(code) || !validCredentialTimestamp(at) {
		return credentialport.ErrInvalidState
	}
	return store.credentialTransaction(ctx, func(tx *sql.Tx) error {
		rotation, err := readCredentialRotation(ctx, tx, key, revision)
		if errors.Is(err, credentialport.ErrNotFound) {
			return credentialport.ErrConflict
		}
		if err != nil {
			return err
		}
		if rotation.Status == authorization.RotationReauthorizationRequired {
			return nil
		}
		if rotation.Status == authorization.RotationCompleted || at.Before(rotation.StartedAt) {
			return credentialport.ErrConflict
		}
		if err := credentialExecOne(ctx, tx, `UPDATE credential_rotations SET state='reauthorization_required',failure_code=?
			WHERE channel=? AND authorization_id=? AND new_revision=? AND state IN ('intent','candidate_stored','activated')`,
			code, key.Channel, key.AuthorizationID, revision); err != nil {
			return err
		}
		return credentialExecOne(ctx, tx, `UPDATE credential_authorizations SET state='reauthorization_required',updated_at=?
			WHERE channel=? AND authorization_id=? AND state='active'`, credentialTimestamp(at), key.Channel, key.AuthorizationID)
	})
}

func (store *Store) credentialTransaction(ctx context.Context, action func(*sql.Tx) error) error {
	if ctx == nil || store == nil || store.db == nil {
		return credentialport.ErrInvalidState
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := store.ensureIdentity(); err != nil {
		return ErrUnsafePath
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return credentialport.ErrUnavailable
	}
	defer tx.Rollback()
	if err := action(tx); err != nil {
		return err
	}
	if err := store.ensureIdentity(); err != nil {
		return ErrUnsafePath
	}
	if err := tx.Commit(); err != nil {
		return credentialport.ErrUnavailable
	}
	return nil
}

func readCredentialState(ctx context.Context, db credentialQueryer, key authorization.Key) (authorization.CredentialState, error) {
	var reference string
	var revision int64
	var status authorization.Status
	err := db.QueryRowContext(ctx, `SELECT credential_ref,token_revision,state FROM credential_authorizations
		WHERE channel=? AND authorization_id=?`, key.Channel, key.AuthorizationID).Scan(&reference, &revision, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return authorization.CredentialState{}, credentialport.ErrNotFound
	}
	if err != nil {
		return authorization.CredentialState{}, credentialport.ErrUnavailable
	}
	version, err := readCredentialVersion(ctx, db, key, revision, reference, "active")
	if err != nil {
		return authorization.CredentialState{}, credentialport.ErrInvalidState
	}
	result := authorization.CredentialState{Credential: version, Status: status}
	rotation, err := readCredentialRotation(ctx, db, key, 0)
	if err == nil {
		result.Rotation = &rotation
	} else if !errors.Is(err, credentialport.ErrNotFound) {
		return authorization.CredentialState{}, err
	}
	if result.Validate() != nil {
		return authorization.CredentialState{}, credentialport.ErrInvalidState
	}
	return result, nil
}

func insertCredentialVersion(ctx context.Context, tx *sql.Tx, version authorization.CredentialVersion, state string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO credential_versions
		(channel,authorization_id,revision,credential_ref,credential_digest,issued_at,access_expires_at,refresh_expires_at,state)
		VALUES(?,?,?,?,?,?,?,?,?)`, version.Key.Channel, version.Key.AuthorizationID, version.Revision, version.Reference,
		version.Digest, credentialTimestamp(version.IssuedAt), credentialTimestamp(version.AccessExpiresAt), credentialTimestamp(version.RefreshExpiresAt), state)
	if err != nil {
		return credentialport.ErrUnavailable
	}
	return nil
}

func readCredentialVersion(ctx context.Context, db credentialQueryer, key authorization.Key, revision int64, reference, state string) (authorization.CredentialVersion, error) {
	var digest, issued, accessExpiry, refreshExpiry string
	err := db.QueryRowContext(ctx, `SELECT credential_digest,issued_at,access_expires_at,refresh_expires_at FROM credential_versions
		WHERE channel=? AND authorization_id=? AND revision=? AND credential_ref=? AND state=?`,
		key.Channel, key.AuthorizationID, revision, reference, state).Scan(&digest, &issued, &accessExpiry, &refreshExpiry)
	if errors.Is(err, sql.ErrNoRows) {
		return authorization.CredentialVersion{}, credentialport.ErrNotFound
	}
	if err != nil {
		return authorization.CredentialVersion{}, credentialport.ErrUnavailable
	}
	return parseCredentialVersion(key, revision, reference, digest, issued, accessExpiry, refreshExpiry)
}

func readCredentialRotation(ctx context.Context, db credentialQueryer, key authorization.Key, revision int64) (authorization.Rotation, error) {
	var rotation authorization.Rotation
	rotation.Key = key
	var started string
	var digest, issued, accessExpiry, refreshExpiry, activated, completed, failure sql.NullString
	query := `SELECT old_revision,new_revision,old_ref,candidate_ref,state,started_at,candidate_digest,
		candidate_issued_at,candidate_access_expires_at,candidate_refresh_expires_at,activated_at,completed_at,failure_code
		FROM credential_rotations WHERE channel=? AND authorization_id=?`
	args := []any{key.Channel, key.AuthorizationID}
	if revision == 0 {
		query += " AND state!='completed' ORDER BY new_revision DESC LIMIT 1"
	} else {
		query += " AND new_revision=?"
		args = append(args, revision)
	}
	err := db.QueryRowContext(ctx, query, args...).Scan(&rotation.OldRevision, &rotation.NewRevision,
		&rotation.OldReference, &rotation.CandidateReference, &rotation.Status, &started, &digest,
		&issued, &accessExpiry, &refreshExpiry, &activated, &completed, &failure)
	if errors.Is(err, sql.ErrNoRows) {
		return authorization.Rotation{}, credentialport.ErrNotFound
	}
	if err != nil {
		return authorization.Rotation{}, credentialport.ErrUnavailable
	}
	rotation.StartedAt, err = time.Parse(time.RFC3339Nano, started)
	if err != nil {
		return authorization.Rotation{}, credentialport.ErrInvalidState
	}
	if activated.Valid {
		rotation.ActivatedAt, err = time.Parse(time.RFC3339Nano, activated.String)
		if err != nil {
			return authorization.Rotation{}, credentialport.ErrInvalidState
		}
	}
	if completed.Valid {
		rotation.CompletedAt, err = time.Parse(time.RFC3339Nano, completed.String)
		if err != nil {
			return authorization.Rotation{}, credentialport.ErrInvalidState
		}
	}
	if digest.Valid {
		candidate, err := parseCredentialVersion(key, rotation.NewRevision, rotation.CandidateReference, digest.String,
			issued.String, accessExpiry.String, refreshExpiry.String)
		if err != nil {
			return authorization.Rotation{}, err
		}
		rotation.Candidate = &candidate
	} else if issued.Valid || accessExpiry.Valid || refreshExpiry.Valid {
		return authorization.Rotation{}, credentialport.ErrInvalidState
	}
	rotation.FailureCode = failure.String
	if rotation.Validate() != nil {
		return authorization.Rotation{}, credentialport.ErrInvalidState
	}
	return rotation, nil
}

func parseCredentialVersion(key authorization.Key, revision int64, reference, digest, issued, accessExpiry, refreshExpiry string) (authorization.CredentialVersion, error) {
	issuedAt, issuedErr := time.Parse(time.RFC3339Nano, issued)
	access, accessErr := time.Parse(time.RFC3339Nano, accessExpiry)
	refresh, refreshErr := time.Parse(time.RFC3339Nano, refreshExpiry)
	if issuedErr != nil || accessErr != nil || refreshErr != nil {
		return authorization.CredentialVersion{}, credentialport.ErrInvalidState
	}
	version := authorization.CredentialVersion{Key: key, Revision: revision, Reference: reference, Digest: digest,
		IssuedAt: issuedAt, AccessExpiresAt: access, RefreshExpiresAt: refresh}
	if version.Validate() != nil {
		return authorization.CredentialVersion{}, credentialport.ErrInvalidState
	}
	return version, nil
}

func credentialExecOne(ctx context.Context, tx *sql.Tx, query string, args ...any) error {
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return credentialport.ErrUnavailable
	}
	count, err := result.RowsAffected()
	if err != nil {
		return credentialport.ErrUnavailable
	}
	if count != 1 {
		return credentialport.ErrConflict
	}
	return nil
}

func formatTime(value time.Time) string { return credentialTimestamp(value) }

func credentialTimestamp(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func validCredentialTimestamp(value time.Time) bool {
	return !value.IsZero() && value.Year() >= 1970 && value.Year() < 2200 && value.UnixNano() > 0
}
