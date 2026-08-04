package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	applicationtemplates "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/templates"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	domaintemplates "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/templates"
)

var marketingAgePresets = map[string][]any{
	"不限":    {},
	"none":  {},
	"18-23": {"AGE_BETWEEN_18_23"},
	"24-49": {"AGE_BETWEEN_24_30", "AGE_BETWEEN_31_40", "AGE_BETWEEN_41_49"},
	"50+":   {"AGE_ABOVE_50"},
}

var marketingOfficialAgeGroups = map[string]bool{
	"AGE_BETWEEN_18_23": true,
	"AGE_BETWEEN_24_30": true,
	"AGE_BETWEEN_31_40": true,
	"AGE_BETWEEN_41_49": true,
	"AGE_ABOVE_50":      true,
}

var marketingOfficialRefinedAgeGroups = map[string]bool{
	"AGE_BETWEEN_18_19": true,
	"AGE_BETWEEN_20_23": true,
	"AGE_BETWEEN_24_30": true,
	"AGE_BETWEEN_31_35": true,
	"AGE_BETWEEN_36_40": true,
	"AGE_BETWEEN_41_45": true,
	"AGE_BETWEEN_46_50": true,
	"AGE_BETWEEN_51_55": true,
	"AGE_BETWEEN_56_59": true,
	"AGE_ABOVE_60":      true,
}

