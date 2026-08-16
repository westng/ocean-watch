package auth

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

type QueryService struct {
	Credentials    CredentialStore
	Authorizations AuthorizationStore
}

type StatusQuery struct {
	Channel      string
	AdvertiserID string
}

type StatusResult struct {
	Channel                   string                `json:"channel"`
	HasAppID                  bool                  `json:"has_app_id"`
	HasSecret                 bool                  `json:"has_secret"`
	AuthorizationCount        int                   `json:"authorization_count"`
	Authorizations            []AuthorizationStatus `json:"authorizations"`
	AuthorizedAccountCount    int                   `json:"authorized_account_count"`
	AuthorizedAdvertiserCount int                   `json:"authorized_advertiser_count"`
	Generation                int                   `json:"generation"`
	AdvertiserID              string                `json:"advertiser_id,omitempty"`
	AdvertiserIDAuthorized    *bool                 `json:"advertiser_id_authorized,omitempty"`
}

type AuthorizationStatus struct {
	AuthorizationID           string `json:"authorization_id"`
	TokenRevision             int    `json:"token_revision"`
	HasAccessToken            bool   `json:"has_access_token"`
	HasRefreshToken           bool   `json:"has_refresh_token"`
	AccessTokenExpiresAt      string `json:"access_token_expires_at"`
	RefreshTokenExpiresAt     string `json:"refresh_token_expires_at"`
	PendingAccountSync        bool   `json:"pending_account_sync"`
	AuthorizedAccountCount    int    `json:"authorized_account_count"`
	AuthorizedAdvertiserCount int    `json:"authorized_advertiser_count"`
}

type MappingResult struct {
	Channel                 string                 `json:"channel"`
	AdvertiserFilter        string                 `json:"advertiser_filter"`
	AuthorizationCount      int                    `json:"authorization_count"`
	MappingCount            int                    `json:"mapping_count"`
	CredentialValuesExposed bool                   `json:"credential_values_exposed"`
	Mappings                []AdvertiserMapping    `json:"mappings"`
	Authorizations          []AuthorizationMapping `json:"authorizations"`
}

type AdvertiserMapping struct {
	AdvertiserID     string   `json:"advertiser_id"`
	AuthorizationIDs []string `json:"authorization_ids"`
	Ambiguous        bool     `json:"ambiguous"`
}

type AuthorizationMapping struct {
	AuthorizationID    string              `json:"authorization_id"`
	TokenRevision      int                 `json:"token_revision"`
	HasAccessToken     bool                `json:"has_access_token"`
	HasRefreshToken    bool                `json:"has_refresh_token"`
	PendingAccountSync bool                `json:"pending_account_sync"`
	AdvertiserIDs      []string            `json:"advertiser_ids"`
	AuthorizedAccounts []AuthorizedAccount `json:"authorized_accounts"`
}

type AuthorizedAccount struct {
	AccountID     any      `json:"account_id"`
	AccountName   any      `json:"account_name"`
	AccountRole   any      `json:"account_role"`
	AccountType   any      `json:"account_type"`
	AdvertiserIDs []string `json:"advertiser_ids"`
}

type InspectionResult struct {
	Status   StatusResult
	Mappings MappingResult
}

type authorizationView struct {
	ID                    string
	Revision              int
	HasAccessToken        bool
	HasRefreshToken       bool
	AccessTokenExpiresAt  string
	RefreshTokenExpiresAt string
	PendingAccountSync    bool
	AdvertiserIDs         []string
	AuthorizedAccounts    []AuthorizedAccount
}

func (service QueryService) Status(ctx context.Context, query StatusQuery) (StatusResult, error) {
	inspection, err := service.inspect(ctx, query, true)
	return inspection.Status, err
}

func (service QueryService) Mappings(ctx context.Context, query StatusQuery) (MappingResult, error) {
	inspection, err := service.inspect(ctx, query, false)
	return inspection.Mappings, err
}

func (service QueryService) Inspect(ctx context.Context, query StatusQuery) (InspectionResult, error) {
	return service.inspect(ctx, query, true)
}

