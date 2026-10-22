package auth

import (
	"context"
	"errors"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/authorization"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/credentials"
)

type RotationManager struct {
	State          credentials.Rotations
	Backend        credentials.SecureBackend
	Refresh        credentials.RefreshProvider
	Locks          credentials.RefreshLocker
	Now            func() time.Time
	RefreshTimeout time.Duration
}

func (manager RotationManager) Rotate(ctx context.Context, key authorization.Key, observedRevision int64) (authorization.CredentialVersion, error) {
	if ctx == nil || key.Validate() != nil || observedRevision < 1 || manager.State == nil ||
		manager.Backend == nil || manager.Refresh == nil || manager.Locks == nil ||
		manager.RefreshTimeout < 0 || manager.RefreshTimeout > 2*time.Minute {
		return authorization.CredentialVersion{}, credentials.ErrInvalidState
	}
	if err := ctx.Err(); err != nil {
		return authorization.CredentialVersion{}, err
	}
	release, err := manager.Locks.Acquire(ctx, key.Channel, key.AuthorizationID)
	if err != nil {
		return authorization.CredentialVersion{}, rotationError(ctx, err)
	}
	if release == nil {
		return authorization.CredentialVersion{}, credentials.ErrInvalidState
	}
	defer func() { _ = release() }()
	state, err := manager.State.LoadCredentialState(ctx, key)
	if err != nil {
		return authorization.CredentialVersion{}, rotationError(ctx, err)
	}
	if state.Validate() != nil || state.Credential.Key != key {
		return authorization.CredentialVersion{}, credentials.ErrInvalidState
	}
	if state.Status == authorization.ReauthorizationRequired {
		return authorization.CredentialVersion{}, credentials.ErrReauthorizationRequired
	}
	if state.Rotation != nil {
		return manager.recoverRotation(ctx, state)
	}
	if state.Credential.Revision != observedRevision {
		return state.Credential, nil
	}
	current, err := manager.Backend.Read(ctx, state.Credential.Reference)
	if err != nil || current.Validate() != nil || !current.Version.Equal(state.Credential) ||
		!current.Version.RefreshExpiresAt.After(manager.now()) {
		return authorization.CredentialVersion{}, credentials.ErrReauthorizationRequired
	}
	rotation, err := authorization.NewRotation(state.Credential, manager.now())
	if err != nil {
		return authorization.CredentialVersion{}, credentials.ErrInvalidState
	}
	if err := manager.State.BeginCredentialRotation(ctx, rotation); err != nil {
		return authorization.CredentialVersion{}, rotationError(ctx, err)
	}
	timeout := manager.RefreshTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	refreshCtx, cancel := context.WithTimeout(ctx, timeout)
	result, refreshErr := manager.Refresh.Refresh(refreshCtx, key, current.Secrets)
	cancel()
	if refreshErr != nil {
		return manager.block(ctx, rotation, "official_result_unknown")
	}
	candidate, err := authorization.NewCredentialVersion(key, rotation.NewRevision, manager.now(), result.AccessExpiresAt, result.RefreshExpiresAt)
	if err != nil || result.Secrets.Validate() != nil {
		return manager.block(ctx, rotation, "candidate_invalid")
	}
	record := credentials.Record{Version: candidate, Secrets: result.Secrets}
	writeErr := manager.Backend.Put(ctx, record)
	stored, readErr := manager.Backend.Read(ctx, rotation.CandidateReference)
	if readErr != nil {
		return manager.block(ctx, rotation, "candidate_unavailable")
	}
	if stored.Validate() != nil || !stored.Version.Equal(candidate) || stored.Secrets != record.Secrets {
		return manager.block(ctx, rotation, "candidate_invalid")
	}
	_ = writeErr
	if err := manager.State.RecordCredentialCandidate(ctx, key, rotation.NewRevision, candidate); err != nil {
		return authorization.CredentialVersion{}, rotationError(ctx, err)
	}
	return manager.activate(ctx, rotation, candidate)
}

func (manager RotationManager) recoverRotation(ctx context.Context, state authorization.CredentialState) (authorization.CredentialVersion, error) {
	rotation := *state.Rotation
	stored, err := manager.Backend.Read(ctx, rotation.CandidateReference)
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return manager.block(ctx, rotation, "candidate_unavailable")
		}
		return authorization.CredentialVersion{}, rotationError(ctx, err)
	}
	if stored.Validate() != nil || !rotation.Accepts(stored.Version) || stored.Version.IssuedAt.After(manager.now()) ||
		!stored.Version.RefreshExpiresAt.After(manager.now()) ||
		(rotation.Candidate != nil && !rotation.Candidate.Equal(stored.Version)) {
		return manager.block(ctx, rotation, "candidate_invalid")
	}
	if rotation.Status == authorization.RotationIntent {
		if err := manager.State.RecordCredentialCandidate(ctx, rotation.Key, rotation.NewRevision, stored.Version); err != nil {
			return authorization.CredentialVersion{}, rotationError(ctx, err)
		}
	}
	return manager.activate(ctx, rotation, stored.Version)
}

func (manager RotationManager) activate(ctx context.Context, rotation authorization.Rotation, candidate authorization.CredentialVersion) (authorization.CredentialVersion, error) {
	if rotation.Status != authorization.RotationActivated {
		if err := manager.State.ActivateCredentialCandidate(ctx, rotation.Key, rotation.NewRevision, manager.now()); err != nil {
			return authorization.CredentialVersion{}, rotationError(ctx, err)
		}
	}
	if err := manager.State.CompleteCredentialRotation(ctx, rotation.Key, rotation.NewRevision, manager.now()); err != nil {
		return authorization.CredentialVersion{}, rotationError(ctx, err)
	}
	return candidate, nil
}

func (manager RotationManager) block(ctx context.Context, rotation authorization.Rotation, code string) (authorization.CredentialVersion, error) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := manager.State.RequireCredentialReauthorization(cleanup, rotation.Key, rotation.NewRevision, code, manager.now()); err != nil {
		return authorization.CredentialVersion{}, credentials.ErrUnavailable
	}
	return authorization.CredentialVersion{}, credentials.ErrReauthorizationRequired
}

func (manager RotationManager) now() time.Time {
	if manager.Now != nil {
		return manager.Now().UTC()
	}
	return time.Now().UTC()
}

func rotationError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	for _, known := range []error{credentials.ErrNotFound, credentials.ErrConflict, credentials.ErrInvalidState, credentials.ErrReauthorizationRequired} {
		if errors.Is(err, known) {
			return known
		}
	}
	return credentials.ErrUnavailable
}