func runMarketingTemplateWizard(
	ctx context.Context,
	store applicationtemplates.ConfigStore,
	resolvedPath string,
	reader *promptReader,
	state domain.AuthorizationState,
	materialSourceType string,
) error {
	session, err := (applicationtemplates.Creator{Store: store}).Begin(ctx)
	if err != nil {
		return err
	}
	normalized, sources, err := domaintemplates.MarketingCreateSources(session.Config(), materialSourceType)
	if err != nil {
		return err
	}
	sourceLabel := map[string]string{
		"ACCOUNT_UPLOAD":     "混剪素材",
		"CREATOR_AUTHORIZED": "原生素材",
	}[materialSourceType]
	if sourceLabel == "" {
		_, _ = fmt.Fprintln(reader.output, "创建来源：")
	} else {
		_, _ = fmt.Fprintf(reader.output, "创建来源（%s）:\n", sourceLabel)
	}
	_, _ = fmt.Fprintln(reader.output, "  0. 默认模板（仅作为新业务模板骨架）")
	for index, source := range sources[1:] {
		bindings := mapValue(source.Template["bindings"])
		strategy := mapValue(source.Template["material_strategy"])
		_, _ = fmt.Fprintf(
			reader.output,
			"  %d. %s（渠道 %s，广告主 %s，平台 %s，商品 %s，商品 ID %s，素材来源 %s）\n",
			index+1,
			source.Name,
			textValue(bindings["channel"]),
			textValue(bindings["advertiser_id"]),
			textValue(bindings["platform"]),
			textValue(bindings["product_name"]),
			textValue(bindings["product_id"]),
			marketingMaterialSourceLabel(strategy["source_type"]),
		)
	}
	selected, err := selectIndex(reader, "请选择来源编号: ", len(sources))
	if err != nil {
		return err
	}
	source := sources[selected]
	bindings := mapValue(source.Template["bindings"])
	inheritedStrategy := mapValue(source.Template["material_strategy"])
	overrides := mapValue(source.Template["overrides"])
	sourceDefaults := mapValue(overrides["defaults"])
	account := mapValue(normalized["account"])
	advertiserID, verification, err := promptAdvertiserID(
		reader,
		"marketing",
		[]any{bindings["advertiser_id"], account["advertiser_id"]},
		state,
	)
	if err != nil {
		return err
	}
	platform, err := reader.value("平台", bindings["platform"], true)
	if err != nil {
		return err
	}
	trafficSource, err := reader.value("流量来源", defaultOr(bindings["traffic_source"], "CID"), true)
	if err != nil {
		return err
	}
	productName, err := reader.value("商品名称", bindings["product_name"], true)
	if err != nil {
		return err
	}
	productID, err := reader.value("商品 ID", bindings["product_id"], true)
	if err != nil {
		return err
	}
	policy := domaintemplates.MarketingClonePolicy(source.Template, advertiserID, productID)
	defaultBundle := marketingDefaultBundle(normalized)
	defaultDefaults := mapValue(defaultBundle["defaults"])
	productInfo := marketingWizardProductInfo(
		defaultDefaults,
		sourceDefaults,
		source.Template,
		policy,
		productName,
	)
	var sellingPoints any
	if textValue(productInfo["product_selling_point_type"]) == "CUSTOM" {
		sellingPoints, err = collectMarketingSellingPoints(reader, listValue(productInfo["selling_points"]), productName)
		if err != nil {
			return err
		}
		productInfo["selling_points"] = sellingPoints
	}
	inheritedDefaults := deepMergeMaps(defaultDefaults, sourceDefaults)
	dailyBudget, err := promptPositiveNumber(reader, "日预算", defaultOr(inheritedDefaults["daily_budget"], 300))
	if err != nil {
		return err
	}
	roiGoal, err := promptPositiveNumber(reader, "净成交 ROI 出价", defaultOr(inheritedDefaults["roi_goal"], 1.5))
	if err != nil {
		return err
	}
	gender, err := selectMarketingGender(reader, textValue(inheritedDefaults["gender"]))
	if err != nil {
		return err
	}
	ages, err := selectMarketingAges(reader, listValue(inheritedDefaults["ages"]))
	if err != nil {
		return err
	}
	selectionMode, err := selectMarketingSelectionMode(reader, textValue(inheritedStrategy["selection_mode"]))
	if err != nil {
		return err
	}
	maximumDefault := inheritedStrategy["max_materials_per_unit"]
	if _, exists := inheritedStrategy["max_materials_per_unit"]; !exists {
		maximumDefault = 5
	}
	maximum, unlimited, err := selectMarketingMaterialLimit(reader, materialSourceType, maximumDefault)
	if err != nil {
		return err
	}
	var creatorIDs any
	var minimumRemaining any
	if materialSourceType == "CREATOR_AUTHORIZED" {
		creatorFilters := mapValue(inheritedStrategy["creator_filters"])
		creatorIDsText, promptErr := reader.value(
			"达人 ID 白名单（逗号分隔，留空表示不限）",
			joinValues(listValue(creatorFilters["creator_ids"]), ","),
			false,
		)
		if promptErr != nil {
			return promptErr
		}
		creatorIDs = splitNonempty(creatorIDsText, ",")
		minimumRemainingText, promptErr := reader.value(
			"授权至少剩余天数",
			defaultOr(creatorFilters["minimum_remaining_days"], 1),
			true,
		)
		if promptErr != nil {
			return promptErr
		}
		minimumRemaining, err = strconv.Atoi(minimumRemainingText)
		if err != nil {
			return fmt.Errorf("授权至少剩余天数必须是整数")
		}
	}
	templateName, err := reader.value("模板名称", source.Template["display_name"], true)
	if err != nil {
		return err
	}
	projectNameDefault := sourceDefaults["project_name_template"]
	promotionNameDefault := sourceDefaults["promotion_name_template"]
	if textValue(projectNameDefault) == "" || textValue(promotionNameDefault) == "" {
		if materialSourceType == "ACCOUNT_UPLOAD" {
			projectNameDefault = "{material_date}_混剪素材roi_详情页"
			promotionNameDefault = "自动投放单元_{product_name}_{material_date}日_混剪"
		} else {
			projectNameDefault = "{material_date}_原生素材roi_详情页"
			promotionNameDefault = "自动投放单元_{product_name}_{material_date}日_原生"
		}
	}
	projectNameTemplate, err := reader.value("项目名称形式", projectNameDefault, true)
	if err != nil {
		return err
	}
	promotionNameTemplate, err := reader.value("广告名称形式", promotionNameDefault, true)
	if err != nil {
		return err
	}
	sameProduct := policy == "same_advertiser_same_product" || policy == "cross_advertiser_same_product"
	inheritedTitles := []any{}
	if sameProduct {
		inheritedTitles = listValue(mapValue(source.Template["copy_materials"])["titles"])
	}
	titles, err := collectMarketingTitles(reader, inheritedTitles)
	if err != nil {
		return err
	}
	preserveBusinessDefaults := policy == "same_advertiser_same_product"
	links := map[string]any{}
	tracking := map[string]any{}
	if preserveBusinessDefaults {
		links = mapValue(overrides["links"])
		tracking = mapValue(overrides["tracking_urls"])
	}
	planSource, err := reader.value("计划来源", sourceDefaults["source"], false)
	if err != nil {
		return err
	}
	landingPageURL, err := reader.opaqueValue("落地页链接", links["landing_page_url"])
	if err != nil {
		return err
	}
	openURL, err := reader.opaqueValue("直达链接", links["open_url"])
	if err != nil {
		return err
	}
	trackURL, err := reader.opaqueValue("展示监测链接", firstListText(tracking["track_url"]))
	if err != nil {
		return err
	}
	actionTrackURL, err := reader.opaqueValue("点击/有效触点监测链接", firstListText(tracking["action_track_url"]))
	if err != nil {
		return err
	}
	input := domaintemplates.MarketingCreateInput{
		TemplateName: templateName,
		AdvertiserID: advertiserID, Platform: platform, TrafficSource: trafficSource,
		ProductID: productID, ProductName: productName, ProductInfo: productInfo,
		SellingPoints: sellingPoints, DailyBudget: dailyBudget, ROIGoal: roiGoal,
		Gender: gender, Ages: ages, MaterialSourceType: materialSourceType,
		SelectionMode: selectionMode, MaxMaterials: maximum, UnlimitedMaterials: unlimited,
		CreatorIDs: creatorIDs, MinimumRemaining: minimumRemaining, Titles: titles,
		PlanSource: planSource, LandingPageURL: landingPageURL, OpenURL: openURL,
		TrackURL: trackURL, ActionTrackURL: actionTrackURL,
		ProjectNameTemplate: projectNameTemplate, PromotionNameTemplate: promotionNameTemplate,
	}
	createdName, candidate, err := domaintemplates.BuildMarketingTemplate(
		normalized, source.Name, source.Template, input,
	)
	if err != nil {
		return err
	}
	validation := domaintemplates.MarketingCandidateReadiness(normalized, createdName, candidate)
	var sourceSnapshot map[string]any
	if source.Name != "default_plan_template" {
		sourceSnapshot = mapValue(mapValue(normalized["plan_templates"])[source.Name])
	}
	preview := orderedMarketingPreview(
		createdName, candidate, verification,
		dailyBudget, roiGoal, gender, ages,
		productInfo, validation,
		domaintemplates.MarketingTemplateDiff(sourceSnapshot, candidate),
	)
	_, _ = fmt.Fprintln(reader.output, "创建前预览：")
	if err := writePrettyJSON(reader.output, preview); err != nil {
		return err
	}
	ready, _ := validation["ready_for_plan_creation"].(bool)
	confirmed := false
	blocked := !ready
	if ready {
		confirmed, err = reader.yesNo("确认创建此业务模板", false)
		if err != nil {
			return err
		}
	}
	updated := normalized
	if confirmed {
		updated, err = domaintemplates.ApplyMarketingTemplate(normalized, createdName, candidate)
		if err != nil {
			return err
		}
		if err := session.Finish(ctx, updated, true); err != nil {
			return err
		}
	}
	wizardResult := append(orderedObject{}, preview...)
	wizardResult = append(wizardResult,
		orderedField{name: "confirmed", value: confirmed},
		orderedField{name: "changed", value: confirmed},
	)
	if blocked {
		wizardResult = append(wizardResult, orderedField{name: "blocked", value: true})
	}
	lifecycleResult, err := domaintemplates.MarketingLifecycleResult(updated, resolvedPath, "create-wizard", confirmed)
	if err != nil {
		return err
	}
	return writePrettyJSON(reader.output, orderedMarketingLifecycleResult(lifecycleResult, wizardResult))
}

