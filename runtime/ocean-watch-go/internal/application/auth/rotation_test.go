package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/authorization"
	credentialport "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/credentials"
)

type rotationFakeBackend struct {
	mu      sync.Mutex
	entries map[string]credentialport.Record
	putErr  error
}

func (backend *rotationFakeBackend) Read(_ context.Context, reference string) (credentialport.Record, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	entry, ok := backend.entries[reference]
	if !ok {
		return credentialport.Record{}, credentialport.ErrNotFound
	}
	return entry, nil
}

func (backend *rotationFakeBackend) Put(_ context.Context, record credentialport.Record) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.putErr != nil {
		return backend.putErr
	}
	if err := record.Validate(); err != nil {
		return err
	}
	backend.entries[record.Version.Reference] = record
	return nil
}

type rotationFakeRefresh struct {
	mu     sync.Mutex
	calls  int
	delay  time.Duration
	result credentialport.RefreshResult
	err    error
	secret string
}

func (refresh *rotationFakeRefresh) Refresh(ctx context.Context, _ authorization.Key, _ credentialport.Secrets) (credentialport.RefreshResult, error) {
	if refresh.delay > 0 {
		timer := time.NewTimer(refresh.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return credentialport.RefreshResult{}, ctx.Err()
		case <-timer.C:
		}
	}
	refresh.mu.Lock()
	refresh.calls++
	refresh.mu.Unlock()
	if refresh.err != nil {
		return credentialport.RefreshResult{}, refresh.err
	}
	return refresh.result, nil
}

type rotationFakeLocker struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (locker *rotationFakeLocker) Acquire(_ context.Context, channel, authorizationID string) (func() error, error) {
	key := channel + ":" + authorizationID
	locker.mu.Lock()
	lock := locker.locks[key]
	if lock == nil {
		lock = new(sync.Mutex)
		locker.locks[key] = lock
	}
	locker.mu.Unlock()
	lock.Lock()
	return func() error {
		lock.Unlock()
		return nil
	}, nil
}

type rotationFakeState struct {
	mu             sync.Mutex
	state          authorization.CredentialState
	failActivation bool
}

func (state *rotationFakeState) LoadCredentialState(_ context.Context, _ authorization.Key) (authorization.CredentialState, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	return cloneCredentialState(state.state), nil
}

func (state *rotationFakeState) BeginCredentialRotation(_ context.Context, rotation authorization.Rotation) error {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.state.Rotation != nil || state.state.Credential.Revision != rotation.OldRevision || state.state.Credential.Reference != rotation.OldReference {
		return credentialport.ErrConflict
	}
	copy := rotation
	state.state.Rotation = &copy
	return nil
}

func (state *rotationFakeState) RecordCredentialCandidate(_ context.Context, key authorization.Key, revision int64, candidate authorization.CredentialVersion) error {
	state.mu.Lock()
	defer state.mu.Unlock()
	rotation := state.state.Rotation
	if rotation == nil || rotation.Key != key || rotation.NewRevision != revision || rotation.Status != authorization.RotationIntent || !rotation.Accepts(candidate) {
		return credentialport.ErrConflict
	}
	copy := candidate
	rotation.Candidate = &copy
	rotation.Status = authorization.RotationCandidateStored
	return nil
}

func (state *rotationFakeState) ActivateCredentialCandidate(_ context.Context, key authorization.Key, revision int64, at time.Time) error {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.failActivation {
		state.failActivation = false
		return errors.New("injected activation failure containing candidate-secret")
	}
	rotation := state.state.Rotation
	if rotation == nil || rotation.Key != key || rotation.NewRevision != revision || rotation.Candidate == nil || rotation.Status != authorization.RotationCandidateStored {
		return credentialport.ErrConflict
	}
	state.state.Credential = *rotation.Candidate
	rotation.Status = authorization.RotationActivated
	rotation.ActivatedAt = at.UTC()
	return nil
}

func (state *rotationFakeState) CompleteCredentialRotation(_ context.Context, key authorization.Key, revision int64, at time.Time) error {
	state.mu.Lock()
	defer state.mu.Unlock()
	rotation := state.state.Rotation
	if rotation == nil || rotation.Key != key || rotation.NewRevision != revision || rotation.Status != authorization.RotationActivated {
		return credentialport.ErrConflict
	}
	rotation.Status = authorization.RotationCompleted
	rotation.CompletedAt = at.UTC()
	state.state.Rotation = nil
	return nil
}

func (state *rotationFakeState) RequireCredentialReauthorization(_ context.Context, key authorization.Key, revision int64, code string, _ time.Time) error {
	state.mu.Lock()
	defer state.mu.Unlock()
	rotation := state.state.Rotation
	if rotation == nil || rotation.Key != key || rotation.NewRevision != revision || !authorization.ValidFailureCode(code) {
		return credentialport.ErrConflict
	}
	rotation.Status = authorization.RotationReauthorizationRequired
	rotation.FailureCode = code
	state.state.Status = authorization.ReauthorizationRequired
	return nil
}

func cloneCredentialState(state authorization.CredentialState) authorization.CredentialState {
	if state.Rotation == nil {
		return state
	}
	rotation := *state.Rotation
	if rotation.Candidate != nil {
		candidate := *rotation.Candidate
		rotation.Candidate = &candidate
	}
	state.Rotation = &rotation
	return state
}

