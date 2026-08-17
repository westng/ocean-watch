package templates

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	qianchuanProductSchemaVersion    = 8
	qianchuanProductSchemaVersionKey = "qianchuan_product_template_schema_version"
	qianchuanProductDefaultKey       = "default_qianchuan_product_template"
	qianchuanProductTemplatesKey     = "qianchuan_product_templates"
	qianchuanProductTemplateType     = "QIANCHUAN_PRODUCT_ALL_DOMAIN"
	qianchuanProductMaterialSource   = "CREATOR_RUNTIME_QUERY"
	qianchuanProductDefaultPlanName  = "{month_day}-{creator_name}-{product_short_name}-{type}-{business}"
)

var qianchuanProductIDSeparator = regexp.MustCompile(`[/,，\s]+`)
var qianchuanPlanNamePlaceholder = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func defaultQianchuanProductTemplate() map[string]any {
	return map[string]any{
		"template_type":   qianchuanProductTemplateType,
		"business_usable": false,
		"bindings": map[string]any{
			"channel":            "qianchuan",
			"advertiser_id":      "REPLACE_WITH_ADVERTISER_ID",
			"product_name":       "REPLACE_WITH_PRODUCT_NAME",
			"product_short_name": "REPLACE_WITH_PRODUCT_SHORT_NAME",
			"product_ids":        []any{},
		},
		"delivery_setting": map[string]any{
			"smart_bid_type":       "SMART_BID_CUSTOM",
			"roi2_goal":            1.7,
			"qcpx_mode":            "QCPX_MODE_ON",
			"budget":               5000,
			"video_schedule_type":  "SCHEDULE_FROM_NOW",
			"deep_external_action": "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI",
		},
		"plan_name_template": qianchuanProductDefaultPlanName,
		"material_strategy": map[string]any{
			"source_type":          qianchuanProductMaterialSource,
			"persist_material_ids": false,
		},
	}
}

func ensureQianchuanProductConfig(config map[string]any) (map[string]any, error) {
	normalized := cloneMap(config)
	_, hasVersion := normalized[qianchuanProductSchemaVersionKey]
	_, hasDefault := normalized[qianchuanProductDefaultKey]
	_, hasTemplates := normalized[qianchuanProductTemplatesKey]
	if !hasVersion && !hasDefault && !hasTemplates {
		normalized[qianchuanProductSchemaVersionKey] = qianchuanProductSchemaVersion
		normalized[qianchuanProductDefaultKey] = defaultQianchuanProductTemplate()
		normalized[qianchuanProductTemplatesKey] = map[string]any{}
	}
	if err := requireTemplateSchema(normalized, qianchuanProductSchemaVersionKey, qianchuanProductSchemaVersion, "Qianchuan product"); err != nil {
		return nil, err
	}
	if _, exists := normalized[qianchuanProductDefaultKey]; !exists {
		normalized[qianchuanProductDefaultKey] = defaultQianchuanProductTemplate()
	} else if _, ok := normalized[qianchuanProductDefaultKey].(map[string]any); !ok {
		return nil, configurationError("default_qianchuan_product_template must be an object", nil)
	}
	if _, exists := normalized[qianchuanProductTemplatesKey]; !exists {
		normalized[qianchuanProductTemplatesKey] = map[string]any{}
	} else if _, ok := normalized[qianchuanProductTemplatesKey].(map[string]any); !ok {
		return nil, configurationError("qianchuan_product_templates must be an object", nil)
	}
	return normalized, nil
}