func (service QueryService) inspect(ctx context.Context, query StatusQuery, readApp bool) (InspectionResult, error) {
	if service.Credentials == nil || service.Authorizations == nil {
		return InspectionResult{}, errors.New("authorization query dependencies are incomplete")
	}
	if query.Channel != "marketing" && query.Channel != "qianchuan" {
		return InspectionResult{}, errors.New("authorization channel is not supported")
	}
	if query.AdvertiserID != "" {
		if err := domain.ValidateDecimalID(query.AdvertiserID, "advertiser_id"); err != nil {
			return InspectionResult{}, err
		}
	}
	state, err := service.Authorizations.LoadChannel(ctx, query.Channel)
	if err != nil {
		return InspectionResult{}, err
	}
	views, err := service.authorizationViews(ctx, query.Channel, state)
	if err != nil {
		return InspectionResult{}, err
	}
	appIDPresent, secretPresent := false, false
	if readApp {
		account, accountErr := domain.AppCredentialAccount(query.Channel)
		if accountErr != nil {
			return InspectionResult{}, accountErr
		}
		app, readErr := service.Credentials.Read(ctx, account)
		if readErr != nil {
			return InspectionResult{}, readErr
		}
		appIDPresent = queryCredentialValue(app, "app_id") != ""
		secretPresent = queryCredentialValue(app, "secret") != ""
	}
	advertiserIndex := configuration.Object(state["advertiser_index"])
	statusRows := make([]AuthorizationStatus, 0, len(views))
	mappingRows := make([]AuthorizationMapping, 0, len(views))
	for _, view := range views {
		statusRows = append(statusRows, AuthorizationStatus{
			AuthorizationID: view.ID, TokenRevision: view.Revision,
			HasAccessToken: view.HasAccessToken, HasRefreshToken: view.HasRefreshToken,
			AccessTokenExpiresAt:      view.AccessTokenExpiresAt,
			RefreshTokenExpiresAt:     view.RefreshTokenExpiresAt,
			PendingAccountSync:        view.PendingAccountSync,
			AuthorizedAccountCount:    len(view.AuthorizedAccounts),
			AuthorizedAdvertiserCount: len(view.AdvertiserIDs),
		})
		if query.AdvertiserID == "" || queryContains(view.AdvertiserIDs, query.AdvertiserID) {
			mappingRows = append(mappingRows, AuthorizationMapping{
				AuthorizationID: view.ID, TokenRevision: view.Revision,
				HasAccessToken: view.HasAccessToken, HasRefreshToken: view.HasRefreshToken,
				PendingAccountSync: view.PendingAccountSync,
				AdvertiserIDs:      append([]string(nil), view.AdvertiserIDs...),
				AuthorizedAccounts: append([]AuthorizedAccount(nil), view.AuthorizedAccounts...),
			})
		}
	}
	mappings := make([]AdvertiserMapping, 0, len(advertiserIndex))
	for _, advertiserID := range querySortedKeys(advertiserIndex) {
		if query.AdvertiserID != "" && advertiserID != query.AdvertiserID {
			continue
		}
		owners := queryStringList(advertiserIndex[advertiserID])
		mappings = append(mappings, AdvertiserMapping{
			AdvertiserID: advertiserID, AuthorizationIDs: owners, Ambiguous: len(owners) > 1,
		})
	}
	if query.AdvertiserID != "" && len(mappings) == 0 {
		return InspectionResult{}, domain.NewError(
			"authorization_not_found", "advertiser is not mapped to an authorization", 1,
			map[string]any{"advertiser_id": query.AdvertiserID},
		)
	}
	generation, err := queryIntegerValue(state["generation"], 0)
	if err != nil || generation < 0 {
		return InspectionResult{}, errors.New("authorization generation must be a non-negative integer")
	}
	status := StatusResult{
		Channel: query.Channel, HasAppID: appIDPresent, HasSecret: secretPresent,
		AuthorizationCount: len(statusRows), Authorizations: statusRows,
		AuthorizedAccountCount:    len(configuration.Object(state["account_index"])),
		AuthorizedAdvertiserCount: len(advertiserIndex), Generation: generation,
		AdvertiserID: query.AdvertiserID,
	}
	if query.AdvertiserID != "" {
		authorized := len(queryStringList(advertiserIndex[query.AdvertiserID])) != 0
		status.AdvertiserIDAuthorized = &authorized
	}
	return InspectionResult{
		Status: status,
		Mappings: MappingResult{
			Channel: query.Channel, AdvertiserFilter: query.AdvertiserID,
			AuthorizationCount: len(mappingRows), MappingCount: len(mappings),
			CredentialValuesExposed: false, Mappings: mappings, Authorizations: mappingRows,
		},
	}, nil
}

func (service QueryService) authorizationViews(
	ctx context.Context,
	channel string,
	state map[string]any,
) ([]authorizationView, error) {
	authorizations := configuration.Object(state["authorizations"])
	views := make([]authorizationView, 0, len(authorizations))
	for _, authorizationID := range querySortedKeys(authorizations) {
		metadata := configuration.Object(authorizations[authorizationID])
		revision, err := queryIntegerValue(metadata["token_revision"], 1)
		if err != nil || revision < 1 {
			return nil, errors.New("token_revision must be positive")
		}
		account, err := domain.AuthorizationCredentialAccount(channel, authorizationID, revision)
		if err != nil {
			return nil, err
		}
		token, err := service.Credentials.Read(ctx, account)
		if err != nil {
			return nil, err
		}
		views = append(views, authorizationView{
			ID: authorizationID, Revision: revision,
			HasAccessToken:        queryCredentialValue(token, "access_token") != "",
			HasRefreshToken:       queryCredentialValue(token, "refresh_token") != "",
			AccessTokenExpiresAt:  queryCredentialValue(token, "access_token_expires_at"),
			RefreshTokenExpiresAt: queryCredentialValue(token, "refresh_token_expires_at"),
			PendingAccountSync:    metadata["pending_account_sync"] == true,
			AdvertiserIDs:         queryStringList(metadata["advertiser_ids"]),
			AuthorizedAccounts:    querySafeAccounts(metadata["authorized_accounts"]),
		})
	}
	return views, nil
}

func queryCredentialValue(value map[string]any, key string) string {
	text := strings.TrimSpace(fmt.Sprint(value[key]))
	if text == "<nil>" || strings.HasPrefix(text, "REPLACE_WITH") {
		return ""
	}
	return text
}

func queryIntegerValue(value any, fallback int) (int, error) {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(text)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

func querySortedKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func queryStringList(value any) []string {
	result := []string{}
	switch values := value.(type) {
	case []any:
		for _, item := range values {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" && text != "<nil>" {
				result = append(result, text)
			}
		}
	case []string:
		result = append(result, values...)
	}
	return result
}

func querySafeAccounts(value any) []AuthorizedAccount {
	rows := []AuthorizedAccount{}
	items, _ := value.([]any)
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, AuthorizedAccount{
			AccountID: row["account_id"], AccountName: row["account_name"],
			AccountRole: row["account_role"], AccountType: row["account_type"],
			AdvertiserIDs: queryStringList(row["advertiser_ids"]),
		})
	}
	return rows
}

func queryContains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
