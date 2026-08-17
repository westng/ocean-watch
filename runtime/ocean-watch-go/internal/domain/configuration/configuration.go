package configuration

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	SchemaVersion  = 2
	DefaultChannel = "marketing"
)

var (
	sensitiveKeys = map[string]bool{
		"app_id": true, "developer_id": true, "secret": true,
		"auth_code":    true,
		"access_token": true, "refresh_token": true,
		"access_token_expires_at": true, "refresh_token_expires_at": true,
		"last_token_update_at": true, "last_authorized_account_sync_at": true,
		"oauth_authorized_accounts": true, "authorized_advertiser_ids": true,
	}
	unresolvedTemplate = regexp.MustCompile(`\{+[A-Za-z_][A-Za-z0-9_]*\}+`)
)

type Channel struct {
	ID              string
	DisplayName     string
	BusinessBaseURL string
	AuthorizeURL    string
	TokenBaseURL    string
	RedirectURI     string
	Capabilities    map[string]bool
}

var Channels = map[string]Channel{
	"marketing": {
		ID: "marketing", DisplayName: "巨量营销",
		BusinessBaseURL: "https://api.oceanengine.com/open_api",
		AuthorizeURL:    "https://ad.oceanengine.com/openapi/audit/oauth.html",
		TokenBaseURL:    "https://ad.oceanengine.com/open_api",
		RedirectURI:     "http://127.0.0.1:8787/oauth/callback",
		Capabilities:    map[string]bool{"oauth": true, "accounts": true, "create": true, "query": true, "report": true},
	},
	"qianchuan": {
		ID: "qianchuan", DisplayName: "巨量千川",
		BusinessBaseURL: "https://api.oceanengine.com/open_api",
		AuthorizeURL:    "https://qianchuan.jinritemai.com/openapi/qc/audit/oauth.html",
		TokenBaseURL:    "https://ad.oceanengine.com/open_api",
		RedirectURI:     "http://127.0.0.1:8787/oauth/callback",
		Capabilities: map[string]bool{
			"oauth": true, "accounts": true, "qianchuan_create": true,
			"qianchuan_materials": true, "qianchuan_report": true,
		},
	},
}

type ChannelError struct {
	Code    string
	Channel string
	Message string
}

func (err *ChannelError) Error() string { return err.Message }

func Current(raw map[string]any) (map[string]any, error) {
	current := CloneMap(raw)
	version, exists := current["config_schema_version"]
	if !exists {
		return nil, errors.New("config_schema_version is required; only schema 2 is supported")
	}
	parsed, err := Integer(version)
	if err != nil {
		return nil, errors.New("config_schema_version must be an integer")
	}
	if parsed != SchemaVersion {
		return nil, fmt.Errorf("config schema %d is unsupported; only schema %d is supported", parsed, SchemaVersion)
	}
	for _, key := range []string{"api", "oauth"} {
		if _, removedRoot := current[key]; removedRoot {
			return nil, fmt.Errorf("root-level %s configuration is unsupported; use channels.<channel>.%s", key, key)
		}
	}
	channels, ok := current["channels"].(map[string]any)
	if !ok {
		return nil, errors.New("channels must be an object")
	}
	for channelID, rawChannel := range channels {
		channel, ok := rawChannel.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("channels.%s must be an object", channelID)
		}
		for _, key := range []string{"api", "oauth"} {
			if value, exists := channel[key]; exists && value != nil {
				if _, ok := value.(map[string]any); !ok {
					return nil, fmt.Errorf("channels.%s.%s must be an object", channelID, key)
				}
			}
		}
	}
	if account, exists := current["account"]; exists && account != nil {
		if _, ok := account.(map[string]any); !ok {
			return nil, errors.New("account must be an object")
		}
	}
	if fields := SensitiveFields(current); len(fields) != 0 {
		return nil, fmt.Errorf("credentials are unsupported in project configuration: %s", strings.Join(fields, ", "))
	}
	return current, nil
}

func Runtime(raw map[string]any, explicit, capability string) (map[string]any, Channel, error) {
	current, err := Current(raw)
	if err != nil {
		return nil, Channel{}, err
	}
	selected := SelectedChannel(current, explicit)
	definition, err := GetChannel(selected, capability)
	if err != nil {
		return nil, Channel{}, err
	}
	configuredRaw, exists := Object(current["channels"])[selected]
	if !exists {
		return nil, Channel{}, fmt.Errorf("channel configuration is missing: %s", selected)
	}
	configured := Object(configuredRaw)
	runtimeConfig := CloneMap(current)
	runtimeAPI := CloneMap(Object(configured["api"]))
	runtimeOAuth := CloneMap(Object(configured["oauth"]))
	setDefault(runtimeAPI, "base_url", definition.BusinessBaseURL)
	setDefault(runtimeOAuth, "authorize_url", definition.AuthorizeURL)
	setDefault(runtimeOAuth, "token_base_url", definition.TokenBaseURL)
	setDefault(runtimeOAuth, "redirect_uri", definition.RedirectURI)
	runtimeConfig["api"] = runtimeAPI
	runtimeConfig["oauth"] = runtimeOAuth
	account, _ := objectOrCreate(runtimeConfig, "account")
	account["channel"] = selected
	runtimeConfig["_channel"] = map[string]any{"id": selected, "display_name": definition.DisplayName}
	return runtimeConfig, definition, nil
}

