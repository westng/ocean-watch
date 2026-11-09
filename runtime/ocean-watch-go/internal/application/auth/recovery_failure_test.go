package auth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type diagnosticFixtureError struct {
	reason, requestID string
	code              int64
	httpStatus        int
}

func (err diagnosticFixtureError) Error() string {
	return "fixture-private-secret fixture-access-token fixture-auth-code"
}
func (err diagnosticFixtureError) AuthorizationFailureReason() string { return err.reason }
func (err diagnosticFixtureError) AuthorizationFailureMetadata() (int64, string, int) {
	return err.code, err.requestID, err.httpStatus
}
func (err diagnosticFixtureError) RequiresReauthorization() bool {
	return err.code == 40103 || err.code == 40107
}

type diagnosticWriteFailure struct {
	CredentialStore
	err error
}

type diagnosticAccountFailure struct {
	endpoint, validation string
}

func (diagnosticAccountFailure) Error() string { return "fixture-private-secret" }
func (err diagnosticAccountFailure) AuthorizationFailureContext() (string, string) {
	return err.endpoint, err.validation
}

func TestAccountDiscoveryFailureContextIsAllowlisted(t *testing.T) {
	for _, test := range []struct {
		name, endpoint, validation string
		retained                   bool
	}{
		{"classified", "/2/advertiser/info/", "INCOMPLETE_ADVERTISER_INFO", true},
		{"unsafe", "/2/advertiser/info/?access_token=fixture-private-secret", "fixture-private-secret", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			view := RecoverySession{}
			view.retainFailure(diagnosticAccountFailure{test.endpoint, test.validation}, failureAccountSync)
			if test.retained != (view.FailureEndpoint != "") || test.retained != (view.FailureValidation != "") {
				t.Fatal("account context was not filtered through its stable allowlists")
			}
			encoded, _ := json.Marshal(view.FailureDiagnostics())
			if strings.Contains(string(encoded), "fixture-private-secret") {
				t.Fatal("raw account error or untrusted metadata escaped into retained diagnostics")
			}
		})
	}
}

func (store diagnosticWriteFailure) Write(context.Context, string, map[string]any) (string, error) {
	return "", store.err
}

type diagnosticReadFailure struct{ CredentialStore }

func (diagnosticReadFailure) Read(context.Context, string) (map[string]any, error) {
	return nil, errors.New("fixture-private-secret")
}

type diagnosticActivationFailure struct {
	AuthorizationStore
	updates int
}

func (store *diagnosticActivationFailure) UpdateChannel(ctx context.Context, channel string, update func(map[string]any) error) error {
	store.updates++
	if store.updates == 2 {
		return errors.New("fixture-private-secret fixture-access-token")
	}
	return store.AuthorizationStore.UpdateChannel(ctx, channel, update)
}

