package onboarding

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

type CredentialReader interface {
	BackendName() string
	Read(context.Context, string) (map[string]any, error)
}

type AuthorizationReader interface {
	ReadChannel(context.Context, string) (domain.AuthorizationState, error)
}

type LocalState struct {
	Credentials    CredentialReader
	Authorizations AuthorizationReader
}

type CredentialSnapshot struct {
	App           map[string]any
	Runtime       map[string]any
	ChannelStatus map[string]any
	Status        map[string]any
}

func (state LocalState) Snapshot(
	ctx context.Context,
	channel string,
	advertiserID string,
	config map[string]any,
) (CredentialSnapshot, error) {
	if state.Credentials == nil || state.Authorizations == nil {
		return CredentialSnapshot{}, fmt.Errorf("local credential state is not configured")
	}
	appAccount, err := domain.AppCredentialAccount(channel)
	if err != nil {
		return CredentialSnapshot{}, err
	}
	app, err := state.Credentials.Read(ctx, appAccount)
	if err != nil {
		return CredentialSnapshot{}, err
	}
	authorizations, err := state.Authorizations.ReadChannel(ctx, channel)
	if err != nil {
		return CredentialSnapshot{}, err
	}

	rows := make([]any, 0, len(authorizations.Authorizations))
	accessTokenCount := 0
	refreshTokenCount := 0
	selected := []map[string]any{}
	for _, authorization := range authorizations.Authorizations {
		account, accountErr := domain.AuthorizationCredentialAccount(
			channel,
			authorization.AuthorizationID,
			authorization.TokenRevision,
		)
		if accountErr != nil {
			return CredentialSnapshot{}, accountErr
		}
		credential, readErr := state.Credentials.Read(ctx, account)
		if readErr != nil {
			return CredentialSnapshot{}, readErr
		}
		if !configuration.Missing(credential["access_token"]) {
			accessTokenCount++
		}
		if !configuration.Missing(credential["refresh_token"]) {
			refreshTokenCount++
		}
		if authorizationMatches(authorization, advertiserID) {
			selected = append(selected, credential)
		}
		rows = append(rows, map[string]any{
			"authorization_id":           authorization.AuthorizationID,
			"account_ids":                append([]string(nil), authorization.AccountIDs...),
			"advertiser_count":           len(authorization.AdvertiserIDs),
			"pending_account_sync":       authorization.PendingAccountSync,
			"account_discovery_complete": authorization.AccountDiscoveryComplete,
			"account_discovery_issues":   append([]any(nil), authorization.AccountDiscoveryIssues...),
		})
	}
	runtimeCredential := map[string]any{}
	if len(selected) == 1 {
		for key, value := range selected[0] {
			if key != "developer_id" {
				runtimeCredential[key] = configuration.Clone(value)
			}
		}
	}

	channelStatus := map[string]any{
		"channel":                                channel,
		"authorization_count":                    authorizations.AuthorizationCount,
		"authorized_account_count":               authorizations.AuthorizedAccountCount,
		"authorized_advertiser_count":            len(authorizations.AdvertiserIDs),
		"has_app_id":                             !configuration.Missing(app["app_id"]),
		"has_secret":                             !configuration.Missing(app["secret"]),
		"generation":                             authorizations.Generation,
		"authorization_with_access_token_count":  accessTokenCount,
		"authorization_with_refresh_token_count": refreshTokenCount,
		"pending_account_sync_count":             authorizations.PendingAccountSyncCount,
		"partial_account_discovery_count":        authorizations.PartialAccountDiscoveryCount,
		"authorizations":                         rows,
	}
	if advertiserID != "" {
		channelStatus["advertiser_id"] = advertiserID
		channelStatus["advertiser_id_authorized"] = containsString(authorizations.AdvertiserIDs, advertiserID)
	}

	backend := state.Credentials.BackendName()
	credentialLocation := any("system credential store")
	if backend == "unavailable" || backend == "" {
		credentialLocation = nil
	} else if backend == "file-fallback" {
		credentialLocation = "development-only local credential file"
	}
	status := cloneStatus(channelStatus)
	status["backend"] = backend
	status["credential_location"] = credentialLocation
	status["secure_backend_available"] = backend != "" && backend != "file-fallback" && backend != "unavailable"
	status["insecure_file_fallback"] = backend == "file-fallback"
	status["project_config_has_sensitive_fields"] = configuration.SensitiveFields(config)

	return CredentialSnapshot{
		App: configuration.CloneMap(app), Runtime: runtimeCredential,
		ChannelStatus: channelStatus, Status: status,
	}, nil
}

func (state LocalState) ChannelRows(
	ctx context.Context,
	config map[string]any,
	advertiserID string,
) ([]any, error) {
	rows := make([]any, 0, 2)
	configuredChannels := configuration.Object(config["channels"])
	for _, channel := range []string{"marketing", "qianchuan"} {
		definition := configuration.Channels[channel]
		configured := configuration.Object(configuredChannels[channel])
		snapshot, err := state.Snapshot(ctx, channel, advertiserID, config)
		if err != nil {
			return nil, err
		}
		row := map[string]any{
			"channel": channel, "display_name": definition.DisplayName,
			"implemented": true,
			"configured":  len(configured) != 0 && strings.TrimSpace(fmt.Sprint(configured["status"])) != "not_implemented",
		}
		for key, value := range snapshot.ChannelStatus {
			row[key] = value
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func authorizationMatches(authorization domain.AuthorizationSummary, advertiserID string) bool {
	if authorization.PendingAccountSync {
		return false
	}
	if advertiserID == "" {
		return true
	}
	return containsString(authorization.AdvertiserIDs, advertiserID)
}

func containsString(values []string, target string) bool {
	index := sort.SearchStrings(values, target)
	if index < len(values) && values[index] == target {
		return true
	}
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func cloneStatus(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = configuration.Clone(item)
	}
	return result
}
