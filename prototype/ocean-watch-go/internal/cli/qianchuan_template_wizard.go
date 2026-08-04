package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	applicationtemplates "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/templates"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	domaintemplates "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/templates"
)

func runQianchuanProductTemplateWizard(
	ctx context.Context,
	store applicationtemplates.ConfigStore,
	reader *promptReader,
	state domain.AuthorizationState,
) error {
	session, err := (applicationtemplates.Creator{Store: store}).Begin(ctx)
	if err != nil {
		return err
	}
	normalized, sources, err := domaintemplates.QianchuanProductCreateSources(session.Config())
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(reader.output, "创建来源：")
	_, _ = fmt.Fprintln(reader.output, "  0. 千川商品全域默认模板（仅作为创建骨架）")
	for index, source := range sources[1:] {
		bindings := mapValue(source.Template["bindings"])
		_, _ = fmt.Fprintf(
			reader.output,
			"  %d. %s（广告主 %s，产品 %s / %s，商品 %d 个）\n",
			index+1,
			textValue(source.Template["display_name"]),
			textValue(bindings["advertiser_id"]),
			textValue(bindings["product_name"]),
			textValue(bindings["product_short_name"]),
			len(listValue(bindings["product_ids"])),
		)
	}
	selected, err := selectIndex(reader, "请选择来源编号: ", len(sources))
	if err != nil {
		return err
	}
	source := sources[selected]
	bindings := mapValue(source.Template["bindings"])
	advertiserID, verification, err := promptAdvertiserID(
		reader, "qianchuan", []any{bindings["advertiser_id"]}, state,
	)
	if err != nil {
		return err
	}
	productNameDefault := bindings["product_name"]
	if strings.HasPrefix(textValue(productNameDefault), "REPLACE_WITH") {
		productNameDefault = nil
	}
	productName, err := reader.value("产品名称", productNameDefault, true)
	if err != nil {
		return err
	}
	productShortNameDefault := withoutPlaceholder(bindings["product_short_name"])
	productShortName, err := reader.value("商品简称（用于计划名称）", productShortNameDefault, true)
	if err != nil {
		return err
	}
	productIDsDefault := joinValues(listValue(bindings["product_ids"]), "/")
	productIDs, err := reader.value("商品 ID（多个使用 / 分隔，最多 30 个）", productIDsDefault, true)
	if err != nil {
		return err
	}
	templateName, err := reader.value("模板名称", source.Template["display_name"], true)
	if err != nil {
		return err
	}
	planNameTemplate, err := reader.value(
		"计划名称形式",
		defaultOr(source.Template["plan_name_template"], "{product_name}-{creator_name}-{datetime}"),
		true,
	)
	if err != nil {
		return err
	}
	templateID, err := generateTemplateID("qcpt_")
	if err != nil {
		return err
	}
	candidate, err := domaintemplates.BuildQianchuanProductTemplate(
		source.Template,
		domaintemplates.QianchuanProductCreateInput{
			TemplateID: templateID, TemplateName: templateName,
			AdvertiserID: advertiserID, ProductName: productName,
			ProductShortName: productShortName,
			ProductIDs:       productIDs, PlanNameTemplate: planNameTemplate,
		},
	)
	if err != nil {
		return err
	}
	preview := orderedObject{
		{name: "source_template", value: source.ID},
		{name: "template", value: orderedQianchuanTemplate(candidate, false)},
		{name: "advertiser_binding_verification", value: orderedVerification(verification)},
		{name: "omitted_fields", value: []any{"aweme_id", "product_channel_info", "multi_product_creative_list"}},
	}
	if err := writePrettyJSON(reader.output, preview); err != nil {
		return err
	}
	confirmed, err := reader.yesNo("确认创建模板", false)
	if err != nil {
		return err
	}
	if !confirmed {
		return writePrettyJSON(reader.output, orderedObject{
			{name: "created", value: false},
			{name: "preview", value: preview},
		})
	}
	updated, err := domaintemplates.ApplyQianchuanProductTemplate(normalized, candidate)
	if err != nil {
		return err
	}
	if err := session.Finish(ctx, updated, true); err != nil {
		return err
	}
	return writePrettyJSON(reader.output, orderedObject{
		{name: "created", value: true},
		{name: "template_id", value: candidate["template_id"]},
		{name: "name", value: candidate["display_name"]},
		{name: "template", value: orderedQianchuanTemplate(candidate, false)},
	})
}

