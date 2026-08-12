package templates

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

var marketingAccountOwnedFields = []string{
	"resolved_ids.event_asset_ids",
	"resolved_ids.landing_page_asset_id",
}

var marketingDynamicMaterialFields = []string{
	"materials.video_ids",
	"materials.video_cover_ids",
}

var marketingProductOwnedFields = []string{
	"defaults.product_info",
	"resolved_ids.brand_info",
	"resolved_ids.category_name",
	"resolved_ids.brand_name",
	"resolved_ids.product_platform_id",
	"resolved_ids.product_image_ids",
}

var marketingLinkFields = []string{
	"links.landing_page_url",
	"links.open_url",
	"tracking_urls.track_url",
	"tracking_urls.action_track_url",
}

var marketingPayloadRequiredFields = []string{
	"defaults.operation",
	"defaults.product_name",
	"defaults.product_id",
	"defaults.daily_budget",
	"defaults.source",
	"defaults.landing_type",
	"defaults.marketing_goal",
	"defaults.delivery_mode",
	"defaults.ad_type",
	"defaults.gender",
	"defaults.location_type",
	"defaults.district",
	"defaults.region_version",
	"defaults.schedule_type",
	"defaults.budget_mode",
	"defaults.pricing",
	"defaults.video_image_mode",
}

var marketingRuntimeMissingFields = map[string]bool{
	"materials.video_ids":                true,
	"runtime.creator_material_selection": true,
	"api.access_token":                   true,
}

var marketingSourceNameTemplates = map[string]map[string]any{
	"ACCOUNT_UPLOAD": {
		"project_name_template":   "{material_date}_混剪素材roi_详情页",
		"promotion_name_template": "自动投放单元_{product_name}_{material_date}日_混剪",
	},
	"CREATOR_AUTHORIZED": {
		"project_name_template":   "{material_date}_原生素材roi_详情页",
		"promotion_name_template": "自动投放单元_{product_name}_{material_date}日_原生",
	},
}

type MarketingCreateSource struct {
	Name     string
	Template map[string]any
}

type MarketingCreateInput struct {
	TemplateName          string
	AdvertiserID          string
	Platform              string
	TrafficSource         string
	ProductID             string
	ProductName           string
	ProductInfo           map[string]any
	SellingPoints         any
	DailyBudget           any
	ROIGoal               any
	Gender                string
	Ages                  any
	MaterialSourceType    string
	SelectionMode         string
	MaxMaterials          any
	UnlimitedMaterials    bool
	CreatorIDs            any
	MinimumRemaining      any
	Titles                any
	PlanSource            string
	LandingPageURL        string
	OpenURL               string
	TrackURL              string
	ActionTrackURL        string
	ProjectNameTemplate   string
	PromotionNameTemplate string
}

func MarketingCreateSources(
	config map[string]any,
	materialSourceType string,
) (map[string]any, []MarketingCreateSource, error) {
	normalized, legacyError, err := MigrateMarketing(config, false)
	if err != nil {
		return nil, nil, err
	}
	if legacyError != nil {
		return nil, nil, legacyError
	}
	sources := []MarketingCreateSource{{Name: "default_plan_template"}}
	for _, name := range sortedKeys(mapOrEmpty(normalized["plan_templates"])) {
		template, err := normalizeMarketingTemplate(
			normalized,
			name,
			mapOrEmpty(mapOrEmpty(normalized["plan_templates"])[name]),
		)
		if err != nil {
			return nil, nil, err
		}
		if materialSourceType != "" && mapOrEmpty(template["material_strategy"])["source_type"] != materialSourceType {
			continue
		}
		sources = append(sources, MarketingCreateSource{Name: name, Template: template})
	}
	return normalized, sources, nil
}

func MarketingClonePolicy(source map[string]any, advertiserID, productID string) string {
	if source == nil {
		return "default"
	}
	bindings := mapOrEmpty(source["bindings"])
	advertiserChanged := stringValue(bindings["advertiser_id"]) != advertiserID
	productChanged := stringValue(bindings["product_id"]) != productID
	switch {
	case advertiserChanged && productChanged:
		return "cross_advertiser_new_product"
	case advertiserChanged:
		return "cross_advertiser_same_product"
	case productChanged:
		return "same_advertiser_new_product"
	default:
		return "same_advertiser_same_product"
	}
}