func marketingDefaultBundle(config map[string]any) map[string]any {
	if configured, ok := config["default_plan_template"].(map[string]any); ok {
		return copyMap(configured)
	}
	result := map[string]any{}
	for _, section := range []string{"defaults", "materials", "resolved_ids", "links", "tracking_urls"} {
		result[section] = copyMap(config[section])
	}
	result["titles"] = copyValue(listValue(config["titles"]))
	return result
}

func marketingWizardProductInfo(
	defaultDefaults map[string]any,
	sourceDefaults map[string]any,
	source map[string]any,
	policy string,
	productName string,
) map[string]any {
	productInfo := copyMap(defaultDefaults["product_info"])
	if policy == "same_advertiser_same_product" && source != nil {
		if inherited := mapValue(sourceDefaults["product_info"]); len(inherited) != 0 {
			productInfo = copyMap(inherited)
		}
	} else {
		productInfo["product_image_type"] = "DPA"
		productInfo["product_image_fields"] = []any{"images_url"}
	}
	if textValue(productInfo["product_name_type"]) == "" {
		productInfo["product_name_type"] = "CUSTOM"
	}
	if productInfo["product_name_type"] == "CUSTOM" {
		productInfo["titles"] = []any{productName}
	}
	if textValue(productInfo["product_selling_point_type"]) == "" {
		productInfo["product_selling_point_type"] = "CUSTOM"
	}
	return productInfo
}

