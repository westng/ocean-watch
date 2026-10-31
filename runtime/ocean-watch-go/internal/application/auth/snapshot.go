package auth

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

type TokenProvider interface {
	Ensure(context.Context, TokenQuery) (TokenLease, error)
}

type AdvertiserDiscovery interface {
	Discover(context.Context, string, string) (domain.AdvertiserSnapshot, error)
}

type AdvertiserSnapshotSync struct {
	Tokens         TokenProvider
	Authorizations AuthorizationStore
	Discovery      AdvertiserDiscovery
	Now            func() time.Time
}

type AdvertiserSyncQuery struct {
	Channel         string
	AuthorizationID string
	AuthAccountID   string
	RebindExisting  bool
}

type AdvertiserSyncResult struct {
	Channel            string
	AuthorizationID    string
	AuthorizedAccounts int
	AdvertiserIDs      []string
	DiscoveryIssues    []domain.AccountDiscoveryIssue
	SyncedAt           string
}

type accountSnapshotFailure struct {
	reason string
	cause  error
}

func (err *accountSnapshotFailure) Error() string                      { return "authorization account snapshot failed" }
func (err *accountSnapshotFailure) Unwrap() error                      { return err.cause }
func (err *accountSnapshotFailure) AuthorizationFailureReason() string { return err.reason }

func accountSyncFailure(reason string, err error) error {
	if err == nil {
		return nil
	}
	var classified authorizationFailureReason
	if errors.As(err, &classified) && allowedAuthorizationFailureReason(classified.AuthorizationFailureReason()) {
		return err
	}
	return &accountSnapshotFailure{reason: reason, cause: err}
}

func (syncer AdvertiserSnapshotSync) Sync(
	ctx context.Context,
	query AdvertiserSyncQuery,
) (AdvertiserSyncResult, error) {
	if syncer.Tokens == nil || syncer.Authorizations == nil || syncer.Discovery == nil {
		return AdvertiserSyncResult{}, errors.New("advertiser snapshot sync dependencies are incomplete")
	}
	lease, err := syncer.Tokens.Ensure(ctx, TokenQuery{
		Channel: query.Channel, AuthorizationID: query.AuthorizationID,
		AuthAccountID: query.AuthAccountID, AllowPending: true,
	})
	if err != nil {
		return AdvertiserSyncResult{}, err
	}
	return syncer.syncLease(ctx, lease, query.RebindExisting)
}

// syncLease is shared by a newly saved grant and explicit account sync. It does
// not acquire a token lock; the caller either owns it or obtained a lease from
// TokenManager. Activation rejects an obsolete credential revision in both cases.
func (syncer AdvertiserSnapshotSync) syncLease(ctx context.Context, lease TokenLease, rebindExisting bool) (AdvertiserSyncResult, error) {
	if syncer.Authorizations == nil || syncer.Discovery == nil || lease.TokenRevision < 1 {
		return AdvertiserSyncResult{}, errors.New("account synchronization dependencies are incomplete")
	}
	requestContext, err := WithTokenLease(ctx, lease)
	if err != nil {
		return AdvertiserSyncResult{}, err
	}
	snapshot, err := syncer.Discovery.Discover(requestContext, lease.Channel, lease.AccessToken)
	if err != nil {
		return AdvertiserSyncResult{}, fmt.Errorf("discover complete advertiser snapshot: %w", accountSyncFailure("ACCOUNT_DISCOVERY_FAILED", err))
	}
	if err := validateAdvertiserSnapshot(snapshot); err != nil {
		return AdvertiserSyncResult{}, accountSyncFailure("ACCOUNT_SNAPSHOT_INVALID", err)
	}
	now := time.Now().UTC()
	if syncer.Now != nil {
		now = syncer.Now().UTC()
	}
	syncedAt := now.Format(time.RFC3339Nano)
	err = syncer.Authorizations.UpdateChannel(ctx, lease.Channel, func(candidate map[string]any) error {
		metadata := configuration.Object(configuration.Object(candidate["authorizations"])[lease.AuthorizationID])
		revision, err := positiveInteger(metadata["token_revision"], 1)
		if err != nil || revision != lease.TokenRevision {
			return errors.New("authorization credential changed during account discovery")
		}
		return activateAdvertiserSnapshot(
			candidate, lease.AuthorizationID, snapshot, rebindExisting, syncedAt,
		)
	})
	if err != nil {
		return AdvertiserSyncResult{}, fmt.Errorf("activate advertiser snapshot: %w", accountSyncFailure("ACCOUNT_ACTIVATION_FAILED", err))
	}
	return AdvertiserSyncResult{
		Channel: lease.Channel, AuthorizationID: lease.AuthorizationID,
		AuthorizedAccounts: len(snapshot.Accounts),
		AdvertiserIDs:      append([]string(nil), snapshot.AdvertiserIDs...),
		DiscoveryIssues:    append([]domain.AccountDiscoveryIssue(nil), snapshot.DiscoveryIssues...),
		SyncedAt:           syncedAt,
	}, nil
}