func BuildMarketingTemplate(
	config map[string]any,
	sourceName string,
	source map[string]any,
	input MarketingCreateInput,
) (string, map[string]any, error) {
	advertiserID, err := positiveID(input.AdvertiserID, "advertiser_id")
	if err != nil {
		return "", nil, err
	}
	platform, err := requiredText(input.Platform, "platform")
	if err != nil {
		return "", nil, err
	}
	trafficSource, err := requiredText(input.TrafficSource, "traffic_source")
	if err != nil {
		return "", nil, err
	}
	productID, err := requiredText(input.ProductID, "product_id")
	if err != nil {
		return "", nil, err
	}
	productName, err := requiredText(input.ProductName, "product_name")
	if err != nil {
		return "", nil, err
	}
	templateName, err := requiredText(input.TemplateName, "template_name")
	if err != nil {
		return "", nil, err
	}
	targetBindings := map[string]any{
		"channel":        "marketing",
		"advertiser_id":  advertiserID,
		"platform":       platform,
		"traffic_source": trafficSource,
		"product_id":     productID,
		"product_name":   productName,
	}
	overrides, inheritedCopy, inheritedStrategy, provenance := prepareMarketingSource(
		sourceName,
		source,
		targetBindings,
	)
	policy := stringValue(provenance["policy"])
	strategy, err := buildMarketingMaterialStrategy(input, inheritedStrategy, policy)
	if err != nil {
		return "", nil, err
	}
	if inheritedStrategy != nil && inheritedStrategy["source_type"] != strategy["source_type"] {
		if hasValue(inheritedStrategy["creator_filters"]) {
			appendMarketingClearedField(provenance, "material_strategy.creator_filters")
		}
	} else if strings.HasPrefix(policy, "cross_advertiser") {
		creatorIDs := mapOrEmpty(inheritedStrategy["creator_filters"])["creator_ids"]
		if hasValue(creatorIDs) {
			appendMarketingClearedField(provenance, "material_strategy.creator_filters.creator_ids")
		}
	}
	defaults := cloneMap(mapOrEmpty(overrides["defaults"]))
	defaults["product_name"] = productName
	defaults["product_id"] = productID
	for key, value := range marketingSourceNameTemplates[stringValue(strategy["source_type"])] {
		if !hasValue(defaults[key]) {
			defaults[key] = clone(value)
		}
	}
	if strings.TrimSpace(input.ProjectNameTemplate) != "" {
		defaults["project_name_template"] = strings.TrimSpace(input.ProjectNameTemplate)
	}
	if strings.TrimSpace(input.PromotionNameTemplate) != "" {
		defaults["promotion_name_template"] = strings.TrimSpace(input.PromotionNameTemplate)
	}
	for key, value := range map[string]any{
		"daily_budget": input.DailyBudget,
		"roi_goal":     input.ROIGoal,
		"gender":       input.Gender,
		"ages":         input.Ages,
	} {
		if value != nil && value != "" {
			defaults[key] = clone(value)
		}
	}
	productInfo, err := marketingProductInfo(config, source, policy, productName, input)
	if err != nil {
		return "", nil, err
	}
	defaults["product_info"] = productInfo
	overrides["defaults"] = defaults
	resolvedIDs := cloneMap(mapOrEmpty(overrides["resolved_ids"]))
	resolvedIDs["unique_product_id"] = productID
	overrides["resolved_ids"] = resolvedIDs
	setMarketingOptionalField(overrides, "defaults", "source", input.PlanSource, false)
	setMarketingOptionalField(overrides, "links", "landing_page_url", input.LandingPageURL, false)
	setMarketingOptionalField(overrides, "links", "open_url", input.OpenURL, false)
	setMarketingOptionalField(overrides, "tracking_urls", "track_url", input.TrackURL, true)
	setMarketingOptionalField(overrides, "tracking_urls", "action_track_url", input.ActionTrackURL, true)

	titles, err := normalizeMarketingOptionalTitles(input.Titles)
	if err != nil {
		return "", nil, err
	}
	if len(titles) == 0 {
		titles = clone(listOrEmpty(inheritedCopy["titles"])).([]any)
	}
	if strings.HasSuffix(policy, "new_product") && !hasValue(input.Titles) {
		titles = []any{}
	}
	name := templateName
	return name, map[string]any{
		"display_name":      name,
		"bindings":          targetBindings,
		"copy_materials":    map[string]any{"titles": titles},
		"material_strategy": strategy,
		"created_from":      provenance,
		"overrides":         overrides,
	}, nil
}