func runQianchuanLiveTemplateWizard(
	ctx context.Context,
	store applicationtemplates.ConfigStore,
	resolvedPath string,
	reader *promptReader,
	state domain.AuthorizationState,
) error {
	session, err := (applicationtemplates.Creator{Store: store}).Begin(ctx)
	if err != nil {
		return err
	}
	normalized, sources, err := domaintemplates.QianchuanLiveCreateSources(session.Config())
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(reader.output, "创建来源：")
	_, _ = fmt.Fprintln(reader.output, "  0. 千川直播全域默认模板（仅作为创建骨架）")
	for index, source := range sources[1:] {
		bindings := mapValue(source.Template["bindings"])
		_, _ = fmt.Fprintf(
			reader.output,
			"  %d. %s（广告主 %s，直播账号 %s / %s）\n",
			index+1,
			textValue(source.Template["display_name"]),
			textValue(bindings["advertiser_id"]),
			textValue(bindings["creator_name"]),
			textValue(bindings["aweme_id"]),
		)
	}
	selected, err := selectIndex(reader, "请选择来源编号: ", len(sources))
	if err != nil {
		return err
	}
	source := sources[selected]
	bindings := mapValue(source.Template["bindings"])
	advertiserID, verification, err := promptAdvertiserID(
		reader, "qianchuan", []any{bindings["advertiser_id"]}, state,
	)
	if err != nil {
		return err
	}
	creatorDefault := withoutPlaceholder(bindings["creator_name"])
	creatorName, err := reader.value("直播账号名称", creatorDefault, true)
	if err != nil {
		return err
	}
	awemeDefault := withoutPlaceholder(bindings["aweme_id"])
	awemeID, err := reader.value("直播账号 numeric aweme_id", awemeDefault, true)
	if err != nil {
		return err
	}
	delivery := copyMap(source.Template["delivery_setting"])
	budget, err := reader.value("日预算", defaultOr(delivery["budget"], 5000), true)
	if err != nil {
		return err
	}
	delivery["budget"] = budget
	bidType, err := selectLiveBidType(reader, textValue(delivery["smart_bid_type"]))
	if err != nil {
		return err
	}
	delivery["smart_bid_type"] = bidType
	if bidType == "SMART_BID_CUSTOM" {
		roi, promptErr := reader.value("目标 ROI", defaultOr(delivery["roi2_goal"], 1.7), true)
		if promptErr != nil {
			return promptErr
		}
		delivery["roi2_goal"] = roi
		delete(delivery, "daily_delivery_time")
	} else {
		duration, promptErr := reader.value("每日投放时长（0.5 小时步进）", defaultOr(delivery["daily_delivery_time"], 8.5), true)
		if promptErr != nil {
			return promptErr
		}
		delivery["daily_delivery_time"] = duration
		delete(delivery, "roi2_goal")
	}
	updatedSource := copyMap(source.Template)
	updatedSource["delivery_setting"] = delivery
	templateID, err := generateTemplateID("qclt_")
	if err != nil {
		return err
	}
	candidate, err := domaintemplates.BuildQianchuanLiveTemplate(
		updatedSource,
		domaintemplates.QianchuanLiveCreateInput{
			TemplateID: templateID, AdvertiserID: advertiserID,
			CreatorName: creatorName, AwemeID: awemeID,
		},
	)
	if err != nil {
		return err
	}
	preview := orderedObject{
		{name: "source_template", value: source.ID},
		{name: "template", value: orderedQianchuanTemplate(candidate, true)},
		{name: "advertiser_binding_verification", value: orderedVerification(verification)},
	}
	if err := writePrettyJSON(reader.output, preview); err != nil {
		return err
	}
	confirmed, err := reader.yesNo("确认创建模板", false)
	if err != nil {
		return err
	}
	if !confirmed {
		return writePrettyJSON(reader.output, orderedObject{
			{name: "created", value: false},
			{name: "preview", value: preview},
			{name: "config", value: resolvedPath},
		})
	}
	updated, err := domaintemplates.ApplyQianchuanLiveTemplate(normalized, candidate)
	if err != nil {
		return err
	}
	if err := session.Finish(ctx, updated, true); err != nil {
		return err
	}
	return writePrettyJSON(reader.output, orderedObject{
		{name: "created", value: true},
		{name: "template_id", value: candidate["template_id"]},
		{name: "name", value: candidate["display_name"]},
		{name: "template", value: orderedQianchuanTemplate(candidate, true)},
		{name: "config", value: resolvedPath},
	})
}

func selectIndex(reader *promptReader, prompt string, count int) (int, error) {
	for {
		value, err := reader.line(prompt)
		if err != nil {
			return 0, err
		}
		index, parseErr := strconv.Atoi(value)
		if parseErr == nil && index >= 0 && index < count {
			return index, nil
		}
	}
}

func selectLiveBidType(reader *promptReader, inherited string) (string, error) {
	choices := []struct {
		value string
		label string
	}{
		{value: "SMART_BID_CUSTOM", label: "控成本（目标 ROI）"},
		{value: "SMART_BID_CONSERVATIVE", label: "放量（保守出价）"},
	}
	defaultIndex := 1
	for index, choice := range choices {
		if choice.value == inherited {
			defaultIndex = index
		}
	}
	_, _ = fmt.Fprintln(reader.output, "直播出价方式：")
	for index, choice := range choices {
		suffix := ""
		if index == defaultIndex {
			suffix = "（当前）"
		}
		_, _ = fmt.Fprintf(reader.output, "  %d. %s%s\n", index, choice.label, suffix)
	}
	for {
		value, err := reader.line(fmt.Sprintf("请选择出价方式编号 [%d]: ", defaultIndex))
		if err != nil {
			return "", err
		}
		if value == "" {
			return choices[defaultIndex].value, nil
		}
		index, parseErr := strconv.Atoi(value)
		if parseErr == nil && index >= 0 && index < len(choices) {
			return choices[index].value, nil
		}
	}
}

func joinValues(values []any, separator string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, textValue(value))
	}
	return strings.Join(parts, separator)
}

func withoutPlaceholder(value any) any {
	if strings.HasPrefix(textValue(value), "REPLACE_WITH") {
		return nil
	}
	return value
}

func defaultOr(value, fallback any) any {
	if value == nil || textValue(value) == "" {
		return fallback
	}
	return value
}