func activateAdvertiserSnapshot(
	state map[string]any,
	authorizationID string,
	snapshot domain.AdvertiserSnapshot,
	rebindExisting bool,
	syncedAt string,
) error {
	authorizations := configuration.Object(state["authorizations"])
	metadata, exists := authorizations[authorizationID]
	if !exists {
		return fmt.Errorf("unknown authorization %s", authorizationID)
	}
	target := configuration.Object(metadata)
	currentOwners := configuration.Object(state["account_index"])
	conflicts := map[string]string{}
	for _, account := range snapshot.Accounts {
		owner := strings.TrimSpace(fmt.Sprint(currentOwners[account.AccountID]))
		if owner != "" && owner != "<nil>" && owner != authorizationID {
			conflicts[account.AccountID] = owner
		}
	}
	if len(conflicts) != 0 && !rebindExisting {
		return accountSyncFailure("ACCOUNT_MAPPING_CONFLICT", fmt.Errorf("authorized accounts already belong to another authorization: %v", sortedConflictKeys(conflicts)))
	}
	if len(conflicts) != 0 {
		for accountID, owner := range conflicts {
			old := configuration.Object(authorizations[owner])
			old["authorized_accounts"] = removeAuthorizedAccount(old["authorized_accounts"], accountID)
		}
	}
	target["authorized_accounts"] = authorizedAccountMaps(snapshot.Accounts)
	target["pending_account_sync"] = false
	target["last_authorized_account_sync_at"] = syncedAt
	target["account_discovery_issues"] = discoveryIssueMaps(snapshot.DiscoveryIssues)
	if err := rebuildAuthorizationIndexes(state); err != nil {
		return err
	}
	state["generation"] = nextGeneration(state["generation"])
	return nil
}

func rebuildAuthorizationIndexes(state map[string]any) error {
	authorizations := configuration.Object(state["authorizations"])
	accountIndex := map[string]any{}
	advertiserIndex := map[string][]string{}
	authorizationIDs := make([]string, 0, len(authorizations))
	for authorizationID := range authorizations {
		authorizationIDs = append(authorizationIDs, authorizationID)
	}
	sort.Strings(authorizationIDs)
	for _, authorizationID := range authorizationIDs {
		metadata := configuration.Object(authorizations[authorizationID])
		advertiserIDs := []string{}
		for _, row := range objectList(metadata["authorized_accounts"]) {
			accountID := strings.TrimSpace(fmt.Sprint(row["account_id"]))
			if err := domain.ValidateDecimalID(accountID, "account_id"); err != nil {
				return err
			}
			if owner := strings.TrimSpace(fmt.Sprint(accountIndex[accountID])); owner != "" && owner != "<nil>" && owner != authorizationID {
				return fmt.Errorf("authorized account %s belongs to multiple authorizations", accountID)
			}
			accountIndex[accountID] = authorizationID
			for _, advertiserID := range stringList(row["advertiser_ids"]) {
				if err := domain.ValidateDecimalID(advertiserID, "advertiser_id"); err != nil {
					return err
				}
				advertiserIDs = appendUnique(advertiserIDs, advertiserID)
			}
		}
		metadata["advertiser_ids"] = stringsToAny(advertiserIDs)
		for _, advertiserID := range advertiserIDs {
			advertiserIndex[advertiserID] = appendUnique(advertiserIndex[advertiserID], authorizationID)
		}
	}
	state["account_index"] = accountIndex
	indexedAdvertisers := map[string]any{}
	for advertiserID, owners := range advertiserIndex {
		indexedAdvertisers[advertiserID] = stringsToAny(owners)
	}
	state["advertiser_index"] = indexedAdvertisers
	return nil
}