func collectMarketingSellingPoints(reader *promptReader, inherited []any, productName string) ([]any, error) {
	defaultValue := joinValues(inherited, ",")
	if defaultValue == "" {
		for _, candidate := range []string{productName + "推荐", productName} {
			if marketingSellingPointValid(candidate) {
				defaultValue = candidate
				break
			}
		}
	}
	for {
		value, err := reader.value("产品卖点（每条 6-9 位置，多个用逗号分隔）", defaultValue, true)
		if err != nil {
			return nil, err
		}
		values := splitNonempty(strings.ReplaceAll(value, "，", ","), ",")
		if len(values) == 0 || len(values) > 10 {
			continue
		}
		seen := map[string]bool{}
		result := []any{}
		valid := true
		for _, item := range values {
			text := textValue(item)
			if !marketingSellingPointValid(text) {
				valid = false
				break
			}
			if !seen[text] {
				seen[text] = true
				result = append(result, text)
			}
		}
		if valid && len(result) != 0 {
			return result, nil
		}
	}
}

func marketingSellingPointValid(value string) bool {
	positions := 0
	for _, character := range value {
		if character < 128 {
			positions++
		} else {
			positions += 2
		}
	}
	return positions >= 12 && positions <= 18
}

func promptPositiveNumber(reader *promptReader, label string, defaultValue any) (any, error) {
	for {
		value, err := reader.value(label, defaultValue, true)
		if err != nil {
			return nil, err
		}
		number, parseErr := strconv.ParseFloat(value, 64)
		if parseErr != nil || number <= 0 {
			continue
		}
		if number == float64(int64(number)) {
			return int64(number), nil
		}
		return number, nil
	}
}

func selectMarketingGender(reader *promptReader, inherited string) (string, error) {
	labels := map[string]string{"NONE": "不限", "GENDER_MALE": "男", "GENDER_FEMALE": "女"}
	aliases := map[string]string{
		"0": "NONE", "不限": "NONE", "none": "NONE",
		"1": "GENDER_MALE", "男": "GENDER_MALE", "gender_male": "GENDER_MALE",
		"2": "GENDER_FEMALE", "女": "GENDER_FEMALE", "gender_female": "GENDER_FEMALE",
	}
	defaultValue := labels[inherited]
	if defaultValue == "" {
		defaultValue = "不限"
	}
	for {
		value, err := reader.line(fmt.Sprintf("性别（0 不限 / 1 男 / 2 女） [%s]: ", defaultValue))
		if err != nil {
			return "", err
		}
		if value == "" {
			value = defaultValue
		}
		if gender := aliases[strings.ToLower(value)]; gender != "" {
			return gender, nil
		}
	}
}

func selectMarketingAges(reader *promptReader, inherited []any) ([]any, error) {
	defaultValue := marketingAgeDisplay(inherited)
	for {
		value, err := reader.line(
			"年龄（不限 / 18-23 / 24-49 / 50+ / 官方枚举逗号分隔） [" + defaultValue + "]: ",
		)
		if err != nil {
			return nil, err
		}
		if value == "" {
			value = defaultValue
		}
		if ages, ok := normalizeMarketingAges(value); ok {
			return ages, nil
		}
	}
}

func marketingAgeDisplay(values []any) string {
	for _, label := range []string{"不限", "18-23", "24-49", "50+"} {
		if sameTextList(values, marketingAgePresets[label]) {
			return label
		}
	}
	return joinValues(values, ",")
}