func GetChannel(channel, capability string) (Channel, error) {
	normalized := strings.ToLower(strings.TrimSpace(channel))
	if normalized == "" {
		normalized = DefaultChannel
	}
	definition, ok := Channels[normalized]
	if !ok {
		return Channel{}, &ChannelError{Code: "unknown_channel", Channel: normalized, Message: "unknown channel: " + normalized}
	}
	if capability != "" && !definition.Capabilities[capability] {
		return Channel{}, &ChannelError{
			Code: "channel_capability_not_implemented", Channel: normalized,
			Message: fmt.Sprintf("channel %s does not implement %s", definition.DisplayName, capability),
		}
	}
	return definition, nil
}

func SelectedChannel(config map[string]any, explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return strings.ToLower(strings.TrimSpace(explicit))
	}
	account := Object(config["account"])
	for _, value := range []any{account["channel"], config["default_channel"]} {
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" && text != "<nil>" {
			return strings.ToLower(text)
		}
	}
	return DefaultChannel
}

func SensitiveFields(config map[string]any) []string {
	fields := []string{}
	for key := range sensitiveKeys {
		if _, exists := Object(config["api"])[key]; exists {
			fields = append(fields, "api."+key)
		}
	}
	for channel, raw := range Object(config["channels"]) {
		api := Object(Object(raw)["api"])
		for key := range sensitiveKeys {
			if _, exists := api[key]; exists {
				fields = append(fields, "channels."+channel+".api."+key)
			}
		}
	}
	sort.Strings(fields)
	return fields
}

func SensitiveKeyNames() []string {
	result := make([]string, 0, len(sensitiveKeys))
	for key := range sensitiveKeys {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func Value(config map[string]any, path string) any {
	var current any = config
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[part]
	}
	return current
}

func Missing(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		trimmed := strings.TrimSpace(typed)
		return trimmed == "" || strings.HasPrefix(trimmed, "REPLACE_WITH")
	case []any:
		return len(typed) == 0
	case []string:
		return len(typed) == 0
	default:
		return false
	}
}

func ContainsUnresolved(value any) bool {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		lower := strings.ToLower(trimmed)
		if Missing(typed) || strings.Contains(lower, "example.com") || unresolvedTemplate.MatchString(trimmed) {
			return true
		}
		for _, marker := range []string{"replace_with", "todo", "待填", "待反查"} {
			if strings.Contains(lower, marker) {
				return true
			}
		}
		return false
	case []any:
		if len(typed) == 0 {
			return true
		}
		for _, item := range typed {
			if ContainsUnresolved(item) {
				return true
			}
		}
		return false
	case map[string]any:
		for _, item := range typed {
			if ContainsUnresolved(item) {
				return true
			}
		}
		return false
	case nil:
		return true
	default:
		return false
	}
}

func Integer(value any) (int, error) {
	switch typed := value.(type) {
	case int:
		return typed, nil
	case int64:
		return int(typed), nil
	case json.Number:
		parsed, err := strconv.Atoi(string(typed))
		return parsed, err
	case float64:
		return int(typed), nil
	case string:
		return strconv.Atoi(strings.TrimSpace(typed))
	default:
		return 0, fmt.Errorf("value is not an integer")
	}
}

func CloneMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return Clone(value).(map[string]any)
}

func Clone(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = Clone(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = Clone(item)
		}
		return result
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}

func Object(value any) map[string]any {
	result, _ := value.(map[string]any)
	if result == nil {
		return map[string]any{}
	}
	return result
}

func List(value any) []any {
	result, _ := value.([]any)
	return result
}

func Equal(left, right any) bool {
	leftPayload, leftErr := json.Marshal(left)
	rightPayload, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftPayload) == string(rightPayload)
}

func objectOrCreate(parent map[string]any, key string) (map[string]any, error) {
	if existing, exists := parent[key]; exists && existing != nil {
		object, ok := existing.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s must be an object", key)
		}
		return object, nil
	}
	created := map[string]any{}
	parent[key] = created
	return created, nil
}

func setDefault(target map[string]any, key string, value any) {
	if _, exists := target[key]; !exists {
		target[key] = value
	}
}
