package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	applicationtemplates "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/templates"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
)

func RunTemplatesInteractive(
	ctx context.Context,
	action string,
	args []string,
	store applicationtemplates.ConfigStore,
	authorizations authorizationReader,
	resolvedPath string,
	stdin io.Reader,
	stdout io.Writer,
) int {
	if action != "create" {
		return RunTemplates(ctx, action, args, store, resolvedPath, stdout)
	}
	options, err := parseTemplateOptions(action, args)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
		return 2
	}
	reader := newPromptReader(stdin, stdout)
	states := map[string]domain.AuthorizationState{}
	channel := options.channel
	if channel == "" {
		for _, candidate := range []string{"marketing", "qianchuan"} {
			state, readErr := authorizations.ReadChannel(ctx, candidate)
			if readErr != nil {
				return writeTemplateCreateError(stdout, readErr)
			}
			states[candidate] = state
		}
		channel, err = selectTemplateChannel(reader, states)
		if err != nil {
			return writeTemplateCreateError(stdout, unexpectedInput(err))
		}
	}
	state, exists := states[channel]
	if !exists {
		state, err = authorizations.ReadChannel(ctx, channel)
		if err != nil {
			return writeTemplateCreateError(stdout, err)
		}
	}
	if channel == "marketing" {
		materialSourceType := options.materialSourceType
		if materialSourceType == "" {
			materialSourceType, err = selectMarketingMaterialSource(reader)
			if err != nil {
				return writeTemplateCreateError(stdout, unexpectedInput(err))
			}
		}
		if err := runMarketingTemplateWizard(ctx, store, resolvedPath, reader, state, materialSourceType); err != nil {
			return writeTemplateCreateError(stdout, unexpectedInput(err))
		}
		return 0
	}
	templateType := options.templateType
	if templateType == "" {
		templateType, err = selectQianchuanTemplateType(reader)
		if err != nil {
			return writeTemplateCreateError(stdout, unexpectedInput(err))
		}
	}
	if templateType == "live" {
		err = runQianchuanLiveTemplateWizard(ctx, store, resolvedPath, reader, state)
	} else {
		err = runQianchuanProductTemplateWizard(ctx, store, reader, state)
	}
	if err != nil {
		return writeTemplateCreateError(stdout, unexpectedInput(err))
	}
	return 0
}

func RunQianchuanTemplatesInteractive(
	ctx context.Context,
	action string,
	args []string,
	store applicationtemplates.ConfigStore,
	authorizations authorizationReader,
	resolvedPath string,
	stdin io.Reader,
	stdout io.Writer,
) int {
	if action != "create" && action != "create-live" {
		return RunQianchuanTemplates(ctx, action, args, store, resolvedPath, stdout)
	}
	if _, err := parseQianchuanTemplateOptions(action, args); err != nil {
		WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
		return 2
	}
	state, err := authorizations.ReadChannel(ctx, "qianchuan")
	if err != nil {
		return writeTemplateCreateError(stdout, err)
	}
	reader := newPromptReader(stdin, stdout)
	if action == "create-live" {
		err = runQianchuanLiveTemplateWizard(ctx, store, resolvedPath, reader, state)
	} else {
		err = runQianchuanProductTemplateWizard(ctx, store, reader, state)
	}
	if err != nil {
		return writeTemplateCreateError(stdout, unexpectedInput(err))
	}
	return 0
}

func selectTemplateChannel(reader *promptReader, states map[string]domain.AuthorizationState) (string, error) {
	_, _ = fmt.Fprintln(reader.output, "创建投放模板渠道：")
	channels := []string{"marketing", "qianchuan"}
	for index, channel := range channels {
		_, _ = fmt.Fprintf(reader.output, "  %d. %s（%s）\n", index, channelDisplayName(channel), authorizationLabel(states[channel]))
	}
	for {
		value, err := reader.line("请选择渠道编号: ")
		if err != nil {
			return "", err
		}
		if value == "0" || value == "1" {
			if value == "0" {
				return "marketing", nil
			}
			return "qianchuan", nil
		}
	}
}

func selectMarketingMaterialSource(reader *promptReader) (string, error) {
	_, _ = fmt.Fprintln(reader.output, "巨量营销素材模式：")
	_, _ = fmt.Fprintln(reader.output, "  0. 混剪素材（账户上传）")
	_, _ = fmt.Fprintln(reader.output, "  1. 原生素材（达人授权）")
	for {
		value, err := reader.line("请选择素材模式编号: ")
		if err != nil {
			return "", err
		}
		if value == "0" {
			return "ACCOUNT_UPLOAD", nil
		}
		if value == "1" {
			return "CREATOR_AUTHORIZED", nil
		}
	}
}

func selectQianchuanTemplateType(reader *promptReader) (string, error) {
	_, _ = fmt.Fprintln(reader.output, "巨量千川模板类型：")
	_, _ = fmt.Fprintln(reader.output, "  0. 商品全域")
	_, _ = fmt.Fprintln(reader.output, "  1. 直播全域")
	for {
		value, err := reader.line("请选择模板类型编号: ")
		if err != nil {
			return "", err
		}
		if value == "0" {
			return "product", nil
		}
		if value == "1" {
			return "live", nil
		}
	}
}

func writeTemplateCreateError(stdout io.Writer, err error) int {
	mapped := templateError(err)
	if errors.Is(err, context.Canceled) {
		mapped = domain.NewError("interrupted", "operation interrupted", 130, nil)
	}
	WriteDomainError(stdout, mapped)
	return mapped.ExitCode
}

func generateTemplateID(prefix string) (string, error) {
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate template ID: %w", err)
	}
	return prefix + hex.EncodeToString(random), nil
}

func orderedVerification(value map[string]any) orderedObject {
	return orderedMap(value, []string{"channel", "status", "authorized_advertiser_count", "reason"}, nil)
}

func orderedMaterialStrategy(value map[string]any) orderedObject {
	return orderedMap(value, []string{"source_type", "selection_mode", "max_materials_per_unit", "creator_filters", "persist_material_ids"}, func(key string, item any) any {
		if key == "creator_filters" {
			return orderedMap(mapValue(item), []string{"creator_ids", "auth_types", "authorization_status", "minimum_remaining_days"}, nil)
		}
		return item
	})
}

func orderedQianchuanTemplate(candidate map[string]any, live bool) orderedObject {
	keys := []string{"template_id", "display_name", "template_type", "status", "bindings", "delivery_setting"}
	if live {
		keys = append(keys, "creative_setting")
	}
	keys = append(keys, "material_strategy")
	return orderedMap(candidate, keys, func(key string, item any) any {
		switch key {
		case "bindings":
			bindingKeys := []string{"channel", "advertiser_id", "product_name", "product_ids"}
			if live {
				bindingKeys = []string{"channel", "advertiser_id", "creator_name", "aweme_id"}
			}
			return orderedMap(mapValue(item), bindingKeys, nil)
		case "delivery_setting":
			deliveryKeys := []string{"smart_bid_type", "roi2_goal", "qcpx_mode", "budget", "video_schedule_type", "deep_external_action"}
			if live {
				deliveryKeys = []string{"smart_bid_type", "budget", "live_schedule_type", "roi2_goal", "daily_delivery_time", "deep_external_action", "qcpx_mode"}
			}
			return orderedMap(mapValue(item), deliveryKeys, nil)
		case "creative_setting":
			return orderedMap(mapValue(item), []string{"smart_select_material"}, nil)
		case "material_strategy":
			return orderedMaterialStrategy(mapValue(item))
		default:
			return item
		}
	})
}