func validateAdvertiserSnapshot(snapshot domain.AdvertiserSnapshot) error {
	accountIDs, advertiserIDs := map[string]struct{}{}, map[string]struct{}{}
	for _, account := range snapshot.Accounts {
		if err := domain.ValidateDecimalID(account.AccountID, "account_id"); err != nil {
			return err
		}
		if _, exists := accountIDs[account.AccountID]; exists {
			return fmt.Errorf("authorized account is duplicated: %s", account.AccountID)
		}
		accountIDs[account.AccountID] = struct{}{}
		for _, advertiserID := range account.AdvertiserIDs {
			if err := domain.ValidateDecimalID(advertiserID, "advertiser_id"); err != nil {
				return err
			}
			advertiserIDs[advertiserID] = struct{}{}
		}
	}
	if len(snapshot.AdvertiserIDs) != len(advertiserIDs) {
		return errors.New("advertiser snapshot top-level IDs do not match account mappings")
	}
	for _, advertiserID := range snapshot.AdvertiserIDs {
		if _, exists := advertiserIDs[advertiserID]; !exists {
			return fmt.Errorf("advertiser snapshot contains unmapped ID %s", advertiserID)
		}
	}
	return nil
}

func authorizedAccountMaps(accounts []domain.AuthorizedAccount) []any {
	rows := make([]any, 0, len(accounts))
	for _, account := range accounts {
		row := map[string]any{
			"account_id": account.AccountID, "advertiser_ids": stringsToAny(account.AdvertiserIDs),
		}
		optionalString(row, "account_string_id", account.AccountStringID)
		optionalString(row, "shop_id", account.ShopID)
		optionalString(row, "account_name", account.AccountName)
		optionalString(row, "account_role", account.AccountRole)
		optionalString(row, "account_type", account.AccountType)
		optionalString(row, "advertiser_name", account.AdvertiserName)
		if account.IsValid != nil {
			row["is_valid"] = *account.IsValid
		}
		rows = append(rows, row)
	}
	return rows
}

func discoveryIssueMaps(issues []domain.AccountDiscoveryIssue) []any {
	rows := make([]any, 0, len(issues))
	for _, issue := range issues {
		rows = append(rows, map[string]any{
			"account_id": issue.AccountID, "role": issue.Role,
			"code": issue.Code, "reason": issue.Reason,
		})
	}
	return rows
}

func removeAuthorizedAccount(value any, accountID string) []any {
	rows := []any{}
	for _, row := range objectList(value) {
		if strings.TrimSpace(fmt.Sprint(row["account_id"])) != accountID {
			rows = append(rows, row)
		}
	}
	return rows
}

func objectList(value any) []map[string]any {
	result := []map[string]any{}
	for _, item := range anyList(value) {
		if row, ok := item.(map[string]any); ok && row != nil {
			result = append(result, row)
		}
	}
	return result
}

func anyList(value any) []any {
	switch values := value.(type) {
	case []any:
		return values
	case nil:
		return nil
	default:
		return nil
	}
}

func stringList(value any) []string {
	result := []string{}
	for _, item := range anyList(value) {
		text := strings.TrimSpace(fmt.Sprint(item))
		if text != "" && text != "<nil>" {
			result = append(result, text)
		}
	}
	if values, ok := value.([]string); ok {
		result = append(result, values...)
	}
	return result
}

func stringsToAny(values []string) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func optionalString(row map[string]any, key, value string) {
	if strings.TrimSpace(value) != "" {
		row[key] = value
	}
}

func sortedConflictKeys(conflicts map[string]string) []string {
	keys := make([]string, 0, len(conflicts))
	for key := range conflicts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