func validateQianchuanProductDelivery(value any) (map[string]any, error) {
	setting, ok := value.(map[string]any)
	if !ok {
		return nil, configurationError("delivery_setting must be an object", nil)
	}
	expected := map[string]any{
		"smart_bid_type":       "SMART_BID_CUSTOM",
		"roi2_goal":            1.7,
		"qcpx_mode":            "QCPX_MODE_ON",
		"budget":               5000,
		"video_schedule_type":  "SCHEDULE_FROM_NOW",
		"deep_external_action": "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI",
	}
	unknown := make([]string, 0)
	for key := range setting {
		if _, exists := expected[key]; !exists {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	if len(unknown) != 0 {
		return nil, configurationError(
			"Qianchuan product template contains unsupported delivery fields",
			map[string]any{"fields": stringsToAny(unknown)},
		)
	}
	missing := make([]any, 0)
	for _, key := range []string{
		"smart_bid_type", "roi2_goal", "qcpx_mode", "budget", "video_schedule_type", "deep_external_action",
	} {
		if setting[key] == nil {
			missing = append(missing, key)
		}
	}
	if len(missing) != 0 {
		return nil, configurationError(
			"Qianchuan product template is missing delivery fields",
			map[string]any{"fields": missing},
		)
	}
	if setting["smart_bid_type"] != "SMART_BID_CUSTOM" {
		return nil, configurationError("smart_bid_type must be SMART_BID_CUSTOM", nil)
	}
	for _, field := range []string{"roi2_goal", "budget"} {
		parsed, parseErr := finiteNumber(setting[field])
		if parseErr != nil {
			return nil, configurationError("delivery_setting."+field+" must be a number", nil)
		}
		if parsed <= 0 {
			return nil, configurationError("delivery_setting."+field+" must be greater than zero", nil)
		}
		exponent, exponentErr := decimalExponent(setting[field])
		if exponentErr != nil {
			return nil, configurationError("delivery_setting."+field+" must be a number", nil)
		}
		if exponent < -2 {
			return nil, configurationError("delivery_setting."+field+" supports at most two decimal places", nil)
		}
	}
	if setting["qcpx_mode"] != "QCPX_MODE_ON" {
		return nil, configurationError("qcpx_mode must be QCPX_MODE_ON", nil)
	}
	if setting["video_schedule_type"] != "SCHEDULE_FROM_NOW" {
		return nil, configurationError("video_schedule_type must be SCHEDULE_FROM_NOW", nil)
	}
	if setting["deep_external_action"] != "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI" {
		return nil, configurationError("deep_external_action must be AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI", nil)
	}
	return cloneMap(setting), nil
}

func normalizeQianchuanProductIDs(value any) ([]any, error) {
	var inputs []any
	if text, ok := value.(string); ok {
		inputs = []any{text}
	} else {
		inputs = listOrEmpty(value)
	}
	parts := make([]string, 0)
	for _, input := range inputs {
		for _, part := range qianchuanProductIDSeparator.Split(stringValue(input), -1) {
			if part = strings.TrimSpace(part); part != "" {
				parts = append(parts, part)
			}
		}
	}
	result := make([]any, 0, len(parts))
	seen := map[string]struct{}{}
	for index, part := range parts {
		productID, err := positiveID(part, fmt.Sprintf("product_ids[%d]", index))
		if err != nil {
			return nil, err
		}
		if _, exists := seen[productID]; !exists {
			seen[productID] = struct{}{}
			result = append(result, productID)
		}
	}
	if len(result) == 0 {
		return nil, configurationError("product_ids must contain at least one product", nil)
	}
	if len(result) > 30 {
		return nil, configurationError("product_ids supports at most 30 products", map[string]any{"product_count": len(result)})
	}
	return result, nil
}

func validateQianchuanPlanNameTemplate(value any) (string, error) {
	template, err := requiredText(value, "plan_name_template")
	if err != nil {
		return "", err
	}
	allowed := map[string]bool{
		"product_name": true, "product_short_name": true, "creator_name": true, "aweme_id": true,
		"douyin_id": true, "date": true, "time": true, "datetime": true,
		"month_day": true, "type": true, "business": true,
	}
	unknown := []string{}
	seenUnknown := map[string]bool{}
	for _, match := range qianchuanPlanNamePlaceholder.FindAllStringSubmatch(template, -1) {
		if !allowed[match[1]] && !seenUnknown[match[1]] {
			seenUnknown[match[1]] = true
			unknown = append(unknown, match[1])
		}
	}
	sort.Strings(unknown)
	if len(unknown) != 0 {
		return "", configurationError(
			"Qianchuan product plan_name_template contains unsupported placeholders",
			map[string]any{"placeholders": stringsToAny(unknown)},
		)
	}
	return template, nil
}

func validateQianchuanProductTemplate(value any) (map[string]any, error) {
	template, ok := value.(map[string]any)
	if !ok {
		return nil, configurationError("Qianchuan product template must be an object", nil)
	}
	if template["template_type"] != qianchuanProductTemplateType {
		return nil, configurationError("invalid Qianchuan product template type", nil)
	}
	bindings := mapOrEmpty(template["bindings"])
	if bindings["channel"] != "qianchuan" {
		return nil, configurationError("Qianchuan product template channel must be qianchuan", nil)
	}
	advertiserID, err := positiveID(bindings["advertiser_id"], "advertiser_id")
	if err != nil {
		return nil, err
	}
	productName, err := requiredText(bindings["product_name"], "product_name")
	if err != nil {
		return nil, err
	}
	productShortName, err := requiredText(bindings["product_short_name"], "product_short_name")
	if err != nil {
		return nil, err
	}
	productIDs, err := normalizeQianchuanProductIDs(bindings["product_ids"])
	if err != nil {
		return nil, err
	}
	templateID, err := requiredText(template["template_id"], "template_id")
	if err != nil {
		return nil, err
	}
	displayName, err := requiredText(template["display_name"], "template_name")
	if err != nil {
		return nil, err
	}
	planNameTemplate, err := validateQianchuanPlanNameTemplate(template["plan_name_template"])
	if err != nil {
		return nil, err
	}
	deliverySource := template["delivery_setting"]
	if !hasValue(deliverySource) {
		deliverySource = defaultQianchuanProductTemplate()["delivery_setting"]
	}
	delivery, err := validateQianchuanProductDelivery(deliverySource)
	if err != nil {
		return nil, err
	}
	status := "inactive"
	if template["status"] == "active" {
		status = "active"
	}
	normalized := map[string]any{
		"template_id":   templateID,
		"display_name":  displayName,
		"template_type": qianchuanProductTemplateType,
		"status":        status,
		"bindings": map[string]any{
			"channel":            "qianchuan",
			"advertiser_id":      advertiserID,
			"product_name":       productName,
			"product_short_name": productShortName,
			"product_ids":        productIDs,
		},
		"delivery_setting":   delivery,
		"plan_name_template": planNameTemplate,
		"material_strategy": map[string]any{
			"source_type":          qianchuanProductMaterialSource,
			"persist_material_ids": false,
		},
	}
	if template["display_name"] != normalized["display_name"] {
		return nil, configurationError("Qianchuan product template display_name is inconsistent", nil)
	}
	strategy := mapOrEmpty(template["material_strategy"])
	if strategy["source_type"] != qianchuanProductMaterialSource {
		return nil, configurationError("invalid Qianchuan product material strategy", nil)
	}
	if !exactFalse(strategy["persist_material_ids"]) {
		return nil, configurationError("Qianchuan product templates cannot persist material IDs", nil)
	}
	if forbidden := qianchuanForbiddenKey(template); forbidden != "" {
		return nil, configurationError(
			"Qianchuan product template contains runtime or unsupported fields",
			map[string]any{"field": forbidden},
		)
	}
	return normalized, nil
}

func listQianchuanProductTemplates(config map[string]any) ([]map[string]any, map[string]any, error) {
	normalized, err := ensureQianchuanProductConfig(config)
	if err != nil {
		return nil, nil, err
	}
	templates := mapOrEmpty(normalized[qianchuanProductTemplatesKey])
	rows := make([]map[string]any, 0, len(templates))
	for _, templateID := range sortedKeys(templates) {
		template, validateErr := validateQianchuanProductTemplate(templates[templateID])
		if validateErr != nil {
			return nil, nil, validateErr
		}
		if template["template_id"] != templateID {
			return nil, nil, configurationError(
				"Qianchuan product template key does not match template_id",
				map[string]any{"key": templateID, "template_id": template["template_id"]},
			)
		}
		rows = append(rows, template)
	}
	return rows, normalized, nil
}

func resolveQianchuanProductTemplate(config map[string]any, selector string) (map[string]any, error) {
	normalized, err := ensureQianchuanProductConfig(config)
	if err != nil {
		return nil, err
	}
	templates := mapOrEmpty(normalized[qianchuanProductTemplatesKey])
	if isMissing(selector) {
		return nil, configurationError("an explicit Qianchuan product template is required", nil)
	}
	if value, exists := templates[selector]; exists {
		return validateQianchuanProductTemplate(value)
	}
	matches := make([]map[string]any, 0)
	for _, value := range templates {
		template := mapOrEmpty(value)
		if template["display_name"] == selector {
			validated, validateErr := validateQianchuanProductTemplate(value)
			if validateErr != nil {
				return nil, validateErr
			}
			matches = append(matches, validated)
		}
	}
	if len(matches) == 0 {
		return nil, configurationError("Qianchuan product template not found", map[string]any{"selector": selector})
	}
	if len(matches) > 1 {
		return nil, configurationError("Qianchuan product template name is ambiguous; use template_id", map[string]any{"selector": selector})
	}
	return matches[0], nil
}

type QianchuanProductBinding struct {
	TemplateID       string
	DisplayName      string
	AdvertiserID     string
	ProductName      string
	ProductShortName string
	ProductIDs       []string
	Active           bool
}

func ResolveQianchuanProductBinding(config map[string]any, selector string) (QianchuanProductBinding, error) {
	template, err := resolveQianchuanProductTemplate(config, selector)
	if err != nil {
		return QianchuanProductBinding{}, err
	}
	bindings := mapOrEmpty(template["bindings"])
	return QianchuanProductBinding{
		TemplateID: stringValue(template["template_id"]), DisplayName: stringValue(template["display_name"]),
		AdvertiserID: stringValue(bindings["advertiser_id"]), ProductName: stringValue(bindings["product_name"]),
		ProductShortName: stringValue(bindings["product_short_name"]),
		ProductIDs:       stringList(listOrEmpty(bindings["product_ids"])), Active: template["status"] == "active",
	}, nil
}

func qianchuanProductDisplayName(advertiserID, productName string, productIDs []string) string {
	return strings.Join([]string{
		"巨量千川", strings.TrimSpace(advertiserID), strings.TrimSpace(productName),
		strings.Join(productIDs, "/"), "商品全域",
	}, "-")
}

func qianchuanForbiddenKey(value any) string {
	forbidden := map[string]struct{}{
		"aweme_id": {}, "aweme_item_id": {}, "channel_id": {}, "channel_type": {},
		"image_ids": {}, "multi_product_creative_list": {}, "product_channel_info": {}, "video_id": {},
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if _, exists := forbidden[key]; exists {
				return key
			}
			if found := qianchuanForbiddenKey(nested); found != "" {
				return found
			}
		}
	case []any:
		for _, nested := range typed {
			if found := qianchuanForbiddenKey(nested); found != "" {
				return found
			}
		}
	}
	return ""
}

func finiteNumber(value any) (float64, error) {
	return decimalNumber(value)
}

func stringList(values []any) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = stringValue(value)
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