func ApplyMarketingTemplate(config map[string]any, name string, candidate map[string]any) (map[string]any, error) {
	normalized, _, err := MarketingCreateSources(config, "")
	if err != nil {
		return nil, err
	}
	if name == "" || candidate["display_name"] != name {
		return nil, configurationError("Marketing template display_name must match the template key", map[string]any{"template": name})
	}
	templates := mapOrEmpty(normalized["plan_templates"])
	if _, exists := templates[name]; exists {
		return nil, configurationError("plan template already exists: "+name+"; use --force to replace it", map[string]any{"template": name})
	}
	readiness := MarketingCandidateReadiness(normalized, name, candidate)
	if readiness["ready_for_plan_creation"] != true {
		missing := stringValues(readiness["template_missing_fields"])
		return nil, configurationError("incomplete plan template cannot be saved: "+strings.Join(missing, ", "), map[string]any{"missing_fields": readiness["template_missing_fields"]})
	}
	updated := cloneMap(normalized)
	updatedTemplates := cloneMap(templates)
	updatedTemplates[name] = cloneMap(candidate)
	updated["plan_templates"] = updatedTemplates
	return updated, nil
}

func MarketingCandidateReadiness(config map[string]any, name string, candidate map[string]any) map[string]any {
	if strategyError := marketingMaterialStrategyError(candidate["material_strategy"]); strategyError != "" {
		return map[string]any{
			"ready_for_plan_creation": false,
			"template_missing_fields": []any{"material_strategy: " + strategyError},
			"runtime_missing_fields":  []any{},
		}
	}
	effective := marketingDefaultBundle(config)
	overrides := mapOrEmpty(candidate["overrides"])
	for _, section := range marketingTemplateSections {
		effective[section] = deepMerge(mapOrEmpty(effective[section]), mapOrEmpty(overrides[section]))
	}
	effective["titles"] = clone(listOrEmpty(mapOrEmpty(candidate["copy_materials"])["titles"]))
	bindings := mapOrEmpty(candidate["bindings"])
	defaults := mapOrEmpty(effective["defaults"])
	defaults["product_name"] = bindings["product_name"]
	defaults["product_id"] = bindings["product_id"]
	effective["defaults"] = defaults
	missing := []string{}
	for _, field := range sortedStrings(marketingPayloadRequiredFields) {
		if containsMarketingUnresolved(marketingPath(effective, field)) {
			missing = append(missing, field)
		}
	}
	baseURL := marketingPath(config, "channels.marketing.api.base_url")
	if isMissing(baseURL) {
		baseURL = marketingPath(config, "api.base_url")
	}
	if containsMarketingUnresolved(baseURL) {
		missing = append(missing, "api.base_url")
	}
	for _, field := range []string{"tracking_urls.track_url", "tracking_urls.action_track_url"} {
		if marketingOpaqueLinkMissing(marketingPath(effective, field)) {
			missing = append(missing, field)
		}
	}
	if isMissing(marketingPath(effective, "resolved_ids.city_ids")) {
		missing = append(missing, "resolved_ids.city_ids")
	}
	if isMissing(marketingPath(effective, "resolved_ids.unique_product_id")) && isMissing(marketingPath(effective, "resolved_ids.product_platform_id")) {
		missing = append(missing, "resolved_ids.product_platform_id")
	}
	if marketingOpaqueLinkMissing(marketingPath(effective, "links.landing_page_url")) {
		missing = append(missing, "links.landing_page_url")
	}
	if marketingOpaqueLinkMissing(marketingPath(effective, "links.open_url")) {
		missing = append(missing, "links.open_url")
	}
	productInfo := mapOrEmpty(marketingPath(effective, "defaults.product_info"))
	if productInfo["product_image_type"] == "DPA" {
		if containsMarketingUnresolved(productInfo["product_image_fields"]) {
			missing = append(missing, "defaults.product_info.product_image_fields")
		}
	} else if isMissing(marketingPath(effective, "resolved_ids.product_image_ids")) {
		missing = append(missing, "resolved_ids.product_image_ids")
	}
	if _, err := normalizeMarketingRequiredTitles(mapOrEmpty(candidate["copy_materials"])["titles"]); err != nil {
		missing = append(missing, "copy_materials.titles: "+err.Error())
	}
	if sellingPoints := productInfo["selling_points"]; hasValue(sellingPoints) {
		if _, err := normalizeMarketingSellingPoints(sellingPoints); err != nil {
			missing = append(missing, "defaults.product_info.selling_points")
		}
	}
	sourceType := stringValue(mapOrEmpty(candidate["material_strategy"])["source_type"])
	if sourceType == "CREATOR_AUTHORIZED" {
		missing = append(missing, "runtime.creator_material_selection")
	} else {
		missing = append(missing, "materials.video_ids")
	}
	if isMissing(marketingPath(config, "api.access_token")) && isMissing(marketingPath(config, "channels.marketing.api.access_token")) {
		missing = append(missing, "api.access_token")
	}
	missing = uniqueStrings(missing)
	templateMissing := []any{}
	runtimeMissing := []any{}
	for _, field := range missing {
		if marketingRuntimeMissingFields[field] {
			runtimeMissing = append(runtimeMissing, field)
		} else {
			templateMissing = append(templateMissing, field)
		}
	}
	return map[string]any{
		"ready_for_plan_creation": len(templateMissing) == 0,
		"template_missing_fields": templateMissing,
		"runtime_missing_fields":  runtimeMissing,
	}
}