func rotationFixture(t *testing.T) (authorization.Key, authorization.CredentialVersion, credentialport.Record) {
	t.Helper()
	key := authorization.Key{Channel: "marketing", AuthorizationID: "auth-rotation"}
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	version, err := authorization.NewCredentialVersion(key, 1, now, now.Add(time.Hour), now.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return key, version, credentialport.Record{Version: version, Secrets: credentialport.Secrets{AccessToken: "access-old", RefreshToken: "refresh-old"}}
}

func rotationManager(state *rotationFakeState, backend *rotationFakeBackend, refresh *rotationFakeRefresh, locker *rotationFakeLocker, now time.Time) RotationManager {
	return RotationManager{State: state, Backend: backend, Refresh: refresh, Locks: locker, Now: func() time.Time { return now }, RefreshTimeout: time.Second}
}

func TestRotationResumesStoredCandidateAfterActivationFailureAndRestart(t *testing.T) {
	key, current, record := rotationFixture(t)
	backend := &rotationFakeBackend{entries: map[string]credentialport.Record{current.Reference: record}}
	now := current.IssuedAt.Add(2 * time.Minute)
	candidate, err := authorization.NewCredentialVersion(key, 2, now, now.Add(time.Hour), now.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	refresh := &rotationFakeRefresh{result: credentialport.RefreshResult{
		Secrets:         credentialport.Secrets{AccessToken: "access-new-secret", RefreshToken: "refresh-new-secret"},
		AccessExpiresAt: candidate.AccessExpiresAt, RefreshExpiresAt: candidate.RefreshExpiresAt,
	}}
	state := &rotationFakeState{state: authorization.CredentialState{Credential: current, Status: authorization.Active}}
	state.failActivation = true
	locker := &rotationFakeLocker{locks: map[string]*sync.Mutex{}}
	manager := rotationManager(state, backend, refresh, locker, now)
	if _, err := manager.Rotate(context.Background(), key, 1); !errors.Is(err, credentialport.ErrUnavailable) {
		t.Fatalf("first rotation = %v, want activation failure", err)
	}
	if refresh.calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", refresh.calls)
	}
	persisted, err := state.LoadCredentialState(context.Background(), key)
	if err != nil || persisted.Rotation == nil || persisted.Rotation.Status != authorization.RotationCandidateStored {
		t.Fatalf("persisted candidate state = %#v, %v", persisted, err)
	}
	if _, err := manager.Rotate(context.Background(), key, 1); err != nil {
		t.Fatalf("restart recovery = %v", err)
	}
	if refresh.calls != 1 {
		t.Fatalf("restart performed a second refresh: %d", refresh.calls)
	}
	final, err := state.LoadCredentialState(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if final.Credential.Revision != 2 || final.Rotation != nil || final.Status != authorization.Active {
		t.Fatalf("final state = %#v", final)
	}
}

func TestRotationUnknownOfficialResultRequiresReauthorizationWithoutBlindRefresh(t *testing.T) {
	key, current, record := rotationFixture(t)
	backend := &rotationFakeBackend{entries: map[string]credentialport.Record{current.Reference: record}}
	refresh := &rotationFakeRefresh{err: errors.New("official response lost after sending refresh-secret-value")}
	state := &rotationFakeState{state: authorization.CredentialState{Credential: current, Status: authorization.Active}}
	locker := &rotationFakeLocker{locks: map[string]*sync.Mutex{}}
	manager := rotationManager(state, backend, refresh, locker, current.IssuedAt.Add(time.Minute))
	_, err := manager.Rotate(context.Background(), key, 1)
	if !errors.Is(err, credentialport.ErrReauthorizationRequired) {
		t.Fatalf("unknown result = %v, want reauthorization", err)
	}
	if strings.Contains(fmt.Sprint(err), "refresh-secret-value") {
		t.Fatalf("secret leaked in error: %v", err)
	}
	if _, err := manager.Rotate(context.Background(), key, 1); !errors.Is(err, credentialport.ErrReauthorizationRequired) {
		t.Fatalf("second rotation = %v, want persistent reauthorization", err)
	}
	if refresh.calls != 1 {
		t.Fatalf("blind refresh count = %d, want 1", refresh.calls)
	}
}

func TestRotationConcurrentCallsShareLockAndReloadRevision(t *testing.T) {
	key, current, record := rotationFixture(t)
	backend := &rotationFakeBackend{entries: map[string]credentialport.Record{current.Reference: record}}
	now := current.IssuedAt.Add(time.Minute)
	candidate, err := authorization.NewCredentialVersion(key, 2, now, now.Add(time.Hour), now.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	refresh := &rotationFakeRefresh{delay: 20 * time.Millisecond, result: credentialport.RefreshResult{
		Secrets:         credentialport.Secrets{AccessToken: "access-new", RefreshToken: "refresh-new"},
		AccessExpiresAt: candidate.AccessExpiresAt, RefreshExpiresAt: candidate.RefreshExpiresAt,
	}}
	state := &rotationFakeState{state: authorization.CredentialState{Credential: current, Status: authorization.Active}}
	locker := &rotationFakeLocker{locks: map[string]*sync.Mutex{}}
	manager := rotationManager(state, backend, refresh, locker, now)
	results := make(chan authorization.CredentialVersion, 2)
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			version, err := manager.Rotate(context.Background(), key, 1)
			if err != nil {
				errs <- err
				return
			}
			results <- version
		}()
	}
	wait.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	count := 0
	for version := range results {
		count++
		if version.Revision != 2 {
			t.Fatalf("returned revision = %d, want 2", version.Revision)
		}
	}
	if count != 2 || refresh.calls != 1 {
		t.Fatalf("results=%d refresh_calls=%d, want 2 and 1", count, refresh.calls)
	}
}