func normalizeMarketingAges(value string) ([]any, bool) {
	normalized := strings.TrimSpace(value)
	if preset, exists := marketingAgePresets[strings.ToLower(normalized)]; exists {
		return copyValue(preset).([]any), true
	}
	parts := splitNonempty(strings.ToUpper(strings.ReplaceAll(normalized, "，", ",")), ",")
	if len(parts) == 0 {
		return []any{}, true
	}
	allStandard := true
	allRefined := true
	for _, part := range parts {
		text := textValue(part)
		allStandard = allStandard && marketingOfficialAgeGroups[text]
		allRefined = allRefined && marketingOfficialRefinedAgeGroups[text]
	}
	return parts, allStandard || allRefined
}

func selectMarketingSelectionMode(reader *promptReader, inherited string) (string, error) {
	defaultValue := "1"
	if inherited == "LATEST" {
		defaultValue = "2"
	}
	for {
		value, err := reader.line("素材选择方式（1 手动选择 / 2 自动选择最新） [" + defaultValue + "]: ")
		if err != nil {
			return "", err
		}
		if value == "" {
			value = defaultValue
		}
		if value == "1" {
			return "MANUAL", nil
		}
		if value == "2" {
			return "LATEST", nil
		}
	}
}

func selectMarketingMaterialLimit(reader *promptReader, sourceType string, inherited any) (any, bool, error) {
	if sourceType != "CREATOR_AUTHORIZED" {
		value, err := reader.value("每单元素材数量", defaultOr(inherited, 5), true)
		if err != nil {
			return nil, false, err
		}
		maximum, parseErr := strconv.Atoi(value)
		if parseErr != nil {
			return nil, false, parseErr
		}
		return maximum, false, nil
	}
	defaultValue := "不限"
	if inherited != nil {
		defaultValue = textValue(inherited)
	}
	for {
		value, err := reader.line("每单元素材数量（正整数 / 不限） [" + defaultValue + "]: ")
		if err != nil {
			return nil, false, err
		}
		if value == "" {
			value = defaultValue
		}
		if value == "不限" || strings.EqualFold(value, "unlimited") || strings.EqualFold(value, "none") {
			return nil, true, nil
		}
		maximum, parseErr := strconv.Atoi(value)
		if parseErr == nil && maximum > 0 {
			return maximum, false, nil
		}
	}
}

func collectMarketingTitles(reader *promptReader, inherited []any) ([]any, error) {
	if len(inherited) != 0 {
		confirmed, err := reader.yesNo(fmt.Sprintf("复制来源模板的 %d 条文案", len(inherited)), true)
		if err != nil {
			return nil, err
		}
		if confirmed {
			return copyValue(inherited).([]any), nil
		}
	}
	result := []any{}
	seen := map[string]bool{}
	for {
		value, err := reader.line("输入文案标题（留空结束）: ")
		if err != nil {
			return nil, err
		}
		if value == "" {
			return result, nil
		}
		length := utf8.RuneCountInString(value)
		if length < 5 || length > 30 {
			return nil, fmt.Errorf("copy title length must be 5-30 characters: %s", value)
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
}

func splitNonempty(value, separator string) []any {
	result := []any{}
	seen := map[string]bool{}
	for _, part := range strings.Split(value, separator) {
		part = strings.TrimSpace(part)
		if part != "" && !seen[part] {
			seen[part] = true
			result = append(result, part)
		}
	}
	return result
}

func sameTextList(left, right []any) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if textValue(left[index]) != textValue(right[index]) {
			return false
		}
	}
	return true
}

func marketingMaterialSourceLabel(value any) string {
	if value == "ACCOUNT_UPLOAD" {
		return "上传素材"
	}
	if value == "CREATOR_AUTHORIZED" {
		return "达人素材"
	}
	return textValue(value)
}