func marketingOpaqueLinkMissing(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return typed == ""
	case []any:
		if len(typed) == 0 {
			return true
		}
		for _, item := range typed {
			if item == nil || item == "" {
				return true
			}
		}
		return false
	case []string:
		if len(typed) == 0 {
			return true
		}
		for _, item := range typed {
			if item == "" {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func MarketingTemplateDiff(source, candidate map[string]any) []any {
	left := map[string]any{}
	right := map[string]any{}
	flattenMarketing(source, "", left)
	flattenMarketing(candidate, "", right)
	keys := make([]string, 0, len(left)+len(right))
	seen := map[string]bool{}
	for key := range left {
		seen[key] = true
		keys = append(keys, key)
	}
	for key := range right {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	changes := []any{}
	for _, key := range keys {
		if semanticEqual(left[key], right[key]) {
			continue
		}
		changes = append(changes, map[string]any{
			"field":  key,
			"before": clone(left[key]),
			"after":  clone(right[key]),
		})
	}
	return changes
}

func prepareMarketingSource(
	sourceName string,
	source map[string]any,
	targetBindings map[string]any,
) (map[string]any, map[string]any, map[string]any, map[string]any) {
	if source == nil {
		return map[string]any{}, map[string]any{"titles": []any{}}, nil, map[string]any{
			"type": "default", "template": nil, "policy": "default", "cleared_fields": []any{},
		}
	}
	overrides := cloneMap(mapOrEmpty(source["overrides"]))
	copyMaterials := cloneMap(mapOrEmpty(source["copy_materials"]))
	strategy := cloneMap(mapOrEmpty(source["material_strategy"]))
	policy := MarketingClonePolicy(source, stringValue(targetBindings["advertiser_id"]), stringValue(targetBindings["product_id"]))
	fields := append([]string{}, marketingDynamicMaterialFields...)
	if strings.HasPrefix(policy, "cross_advertiser") || policy == "same_advertiser_new_product" {
		fields = append(fields, marketingAccountOwnedFields...)
		fields = append(fields, marketingProductOwnedFields...)
		fields = append(fields, marketingLinkFields...)
	}
	cleared := []string{}
	for _, field := range fields {
		if deleteMarketingPath(overrides, field) {
			cleared = append(cleared, field)
		}
	}
	sort.Strings(cleared)
	return overrides, copyMaterials, strategy, map[string]any{
		"type": "business_template", "template": sourceName, "policy": policy, "cleared_fields": anyStrings(cleared),
	}
}

func buildMarketingMaterialStrategy(
	input MarketingCreateInput,
	inherited map[string]any,
	policy string,
) (map[string]any, error) {
	sourceType := input.MaterialSourceType
	if sourceType == "" {
		sourceType = stringValue(inherited["source_type"])
	}
	if sourceType == "" {
		sourceType = "ACCOUNT_UPLOAD"
	}
	selectionMode := input.SelectionMode
	if selectionMode == "" {
		selectionMode = stringValue(inherited["selection_mode"])
	}
	if selectionMode == "" {
		selectionMode = "MANUAL"
	}
	maximum := input.MaxMaterials
	if input.UnlimitedMaterials {
		maximum = nil
	} else if maximum == nil {
		maximum = inherited["max_materials_per_unit"]
		if maximum == nil {
			maximum = 5
		}
	}
	strategy := map[string]any{
		"source_type": sourceType, "selection_mode": selectionMode, "max_materials_per_unit": maximum,
	}
	if sourceType == "CREATOR_AUTHORIZED" {
		filters := mapOrEmpty(inherited["creator_filters"])
		creatorIDs := clone(filters["creator_ids"])
		if input.CreatorIDs != nil {
			creatorIDs = normalizeMarketingStringList(input.CreatorIDs)
		} else if strings.HasPrefix(policy, "cross_advertiser") {
			creatorIDs = []any{}
		}
		if creatorIDs == nil {
			creatorIDs = []any{}
		}
		remaining := input.MinimumRemaining
		if remaining == nil {
			remaining = filters["minimum_remaining_days"]
		}
		if remaining == nil {
			remaining = 1
		}
		strategy["creator_filters"] = map[string]any{
			"creator_ids": creatorIDs, "auth_types": []any{"VIDEO_ITEM"},
			"authorization_status": "VALID", "minimum_remaining_days": remaining,
		}
	}
	if errText := marketingMaterialStrategyError(strategy); errText != "" {
		return nil, configurationError(errText, nil)
	}
	return strategy, nil
}

func marketingProductInfo(
	config map[string]any,
	source map[string]any,
	policy string,
	productName string,
	input MarketingCreateInput,
) (map[string]any, error) {
	defaultProductInfo := cloneMap(mapOrEmpty(mapOrEmpty(marketingDefaultBundle(config)["defaults"])["product_info"]))
	productInfo := cloneMap(input.ProductInfo)
	if len(productInfo) == 0 {
		if policy == "same_advertiser_same_product" && source != nil {
			productInfo = cloneMap(mapOrEmpty(mapOrEmpty(mapOrEmpty(source["overrides"])["defaults"])["product_info"]))
		}
		if len(productInfo) == 0 {
			productInfo = defaultProductInfo
		}
	}
	if policy != "same_advertiser_same_product" || source == nil {
		productInfo["product_image_type"] = "DPA"
		productInfo["product_image_fields"] = []any{"images_url"}
	}
	if !hasValue(productInfo["product_name_type"]) {
		productInfo["product_name_type"] = "CUSTOM"
	}
	if productInfo["product_name_type"] == "CUSTOM" {
		productInfo["titles"] = []any{productName}
	}
	if !hasValue(productInfo["product_selling_point_type"]) {
		productInfo["product_selling_point_type"] = "CUSTOM"
	}
	if productInfo["product_selling_point_type"] == "CUSTOM" {
		sellingPoints, err := normalizeMarketingSellingPoints(input.SellingPoints)
		if err != nil {
			return nil, err
		}
		productInfo["selling_points"] = sellingPoints
	}
	return productInfo, nil
}

func normalizeMarketingSellingPoints(value any) ([]any, error) {
	values := normalizeMarketingStringList(value)
	result := []any{}
	seen := map[string]bool{}
	for _, raw := range values {
		text := strings.TrimSpace(stringValue(raw))
		if text == "" || seen[text] {
			continue
		}
		positions := 0
		for _, character := range text {
			if character < 128 {
				positions++
			} else {
				positions += 2
			}
		}
		if positions < 12 || positions > 18 {
			return nil, configurationError("product selling point length must be 6-9 positions: "+text, nil)
		}
		seen[text] = true
		result = append(result, text)
	}
	if len(result) == 0 {
		return nil, configurationError("at least one product selling point is required", nil)
	}
	if len(result) > 10 {
		return nil, configurationError("at most 10 product selling points are allowed", nil)
	}
	return result, nil
}

func normalizeMarketingRequiredTitles(value any) ([]any, error) {
	result, err := normalizeMarketingOptionalTitles(value)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, configurationError("at least one non-empty copy title is required", nil)
	}
	return result, nil
}

func normalizeMarketingOptionalTitles(value any) ([]any, error) {
	values := normalizeMarketingStringList(value)
	result := []any{}
	seen := map[string]bool{}
	for _, raw := range values {
		text := strings.TrimSpace(stringValue(raw))
		if text == "" || seen[text] {
			continue
		}
		length := utf8.RuneCountInString(text)
		if length < 5 || length > 30 {
			return nil, configurationError(fmt.Sprintf("copy title length must be 5-30 characters: %s", text), nil)
		}
		seen[text] = true
		result = append(result, text)
	}
	return result, nil
}

func normalizeMarketingStringList(value any) []any {
	switch typed := value.(type) {
	case nil:
		return []any{}
	case string:
		parts := strings.Split(strings.ReplaceAll(typed, "，", ","), ",")
		result := make([]any, 0, len(parts))
		for _, part := range parts {
			result = append(result, part)
		}
		return result
	case []string:
		return anyStrings(typed)
	case []any:
		return clone(typed).([]any)
	default:
		return []any{typed}
	}
}

func setMarketingOptionalField(target map[string]any, section, field, value string, list bool) {
	if strings.TrimSpace(value) == "" {
		return
	}
	nested := cloneMap(mapOrEmpty(target[section]))
	if list {
		nested[field] = []any{value}
	} else {
		nested[field] = value
	}
	target[section] = nested
}

func appendMarketingClearedField(provenance map[string]any, field string) {
	values := listOrEmpty(provenance["cleared_fields"])
	for _, value := range values {
		if value == field {
			return
		}
	}
	values = append(values, field)
	sort.Slice(values, func(left, right int) bool { return stringValue(values[left]) < stringValue(values[right]) })
	provenance["cleared_fields"] = values
}

func deleteMarketingPath(value map[string]any, dotted string) bool {
	parts := strings.Split(dotted, ".")
	current := value
	for _, part := range parts[:len(parts)-1] {
		nested, ok := current[part].(map[string]any)
		if !ok {
			return false
		}
		current = nested
	}
	last := parts[len(parts)-1]
	if _, exists := current[last]; !exists {
		return false
	}
	delete(current, last)
	return true
}

func marketingPath(value map[string]any, dotted string) any {
	var current any = value
	for _, part := range strings.Split(dotted, ".") {
		nested, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = nested[part]
	}
	return current
}

func containsMarketingUnresolved(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		text := strings.TrimSpace(typed)
		lower := strings.ToLower(text)
		return text == "" || strings.Contains(lower, "example.com") ||
			strings.Contains(lower, "replace_with") || strings.Contains(lower, "todo") ||
			strings.Contains(text, "待填") || strings.Contains(text, "待反查")
	case []any:
		if len(typed) == 0 {
			return true
		}
		for _, item := range typed {
			if containsMarketingUnresolved(item) {
				return true
			}
		}
		return false
	case map[string]any:
		for _, item := range typed {
			if containsMarketingUnresolved(item) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func flattenMarketing(value any, prefix string, result map[string]any) {
	if mapped, ok := value.(map[string]any); ok {
		for key, item := range mapped {
			dotted := key
			if prefix != "" {
				dotted = prefix + "." + key
			}
			flattenMarketing(item, dotted, result)
		}
		return
	}
	if prefix != "" {
		result[prefix] = clone(value)
	}
}

func anyStrings(values []string) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}

func stringValues(value any) []string {
	values := listOrEmpty(value)
	result := make([]string, 0, len(values))
	for _, item := range values {
		result = append(result, stringValue(item))
	}
	return result
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
