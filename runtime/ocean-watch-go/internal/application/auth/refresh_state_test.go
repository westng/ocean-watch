package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

// Only used with synthetic values under the test's private temporary root.
type fixtureVault string

func (vault fixtureVault) Read(_ context.Context, account string) (map[string]any, error) {
	b, err := os.ReadFile(filepath.Join(string(vault), account))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entry map[string]any
	err = json.Unmarshal(b, &entry)
	return entry, err
}

func (vault fixtureVault) Write(_ context.Context, account string, entry map[string]any) (string, error) {
	b, err := json.Marshal(entry)
	if err != nil {
		return "", err
	}
	return "test-vault", os.WriteFile(filepath.Join(string(vault), account), b, 0600)
}

// The parent and child run sequentially and reopen both backends from disk.
type restartManifest struct{ vault fixtureVault }

func (s restartManifest) LoadChannel(ctx context.Context, channel string) (map[string]any, error) {
	return s.vault.Read(ctx, "manifest-"+channel)
}

func (s restartManifest) UpdateChannel(ctx context.Context, channel string, update func(map[string]any) error) error {
	state, err := s.LoadChannel(ctx, channel)
	if err != nil {
		return err
	}
	if err := update(state); err != nil {
		return err
	}
	_, err = s.vault.Write(ctx, "manifest-"+channel, state)
	return err
}

func TestRefreshCandidateRecoveredAcrossProcesses(t *testing.T) {
	const childArg = "--recover-fixture"
	args := os.Args
	child := len(args) >= 3 && args[len(args)-2] == childArg
	var root string
	if child {
		root = args[len(args)-1]
	} else {
		root = t.TempDir()
	}
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	vault := fixtureVault(root)
	state := restartManifest{vault}
	oauth := &countingOAuth{counts: map[string]int{}}
	manager := TokenManager{Credentials: vault, Authorizations: state, Locks: &keyedMemoryLocker{locks: make(map[string]*sync.Mutex)}, OAuth: oauth, Now: func() time.Time { return now }}
	query := TokenQuery{Channel: "marketing", AuthorizationID: "stable", ForceRefresh: true}
	if child {
		lease, err := manager.Ensure(ctx, query)
		if err != nil || lease.TokenRevision != 2 || lease.AccessToken != "new-marketing-access-1" || oauth.counts[query.Channel] != 0 {
			t.Fatalf("restart failed to reuse the persisted rotation: revision %d, refresh calls %d, %v", lease.TokenRevision, oauth.counts[query.Channel], err)
		}
		return
	}
	for account, entry := range tokenFixtureCredentials(now, query.Channel, query.AuthorizationID).entries {
		if _, err := vault.Write(ctx, account, entry); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := vault.Write(ctx, "manifest-"+query.Channel, tokenFixtureState(query.AuthorizationID)); err != nil {
		t.Fatal(err)
	}
	// Intent succeeds, candidate reaches the vault, manifest activation fails.
	manager.Authorizations = &diagnosticActivationFailure{AuthorizationStore: state}
	if _, err := manager.Ensure(ctx, query); err == nil || oauth.counts[query.Channel] != 1 {
		t.Fatalf("expected one interrupted rotation, got %v", err)
	}
	verify := func(revision int, pending bool) {
		t.Helper()
		manifest, err := state.LoadChannel(ctx, query.Channel)
		if err != nil {
			t.Fatal(err)
		}
		row := configuration.Object(configuration.Object(manifest["authorizations"])[query.AuthorizationID])
		actual, err := positiveInteger(row["token_revision"], 1)
		if err != nil || actual != revision || (pendingRefresh(row, revision) != "") != pending {
			t.Fatalf("unexpected state at revision %d", revision)
		}
		serialized, err := json.Marshal(manifest)
		if err != nil || bytes.Contains(serialized, []byte("new-marketing-")) || bytes.Contains(serialized, []byte("old-marketing-")) {
			t.Fatal("manifest must contain only nonsecret refresh metadata")
		}
	}
	verify(1, true)
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestRefreshCandidateRecoveredAcrossProcesses$", "--", childArg, root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restart: %v\n%s", err, out)
	}
	verify(2, false)
}

func TestPendingRefreshRejectsUnrelatedCandidateAndAllowsReauthorization(t *testing.T) {
	ctx := context.Background()
	state := tokenFixtureState("stable")
	row := configuration.Object(configuration.Object(state["authorizations"])["stable"])
	row["pending_refresh_attempt"] = "1:expected"
	store := &memoryAuthorizationStore{channels: map[string]map[string]any{"marketing": state}}
	vault := tokenFixtureCredentials(time.Now(), "marketing", "stable")
	const candidate = "oceanengine-auth-marketing-stable-r2"
	vault.entries[candidate] = map[string]any{"access_token": "unrelated", "refresh_token": "unrelated"}
	oauth := &countingOAuth{counts: map[string]int{}}
	manager := TokenManager{Credentials: vault, Authorizations: store, Locks: &keyedMemoryLocker{locks: make(map[string]*sync.Mutex)}, OAuth: oauth}
	var terminal *domain.Error
	if _, err := manager.Ensure(ctx, TokenQuery{Channel: "marketing", AuthorizationID: "stable"}); !errors.As(err, &terminal) || terminal.Code != "reauthorization_required" || oauth.counts["marketing"] != 0 {
		t.Fatalf("incomplete refresh reused an unrelated token: %v", err)
	}
	authorizer := Authorizer{Credentials: vault, Authorizations: store}
	request := AuthorizationRequest{Channel: "marketing", AuthorizationID: "stable", AppID: "100", UserID: "7001", Token: domain.OAuthToken{AccessToken: "fixture-access", RefreshToken: "fixture-refresh"}}
	store.failUpdate = true
	if _, _, err := authorizer.save(ctx, request); err == nil || vault.entries[candidate]["access_token"] != "unrelated" {
		t.Fatal("failed authorization destroyed the reserved candidate revision")
	}
	store.failUpdate = false
	result, _, err := authorizer.save(ctx, request)
	if err != nil || result.TokenRevision != 3 {
		t.Fatalf("reauthorization failed to supersede pending refresh: %v", err)
	}
}
