package domain

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	ManagedAccountSchemaVersion = 1
	MaxDecimalID                = "9223372036854775807"
)

var decimalIDPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

type Channel string

const (
	Marketing Channel = "marketing"
	Qianchuan Channel = "qianchuan"
)

var channelOrder = []Channel{Marketing, Qianchuan}

func ParseChannel(value string) (Channel, error) {
	channel := Channel(strings.ToLower(strings.TrimSpace(value)))
	if channel != Marketing && channel != Qianchuan {
		return "", fmt.Errorf("unknown channel: %s", value)
	}
	return channel, nil
}

func (c Channel) DisplayName() string {
	if c == Qianchuan {
		return "巨量千川"
	}
	return "巨量营销"
}

type ManagedAccount struct {
	Channel       Channel `json:"channel"`
	AdvertiserID  string  `json:"advertiser_id"`
	Name          string  `json:"name"`
	Enabled       bool    `json:"enabled"`
	AuthAccountID string  `json:"auth_account_id,omitempty"`
}

type AccountBook struct {
	SchemaVersion int
	Accounts      map[Channel][]ManagedAccount
}

func NewAccountBook() AccountBook {
	return AccountBook{
		SchemaVersion: ManagedAccountSchemaVersion,
		Accounts: map[Channel][]ManagedAccount{
			Marketing: {},
			Qianchuan: {},
		},
	}
}

func ValidateDecimalID(value, field string) error {
	if !decimalIDPattern.MatchString(value) || len(value) > len(MaxDecimalID) ||
		(len(value) == len(MaxDecimalID) && value > MaxDecimalID) {
		return fmt.Errorf("%s must be a canonical positive decimal ID not exceeding %s", field, MaxDecimalID)
	}
	return nil
}

func ValidateAccount(account ManagedAccount) error {
	if _, err := ParseChannel(string(account.Channel)); err != nil {
		return err
	}
	if err := ValidateDecimalID(account.AdvertiserID, "advertiser_id"); err != nil {
		return err
	}
	account.Name = strings.TrimSpace(account.Name)
	if account.Name == "" || len([]rune(account.Name)) > 100 {
		return errors.New("managed account name must contain 1 to 100 characters")
	}
	if account.AuthAccountID != "" {
		if err := ValidateDecimalID(account.AuthAccountID, "auth_account_id"); err != nil {
			return err
		}
	}
	return nil
}

func (book AccountBook) Validate() error {
	if book.SchemaVersion > ManagedAccountSchemaVersion {
		return fmt.Errorf("managed account schema %d is newer than supported %d", book.SchemaVersion, ManagedAccountSchemaVersion)
	}
	seen := map[string]struct{}{}
	for _, channel := range channelOrder {
		for _, account := range book.Accounts[channel] {
			if account.Channel != channel {
				return errors.New("managed account channel does not match its group")
			}
			if err := ValidateAccount(account); err != nil {
				return err
			}
			key := string(channel) + ":" + account.AdvertiserID
			if _, exists := seen[key]; exists {
				return fmt.Errorf("managed account is duplicated: %s", key)
			}
			seen[key] = struct{}{}
		}
	}
	return nil
}

func (book AccountBook) List(channel *Channel, enabledOnly bool) []ManagedAccount {
	selected := channelOrder
	if channel != nil {
		selected = []Channel{*channel}
	}
	result := []ManagedAccount{}
	for _, itemChannel := range selected {
		for _, account := range book.Accounts[itemChannel] {
			if enabledOnly && !account.Enabled {
				continue
			}
			result = append(result, account)
		}
	}
	return result
}

func (book *AccountBook) Upsert(account ManagedAccount) (ManagedAccount, bool, error) {
	account.Name = strings.TrimSpace(account.Name)
	if err := ValidateAccount(account); err != nil {
		return ManagedAccount{}, false, err
	}
	items := book.Accounts[account.Channel]
	for index, current := range items {
		if current.AdvertiserID == account.AdvertiserID {
			items[index] = account
			book.Accounts[account.Channel] = items
			return account, false, nil
		}
	}
	book.Accounts[account.Channel] = append(items, account)
	return account, true, nil
}

func (book *AccountBook) Remove(channel Channel, advertiserID string) (ManagedAccount, error) {
	if err := ValidateDecimalID(advertiserID, "advertiser_id"); err != nil {
		return ManagedAccount{}, err
	}
	items := book.Accounts[channel]
	for index, account := range items {
		if account.AdvertiserID == advertiserID {
			book.Accounts[channel] = append(items[:index], items[index+1:]...)
			return account, nil
		}
	}
	return ManagedAccount{}, errors.New("managed account was not found")
}

func (book *AccountBook) SetEnabled(channel Channel, advertiserID string, enabled bool) (ManagedAccount, error) {
	if err := ValidateDecimalID(advertiserID, "advertiser_id"); err != nil {
		return ManagedAccount{}, err
	}
	for index, account := range book.Accounts[channel] {
		if account.AdvertiserID == advertiserID {
			account.Enabled = enabled
			book.Accounts[channel][index] = account
			return account, nil
		}
	}
	return ManagedAccount{}, errors.New("managed account was not found")
}

var ManagedAccountColumns = []PresentationColumn{
	{Field: "channel_name", Label: "渠道"},
	{Field: "name", Label: "账户名称"},
	{Field: "advertiser_id", Label: "广告主 ID"},
	{Field: "enabled_label", Label: "启用状态"},
}

func ManagedAccountPresentation(accounts []ManagedAccount, includeDisabled bool) Presentation {
	rows := make([]map[string]any, 0, len(accounts))
	for _, account := range accounts {
		enabledLabel := "已停用"
		if account.Enabled {
			enabledLabel = "已启用"
		}
		rows = append(rows, map[string]any{
			"channel_name": account.Channel.DisplayName(),
			"name":         account.Name, "advertiser_id": account.AdvertiserID,
			"enabled_label": enabledLabel,
		})
	}
	scope := "仅展示已启用账户"
	if includeDisabled {
		scope = "包含已停用账户"
	}
	return Presentation{
		Format:                "markdown",
		Required:              true,
		AllowColumnOmission:   false,
		AllowColumnReordering: false,
		Columns:               ManagedAccountColumns,
		RenderedMarkdown: fmt.Sprintf(
			"**负责账户：** 共 %d 个；%s\n\n%s",
			len(rows), scope, RenderMarkdownTable(ManagedAccountColumns, rows),
		),
	}
}

func SortedAccountKeys(book AccountBook) []string {
	keys := make([]string, 0)
	for channel, accounts := range book.Accounts {
		for _, account := range accounts {
			keys = append(keys, string(channel)+":"+account.AdvertiserID)
		}
	}
	sort.Strings(keys)
	return keys
}