func orderedMarketingPreview(
	createdName string,
	candidate map[string]any,
	verification map[string]any,
	dailyBudget any,
	roiGoal any,
	gender string,
	ages []any,
	productInfo map[string]any,
	validation map[string]any,
	changes []any,
) orderedObject {
	return orderedObject{
		{name: "action", value: "create_business_template"},
		{name: "template", value: createdName},
		{name: "name_templates", value: orderedObject{
			{name: "project", value: mapValue(mapValue(candidate["overrides"])["defaults"])["project_name_template"]},
			{name: "promotion", value: mapValue(mapValue(candidate["overrides"])["defaults"])["promotion_name_template"]},
		}},
		{name: "source", value: orderedMap(mapValue(candidate["created_from"]), []string{"type", "template", "policy", "cleared_fields"}, nil)},
		{name: "bindings", value: orderedMap(mapValue(candidate["bindings"]), []string{"channel", "advertiser_id", "platform", "traffic_source", "product_id", "product_name"}, nil)},
		{name: "advertiser_binding_verification", value: orderedVerification(verification)},
		{name: "delivery_settings", value: orderedObject{
			{name: "daily_budget", value: dailyBudget},
			{name: "roi_goal", value: roiGoal},
			{name: "gender", value: gender},
			{name: "ages", value: ages},
		}},
		{name: "product_image", value: orderedObject{
			{name: "type", value: productInfo["product_image_type"]},
			{name: "fields", value: listValue(productInfo["product_image_fields"])},
			{name: "manual_image_ids_required", value: productInfo["product_image_type"] != "DPA"},
		}},
		{name: "product_selling_points", value: listValue(productInfo["selling_points"])},
		{name: "material_strategy", value: orderedMaterialStrategy(mapValue(candidate["material_strategy"]))},
		{name: "copy_title_count", value: len(listValue(mapValue(candidate["copy_materials"])["titles"]))},
		{name: "validation", value: orderedMap(validation, []string{"ready_for_plan_creation", "template_missing_fields", "runtime_missing_fields"}, nil)},
		{name: "changes", value: orderedMarketingChanges(changes)},
	}
}

func orderedMarketingChanges(changes []any) []any {
	result := make([]any, 0, len(changes))
	for _, raw := range changes {
		result = append(result, orderedMap(mapValue(raw), []string{"field", "before", "after"}, nil))
	}
	return result
}

func orderedMarketingLifecycleResult(result map[string]any, wizardResult orderedObject) orderedObject {
	return orderedObject{
		{name: "config", value: result["config"]},
		{name: "command", value: result["command"]},
		{name: "changed", value: result["changed"]},
		{name: "created_template", value: result["created_template"]},
		{name: "wizard_result", value: wizardResult},
		{name: "default_template", value: orderedMarketingDefaultSummary(mapValue(result["default_template"]))},
		{name: "templates", value: orderedMarketingRows(listValue(result["templates"]))},
	}
}

func orderedMarketingDefaultSummary(value map[string]any) orderedObject {
	return orderedMap(
		value,
		[]string{"name", "type", "business_usable", "selectable_for_plan_creation", "purpose", "delivery_settings", "product_image", "regions", "sections"},
		func(key string, item any) any {
			switch key {
			case "delivery_settings":
				return orderedMap(mapValue(item), []string{"daily_budget", "roi_goal", "gender", "ages"}, nil)
			case "product_image":
				return orderedMap(mapValue(item), []string{"type", "fields", "manual_image_ids_required"}, nil)
			case "regions":
				return orderedMap(mapValue(item), []string{"city_count", "city_names"}, nil)
			default:
				return item
			}
		},
	)
}

func orderedMarketingRows(values []any) []any {
	result := make([]any, 0, len(values))
	for _, raw := range values {
		row := mapValue(raw)
		result = append(result, orderedMap(
			row,
			[]string{
				"name", "project_name_template", "promotion_name_template", "channel", "advertiser_id", "platform", "traffic_source",
				"product_id", "product_name", "product_image_ids", "product_image",
				"delivery_settings", "material_source_type", "material_source_name",
				"material_strategy", "copy_materials", "bindings", "legacy", "binding_error",
			},
			func(key string, item any) any {
				switch key {
				case "product_image":
					return orderedMap(mapValue(item), []string{"type", "fields", "manual_image_ids_required"}, nil)
				case "delivery_settings":
					return orderedMap(mapValue(item), []string{"daily_budget", "roi_goal", "gender", "ages"}, nil)
				case "material_strategy":
					return orderedMaterialStrategy(mapValue(item))
				case "copy_materials":
					return orderedMap(mapValue(item), []string{"configured", "title_count", "titles", "copied_from_template"}, nil)
				case "bindings":
					return orderedMap(mapValue(item), []string{"channel", "advertiser_id", "platform", "traffic_source", "product_id", "product_name"}, nil)
				default:
					return item
				}
			},
		))
	}
	return result
}
