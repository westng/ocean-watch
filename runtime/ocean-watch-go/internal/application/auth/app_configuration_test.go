package auth

import (
	"context"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type appVaultFixture struct {
	*memoryCredentials
	backend string
	writes  int
	corrupt bool
}

type blockingAppVault struct {
	*appVaultFixture
	entered chan struct{}
	exited  chan struct{}
}

func (f *blockingAppVault) Write(ctx context.Context, _ string, _ map[string]any) (string, error) {
	close(f.entered)
	<-ctx.Done()
	close(f.exited)
	return "", ctx.Err()
}

func TestConfigurationCancellationStopsCredentialWorkerBeforeTerminalState(t *testing.T) {
	service, _, credentials := authorizationServiceFixture()
	delete(credentials.entries, "oceanengine-app-marketing")
	vault := &blockingAppVault{appVaultFixture: &appVaultFixture{memoryCredentials: credentials, backend: "macos-keychain"}, entered: make(chan struct{}), exited: make(chan struct{})}
	service.Credentials = vault
	coordinator := NewRecoveryCoordinator(context.Background(), RecoveryRequest{Channel: "marketing", Operation: "authorize"}, RecoverySession{Channel: "marketing", Status: "awaiting_configuration"}, "fixture-state", time.Minute, service)
	defer coordinator.RequestCancel()
	finished := make(chan error, 1)
	go func() {
		finished <- coordinator.ConfigureApp(domain.OAuthApp{AppID: "123", Secret: "synthetic-secret"}, "https://official.example.test/")
	}()
	select {
	case <-vault.entered:
	case <-time.After(time.Second):
		t.Fatal("configuration did not reach vault")
	}
	cancelled := make(chan struct{})
	go func() { coordinator.RequestCancel(); close(cancelled) }()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("cancel waited on credential I/O without cancelling it")
	}
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("cancelled configuration succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("configuration worker did not stop")
	}
	select {
	case <-vault.exited:
	default:
		t.Fatal("terminal result preceded credential worker exit")
	}
	select {
	case <-coordinator.Done():
	case <-time.After(time.Second):
		t.Fatal("cancelled coordinator did not finish")
	}
	if coordinator.View().Status != "cancelled" {
		t.Fatal("configuration cancellation lost its terminal state")
	}
}

func (f *appVaultFixture) BackendName() string { return f.backend }
func (f *appVaultFixture) Write(ctx context.Context, account string, value map[string]any) (string, error) {
	f.writes++
	if f.corrupt {
		return f.backend, nil
	}
	_, err := f.memoryCredentials.Write(ctx, account, value)
	return f.backend, err
}

func TestMissingAppConfigurationRejectsUnsafeStorageAndIdentityChanges(t *testing.T) {
	for _, scenario := range []string{"success", "file_fallback", "changed_app", "grant_mismatch", "readback_failure"} {
		t.Run(scenario, func(t *testing.T) {
			service, state, credentials := authorizationServiceFixture()
			delete(credentials.entries, "oceanengine-app-marketing")
			vault := &appVaultFixture{memoryCredentials: credentials, backend: "macos-keychain"}
			service.Credentials = vault
			input := RecoveryRequest{Channel: "marketing", Operation: "authorize"}
			switch scenario {
			case "file_fallback":
				vault.backend = "file-fallback"
			case "changed_app":
				credentials.entries["oceanengine-app-marketing"] = map[string]any{"app_id": "456", "secret": "synthetic-existing"}
			case "grant_mismatch":
				input.AuthorizationID = "grant"
				state.channels["marketing"] = map[string]any{"authorizations": map[string]any{"grant": map[string]any{"app_id": "456"}}}
			case "readback_failure":
				vault.corrupt = true
			}
			err := service.ConfigureMissingApp(context.Background(), input, domain.OAuthApp{AppID: "123", Secret: "synthetic-new"})
			if scenario == "success" {
				if err != nil || vault.writes != 1 {
					t.Fatalf("valid configuration failed: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("unsafe configuration reported success")
				}
				if scenario != "readback_failure" && vault.writes != 0 {
					t.Fatal("unsafe configuration wrote credentials")
				}
			}
		})
	}
}
