package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type authorizationReader interface {
	ReadChannel(context.Context, string) (domain.AuthorizationState, error)
}

func promptAdvertiserID(
	reader *promptReader,
	channel string,
	candidates []any,
	state domain.AuthorizationState,
) (string, map[string]any, error) {
	advertiserIDs := append([]string(nil), state.AdvertiserIDs...)
	sort.Slice(advertiserIDs, func(left, right int) bool {
		if len(advertiserIDs[left]) != len(advertiserIDs[right]) {
			return len(advertiserIDs[left]) < len(advertiserIDs[right])
		}
		return advertiserIDs[left] < advertiserIDs[right]
	})
	authorized := make(map[string]bool, len(advertiserIDs))
	for _, advertiserID := range advertiserIDs {
		authorized[advertiserID] = true
	}
	defaultValue := firstAdvertiserCandidate(candidates)
	if len(authorized) != 0 && !authorized[defaultValue] {
		defaultValue = ""
	}
	if defaultValue == "" && len(advertiserIDs) == 1 {
		defaultValue = advertiserIDs[0]
	}
	displayName := channelDisplayName(channel)
	switch {
	case len(advertiserIDs) != 0:
		_, _ = fmt.Fprintf(reader.output, "当前%s授权覆盖 %d 个广告主，请输入模板要绑定的广告主 ID。\n", displayName, len(advertiserIDs))
	case state.AuthorizationCount != 0:
		_, _ = fmt.Fprintf(reader.output, "当前%s授权尚未同步出可用广告主；可以先创建模板，真实投放前必须重新校验。\n", displayName)
	default:
		_, _ = fmt.Fprintf(reader.output, "当前%s尚未授权；可以先创建模板，真实投放前必须完成授权并校验广告主。\n", displayName)
	}
	for {
		value, err := reader.value("广告主 ID", defaultValue, true)
		if err != nil {
			return "", nil, err
		}
		if !positiveDecimalID(value) {
			_, _ = fmt.Fprintln(reader.output, "广告主 ID 必须是正整数，请重新输入。")
			continue
		}
		if len(authorized) != 0 && !authorized[value] {
			_, _ = fmt.Fprintf(reader.output, "广告主 %s 不在当前%s授权范围内，请重新输入。\n", value, displayName)
			continue
		}
		status := "UNVERIFIED"
		var reason any = "CHANNEL_NOT_AUTHORIZED"
		if len(authorized) != 0 {
			status = "VERIFIED"
			reason = nil
		} else if state.AuthorizationCount != 0 {
			reason = "ADVERTISER_SYNC_EMPTY"
		}
		return value, map[string]any{
			"channel": channel, "status": status,
			"authorized_advertiser_count": len(advertiserIDs), "reason": reason,
		}, nil
	}
}

func firstAdvertiserCandidate(values []any) string {
	for _, value := range values {
		candidate := strings.TrimSpace(textValue(value))
		if strings.HasPrefix(candidate, "REPLACE_WITH") || !positiveDecimalID(candidate) {
			continue
		}
		return candidate
	}
	return ""
}

func positiveDecimalID(value string) bool {
	if value == "" {
		return false
	}
	nonzero := false
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
		if character != '0' {
			nonzero = true
		}
	}
	return nonzero
}

func channelDisplayName(channel string) string {
	if channel == "qianchuan" {
		return "巨量千川"
	}
	return "巨量营销"
}

func authorizationLabel(state domain.AuthorizationState) string {
	if state.AuthorizationCount != 0 {
		return fmt.Sprintf("已授权，%d 个广告主", len(state.AdvertiserIDs))
	}
	return "未授权，可先创建模板，投放前需授权"
}
