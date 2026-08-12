package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type AccountStore struct {
	Path        string
	LockTimeout time.Duration
}

func (s AccountStore) Read(ctx context.Context) (domain.AccountBook, error) {
	select {
	case <-ctx.Done():
		return domain.AccountBook{}, ctx.Err()
	default:
	}
	raw, err := readJSON(s.Path)
	if err != nil {
		return domain.AccountBook{}, err
	}
	return decodeAccountBook(raw)
}

func (s AccountStore) Update(ctx context.Context, update func(*domain.AccountBook) error) (any, error) {
	store := ConfigStore(s)
	return store.Update(ctx, func(raw map[string]any) (any, bool, error) {
		book, err := decodeAccountBook(raw)
		if err != nil {
			return nil, false, err
		}
		if err := update(&book); err != nil {
			return nil, false, err
		}
		if err := book.Validate(); err != nil {
			return nil, false, err
		}
		if err := encodeAccountBook(raw, book); err != nil {
			return nil, false, err
		}
		return book, true, nil
	})
}

func decodeAccountBook(raw map[string]any) (domain.AccountBook, error) {
	book := domain.NewAccountBook()
	if value, ok := raw["managed_account_schema_version"]; ok {
		version, err := numberInt(value)
		if err != nil {
			return domain.AccountBook{}, errors.New("managed_account_schema_version must be an integer")
		}
		book.SchemaVersion = version
	}
	value, ok := raw["managed_accounts"]
	if !ok || value == nil {
		return book, nil
	}
	groups, ok := value.(map[string]any)
	if !ok {
		return domain.AccountBook{}, errors.New("managed_accounts must be an object grouped by channel")
	}
	for configuredChannel, groupValue := range groups {
		channel, err := domain.ParseChannel(configuredChannel)
		if err != nil {
			return domain.AccountBook{}, err
		}
		items, ok := groupValue.([]any)
		if !ok {
			return domain.AccountBook{}, fmt.Errorf("managed_accounts.%s must be a list", configuredChannel)
		}
		for _, item := range items {
			record, ok := item.(map[string]any)
			if !ok {
				return domain.AccountBook{}, fmt.Errorf("managed_accounts.%s contains an invalid record", configuredChannel)
			}
			enabled := true
			if rawEnabled, exists := record["enabled"]; exists {
				enabled, err = boolValue(rawEnabled)
				if err != nil {
					return domain.AccountBook{}, errors.New("managed account enabled must be a boolean")
				}
			}
			account := domain.ManagedAccount{
				Channel:       channel,
				AdvertiserID:  stringValue(record["advertiser_id"]),
				Name:          stringValue(record["name"]),
				Enabled:       enabled,
				AuthAccountID: stringValue(record["auth_account_id"]),
			}
			if err := domain.ValidateAccount(account); err != nil {
				return domain.AccountBook{}, err
			}
			book.Accounts[channel] = append(book.Accounts[channel], account)
		}
	}
	if err := book.Validate(); err != nil {
		return domain.AccountBook{}, err
	}
	return book, nil
}

func encodeAccountBook(raw map[string]any, after domain.AccountBook) error {
	raw["managed_account_schema_version"] = after.SchemaVersion
	groups := map[string]any{}
	for _, channel := range []domain.Channel{domain.Marketing, domain.Qianchuan} {
		oldRecords := rawRecords(raw, channel)
		oldByID := map[string]map[string]any{}
		for _, record := range oldRecords {
			if id := stringValue(record["advertiser_id"]); id != "" {
				oldByID[id] = record
			}
		}
		newRecords := make([]any, 0, len(after.Accounts[channel]))
		for _, account := range after.Accounts[channel] {
			record := map[string]any{}
			for key, value := range oldByID[account.AdvertiserID] {
				record[key] = value
			}
			record["advertiser_id"] = account.AdvertiserID
			record["name"] = account.Name
			record["enabled"] = account.Enabled
			if account.AuthAccountID == "" {
				delete(record, "auth_account_id")
			} else {
				record["auth_account_id"] = account.AuthAccountID
			}
			newRecords = append(newRecords, record)
		}
		groups[string(channel)] = newRecords
	}
	raw["managed_accounts"] = groups
	return nil
}

func rawRecords(raw map[string]any, channel domain.Channel) []map[string]any {
	groups, _ := raw["managed_accounts"].(map[string]any)
	items, _ := groups[string(channel)].([]any)
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if record, ok := item.(map[string]any); ok {
			result = append(result, record)
		}
	}
	return result
}

func numberInt(value any) (int, error) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := strconv.Atoi(string(typed))
		return parsed, err
	case float64:
		return int(typed), nil
	case int:
		return typed, nil
	default:
		return 0, errors.New("not an integer")
	}
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func boolValue(value any) (bool, error) {
	result, ok := value.(bool)
	if !ok {
		return false, errors.New("not a boolean")
	}
	return result, nil
}
