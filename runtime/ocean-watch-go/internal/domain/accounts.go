package domain

import (
	"encoding/json"
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
	StarMap   Channel = "star_map"
)

var channelOrder = []Channel{Marketing, Qianchuan, StarMap}

func ParseChannel(value string) (Channel, error) {
	channel := Channel(strings.ToLower(strings.TrimSpace(value)))
	if channel != Marketing && channel != Qianchuan && channel != StarMap {
		return "", fmt.Errorf("unknown channel: %s", value)
	}
	return channel, nil
}

func (c Channel) DisplayName() string {
	if c == Qianchuan {
		return "巨量千川"
	}
	if c == StarMap {
		return "巨量星图"
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

// AccountSubject describes the external account identity without assuming
// that every channel exposes an advertiser.
type AccountSubject struct {
	ID    string
	Kind  string
	Label string
}

func (c Channel) SubjectKind() string {
	if c == StarMap {
		return "star_account"
	}
	return "advertiser"
}

func (c Channel) SubjectIDLabel() string {
	if c == StarMap {
		return "星图账号 ID"
	}
	return "广告主 ID"
}

func (c Channel) SubjectKindLabel() string {
	if c == StarMap {
		return "星图账号"
	}
	return "广告主"
}

func (account ManagedAccount) Subject() AccountSubject {
	return AccountSubject{
		ID: account.AdvertiserID, Kind: account.Channel.SubjectKind(), Label: account.Channel.SubjectIDLabel(),
	}
}

// MarshalJSON keeps the legacy advertiser_id field while exposing the
// channel-neutral identity used by new consumers.
func (account ManagedAccount) MarshalJSON() ([]byte, error) {
	type accountJSON struct {
		Channel       Channel `json:"channel"`
		AdvertiserID  string  `json:"advertiser_id"`
		SubjectID     string  `json:"subject_id"`
		SubjectKind   string  `json:"subject_kind"`
		Name          string  `json:"name"`
		Enabled       bool    `json:"enabled"`
		AuthAccountID string  `json:"auth_account_id,omitempty"`
	}
	subject := account.Subject()
	return json.Marshal(accountJSON{
		Channel: account.Channel, AdvertiserID: account.AdvertiserID,
		SubjectID: subject.ID, SubjectKind: subject.Kind, Name: account.Name,
		Enabled: account.Enabled, AuthAccountID: account.AuthAccountID,
	})
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
			StarMap:   {},
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
	{Field: "subject_kind", Label: "账号类型"},
	{Field: "subject_id", Label: "账号 ID"},
	{Field: "enabled_label", Label: "启用状态"},
}

func ManagedAccountPresentation(accounts []ManagedAccount, includeDisabled bool) Presentation {
	rows := make([]map[string]any, 0, len(accounts))
	for _, account := range accounts {
		enabledLabel := "已停用"
		if account.Enabled {
			enabledLabel = "已启用"
		}
		subject := account.Subject()
		rows = append(rows, map[string]any{
			"channel_name": account.Channel.DisplayName(),
			"name":         account.Name, "subject_kind": account.Channel.SubjectKindLabel(), "subject_id": subject.ID,
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