func TestRecoveryFailureIdentifiesTheFailedStageWithoutRetainingSecrets(t *testing.T) {
	for _, test := range []struct {
		name, stage, reason string
		pending             bool
		configure           func(*Service)
	}{
		{"app read", failureAppCredentials, "APP_CREDENTIAL_READ_FAILED", false, func(flow *Service) {
			flow.Credentials = diagnosticReadFailure{flow.Credentials}
		}},
		{"official rejection", failureTokenExchange, "OFFICIAL_API_ERROR", false, func(flow *Service) {
			flow.OAuth = recoveryOAuthFunc(func(context.Context) (domain.OAuthToken, error) {
				return domain.OAuthToken{}, diagnosticFixtureError{reason: "OFFICIAL_API_ERROR", code: 40001, requestID: "request-fixture", httpStatus: 200}
			})
		}},
		{"transport failure", failureTokenExchange, "REQUEST_FAILED", false, func(flow *Service) {
			flow.OAuth = recoveryOAuthFunc(func(context.Context) (domain.OAuthToken, error) {
				return domain.OAuthToken{}, diagnosticFixtureError{reason: "REQUEST_FAILED"}
			})
		}},
		{"authorized user", failureAuthorizedUser, "INVALID_USER_INFO_RESPONSE", false, func(flow *Service) {
			flow.Users = authorizedUserFunc(func(context.Context, string, string) (domain.AuthorizedUser, error) {
				return domain.AuthorizedUser{}, diagnosticFixtureError{reason: "INVALID_USER_INFO_RESPONSE", requestID: "user-fixture", httpStatus: 200}
			})
		}},
		{"identity mismatch", failureAuthorizedUser, "AUTHORIZATION_IDENTITY_MISMATCH", false, func(flow *Service) {
			flow.Users = authorizedUserFunc(func(context.Context, string, string) (domain.AuthorizedUser, error) {
				return domain.AuthorizedUser{ID: "7001", AppID: "456"}, nil
			})
		}},
		{"credential write", failureCredentialStorage, "CREDENTIAL_WRITE_FAILED", false, func(flow *Service) {
			flow.Authorizer.Credentials = diagnosticWriteFailure{flow.Authorizer.Credentials, diagnosticFixtureError{reason: "CREDENTIAL_WRITE_FAILED"}}
		}},
		{"credential verification", failureCredentialStorage, "CREDENTIAL_VERIFY_FAILED", false, func(flow *Service) {
			flow.Authorizer.Credentials = diagnosticWriteFailure{flow.Authorizer.Credentials, diagnosticFixtureError{reason: "CREDENTIAL_VERIFY_FAILED"}}
		}},
		{"authorization commit", failureAuthorizationStorage, "AUTHORIZATION_STORE_FAILED", false, func(flow *Service) {
			flow.Authorizer.Authorizations.(*memoryAuthorizationStore).failUpdate = true
		}},
		{"account discovery", failureAccountSync, "ACCOUNT_DISCOVERY_FAILED", true, func(flow *Service) {
			flow.Syncer.Discovery = recoveryDiscoveryFunc(func(context.Context) (domain.AdvertiserSnapshot, error) {
				return domain.AdvertiserSnapshot{}, errors.New("fixture-private-secret fixture-access-token")
			})
		}},
		{"expired discovery token", failureAccountSync, "OFFICIAL_API_ERROR", true, func(flow *Service) {
			flow.Syncer.Discovery = recoveryDiscoveryFunc(func(context.Context) (domain.AdvertiserSnapshot, error) {
				return domain.AdvertiserSnapshot{}, diagnosticFixtureError{reason: "OFFICIAL_API_ERROR", code: 40103}
			})
		}},
		{"account snapshot validation", failureAccountSync, "ACCOUNT_SNAPSHOT_INVALID", true, func(flow *Service) {
			flow.Syncer.Discovery = staticDiscovery{snapshot: domain.AdvertiserSnapshot{
				Accounts: []domain.AuthorizedAccount{{AccountID: "9001", AdvertiserIDs: []string{"1001"}}},
			}}
		}},
		{"account snapshot commit", failureAccountSync, "ACCOUNT_ACTIVATION_FAILED", true, func(flow *Service) {
			flow.Authorizer.Authorizations = &diagnosticActivationFailure{AuthorizationStore: flow.Authorizer.Authorizations}
		}},
		{"account ownership conflict", failureAccountSync, "ACCOUNT_MAPPING_CONFLICT", true, func(flow *Service) {
			flow.Authorizer.Authorizations.(*memoryAuthorizationStore).channels["qianchuan"] = map[string]any{
				"authorizations": map[string]any{"old-grant": map[string]any{
					"authorized_accounts": []any{map[string]any{"account_id": "9001", "advertiser_ids": []any{"1001"}}},
				}},
				"account_index": map[string]any{"9001": "old-grant"},
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			coordinator, store, state := recoveryCoordinatorFixture(t, time.Minute, test.configure)
			if err := coordinator.AcceptCallback(state, "fixture-auth-code", ""); err != nil {
				t.Fatal(err)
			}
			awaitRecoverySignal(t, coordinator.Done())
			view := coordinator.View()
			if view.Status != "failed" || view.CanResume || view.FailureStage != test.stage || view.FailureReason != test.reason || view.SyncRequired != test.pending {
				t.Fatalf("incorrect failure classification: %+v", view)
			}
			if test.name == "official rejection" && (view.OfficialCode != 40001 || view.OfficialRequestID != "request-fixture" || view.HTTPStatus != 200) {
				t.Fatal("official trace metadata was lost")
			}
			if test.name == "identity mismatch" && view.ErrorCode != "AUTHORIZATION_IDENTITY_MISMATCH" {
				t.Fatal("identity mismatch was reported as a storage failure")
			}
			if test.name == "expired discovery token" && view.ErrorCode != "REAUTHORIZATION_REQUIRED" {
				t.Fatal("invalid discovery access was incorrectly sent back to account sync")
			}
			if view.FailureDiagnostics()["failure_reason"] != test.reason {
				t.Fatal("transport diagnostics lost the classified failure reason")
			}
			metadata, err := store.LoadChannel(context.Background(), "qianchuan")
			if err != nil {
				t.Fatal(err)
			}
			if test.pending != (metadata["authorizations"] != nil) {
				t.Fatal("failure changed whether the grant is available for account sync")
			}
			encoded, err := json.Marshal(view)
			if err != nil {
				t.Fatal(err)
			}
			for _, private := range []string{"fixture-private-secret", "fixture-access-token", "fixture-auth-code"} {
				if strings.Contains(string(encoded), private) {
					t.Fatal("failure retained a credential or callback code")
				}
			}
		})
	}
}

func TestRecoveryFailureDropsUnsafeAdapterMetadata(t *testing.T) {
	coordinator, _, state := recoveryCoordinatorFixture(t, time.Minute, func(flow *Service) {
		flow.OAuth = recoveryOAuthFunc(func(context.Context) (domain.OAuthToken, error) {
			return domain.OAuthToken{}, diagnosticFixtureError{reason: "fixture-private-secret", requestID: "https://example.test/?secret=fixture-private-secret", code: -1, httpStatus: 999}
		})
	})
	if err := coordinator.AcceptCallback(state, "fixture-auth-code", ""); err != nil {
		t.Fatal(err)
	}
	awaitRecoverySignal(t, coordinator.Done())
	view := coordinator.View()
	if view.FailureReason != "TOKEN_EXCHANGE_FAILED" || view.OfficialCode != 0 || view.OfficialRequestID != "" || view.HTTPStatus != 0 {
		t.Fatalf("unsafe metadata retained: %+v", view)
	}
	encoded, _ := json.Marshal(view.FailureDiagnostics())
	if strings.Contains(string(encoded), "fixture-private-secret") || strings.Contains(string(encoded), "https://") {
		t.Fatal("unsafe metadata reached transport diagnostics")
	}
}

func TestSuccessfulRecoveryOmitsFailureDiagnostics(t *testing.T) {
	coordinator, _, state := recoveryCoordinatorFixture(t, time.Minute, nil)
	if err := coordinator.AcceptCallback(state, "fixture-auth-code", ""); err != nil {
		t.Fatal(err)
	}
	awaitRecoverySignal(t, coordinator.Done())
	view := coordinator.View()
	if view.Status != "completed" || view.FailureDiagnostics() != nil {
		t.Fatal("successful authorization reported failure diagnostics")
	}
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), "failure_stage") || strings.Contains(string(encoded), "official_error_code") {
		t.Fatal("successful result contains failure fields")
	}
}
