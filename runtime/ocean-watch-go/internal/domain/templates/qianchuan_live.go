package templates

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

const (
	qianchuanLiveSchemaVersion    = 1
	qianchuanLiveSchemaVersionKey = "qianchuan_live_template_schema_version"
	qianchuanLiveDefaultKey       = "default_qianchuan_live_template"
	qianchuanLiveTemplatesKey     = "qianchuan_live_templates"
	qianchuanLiveTemplateType     = "QIANCHUAN_LIVE_ALL_DOMAIN"
	qianchuanLiveMaterialSource   = "LIVE_SMART_SELECTION"
)

func defaultQianchuanLiveTemplate() map[string]any {
	return map[string]any{
		"template_type":   qianchuanLiveTemplateType,
		"business_usable": false,
		"bindings": map[string]any{
			"channel":       "qianchuan",
			"advertiser_id": "REPLACE_WITH_ADVERTISER_ID",
			"creator_name":  "REPLACE_WITH_CREATOR_NAME",
			"aweme_id":      "REPLACE_WITH_AWEME_ID",
		},
		"delivery_setting": map[string]any{
			"smart_bid_type":      "SMART_BID_CONSERVATIVE",
			"budget":              5000,
			"live_schedule_type":  "SCHEDULE_FROM_NOW",
			"daily_delivery_time": 8.5,
		},
		"creative_setting": map[string]any{"smart_select_material": true},
		"material_strategy": map[string]any{
			"source_type":          qianchuanLiveMaterialSource,
			"persist_material_ids": false,
		},
	}
}

func ensureQianchuanLiveConfig(config map[string]any) (map[string]any, error) {
	normalized := cloneMap(config)
	version, err := parseVersion(normalized[qianchuanLiveSchemaVersionKey], 1, qianchuanLiveSchemaVersionKey)
	if err != nil {
		return nil, err
	}
	if version > qianchuanLiveSchemaVersion {
		return nil, configurationError(fmt.Sprintf(
			"Qianchuan live template schema %d is newer than supported %d",
			version, qianchuanLiveSchemaVersion,
		), nil)
	}
	normalized[qianchuanLiveSchemaVersionKey] = qianchuanLiveSchemaVersion
	if _, exists := normalized[qianchuanLiveDefaultKey]; !exists {
		normalized[qianchuanLiveDefaultKey] = defaultQianchuanLiveTemplate()
	}
	if _, exists := normalized[qianchuanLiveTemplatesKey]; !exists {
		normalized[qianchuanLiveTemplatesKey] = map[string]any{}
	}
	return normalized, nil
}

func validateQianchuanLiveDelivery(value any) (map[string]any, error) {
	setting, ok := value.(map[string]any)
	if !ok {
		return nil, configurationError("delivery_setting must be an object", nil)
	}
	allowed := map[string]struct{}{
		"smart_bid_type": {}, "roi2_goal": {}, "budget": {}, "live_schedule_type": {},
		"start_time": {}, "end_time": {}, "daily_delivery_time": {},
		"deep_external_action": {}, "qcpx_mode": {},
	}
	unknown := make([]string, 0)
	for key := range setting {
		if _, exists := allowed[key]; !exists {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	if len(unknown) != 0 {
		return nil, configurationError("unsupported Qianchuan live delivery fields", map[string]any{"fields": stringsToAny(unknown)})
	}
	result := cloneMap(setting)
	bidType := result["smart_bid_type"]
	if bidType != "SMART_BID_CUSTOM" && bidType != "SMART_BID_CONSERVATIVE" {
		return nil, configurationError("invalid live smart_bid_type", nil)
	}
	budget, err := liveDecimal(result["budget"], "delivery_setting.budget")
	if err != nil {
		return nil, err
	}
	result["budget"] = DecimalFloat64(budget)
	if bidType == "SMART_BID_CUSTOM" {
		roi, roiErr := liveDecimal(result["roi2_goal"], "delivery_setting.roi2_goal")
		if roiErr != nil {
			return nil, roiErr
		}
		result["roi2_goal"] = DecimalFloat64(roi)
		if result["daily_delivery_time"] != nil {
			return nil, configurationError("daily_delivery_time is supported only for SMART_BID_CONSERVATIVE", nil)
		}
	} else if result["roi2_goal"] != nil {
		return nil, configurationError("SMART_BID_CONSERVATIVE must not set roi2_goal", nil)
	}
	if durationValue := result["daily_delivery_time"]; durationValue != nil {
		duration, parseErr := decimalNumber(durationValue)
		if parseErr != nil {
			return nil, configurationError("daily_delivery_time must be a number", nil)
		}
		if duration < 0.5 || duration > 24 {
			return nil, configurationError("daily_delivery_time must be between 0.5 and 24", nil)
		}
		if math.Mod(duration, 0.5) != 0 {
			return nil, configurationError("daily_delivery_time must use 0.5-hour steps", nil)
		}
		result["daily_delivery_time"] = DecimalFloat64(duration)
	}
	if result["live_schedule_type"] != "SCHEDULE_FROM_NOW" && result["live_schedule_type"] != "SCHEDULE_START_END" {
		return nil, configurationError("invalid live_schedule_type", nil)
	}
	if value := result["deep_external_action"]; value != nil && value != "AD_CONVERT_TYPE_LIVE_PAY_ROI" && value != "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI" {
		return nil, configurationError("invalid live deep_external_action", nil)
	}
	if value := result["qcpx_mode"]; value != nil && value != "QCPX_MODE_OFF" && value != "QCPX_MODE_ON" {
		return nil, configurationError("invalid live qcpx_mode", nil)
	}
	return result, nil
}

func validateQianchuanLiveCreative(value any) (map[string]any, error) {
	setting, ok := value.(map[string]any)
	if !ok {
		return nil, configurationError("creative_setting must be an object", nil)
	}
	if len(setting) != 1 {
		return nil, configurationError("live creative_setting supports only smart_select_material", nil)
	}
	if _, exists := setting["smart_select_material"]; !exists {
		return nil, configurationError("live creative_setting supports only smart_select_material", nil)
	}
	if !exactTrue(setting["smart_select_material"]) {
		return nil, configurationError("material-free live templates require smart_select_material true", nil)
	}
	return cloneMap(setting), nil
}

func validateQianchuanLiveTemplate(value any) (map[string]any, error) {
	template, ok := value.(map[string]any)
	if !ok || template["template_type"] != qianchuanLiveTemplateType {
		return nil, configurationError("invalid Qianchuan live template type", nil)
	}
	bindings := mapOrEmpty(template["bindings"])
	if bindings["channel"] != "qianchuan" {
		return nil, configurationError("Qianchuan live template channel must be qianchuan", nil)
	}
	advertiserID, err := positiveID(bindings["advertiser_id"], "advertiser_id")
	if err != nil {
		return nil, err
	}
	creatorName, err := requiredText(bindings["creator_name"], "creator_name")
	if err != nil {
		return nil, err
	}
	awemeID, err := positiveID(bindings["aweme_id"], "aweme_id")
	if err != nil {
		return nil, err
	}
	templateID, err := requiredText(template["template_id"], "template_id")
	if err != nil {
		return nil, err
	}
	deliverySource := template["delivery_setting"]
	if !hasValue(deliverySource) {
		deliverySource = defaultQianchuanLiveTemplate()["delivery_setting"]
	}
	delivery, err := validateQianchuanLiveDelivery(deliverySource)
	if err != nil {
		return nil, err
	}
	creativeSource := template["creative_setting"]
	if !hasValue(creativeSource) {
		creativeSource = defaultQianchuanLiveTemplate()["creative_setting"]
	}
	creative, err := validateQianchuanLiveCreative(creativeSource)
	if err != nil {
		return nil, err
	}
	status := "inactive"
	if template["status"] == "active" {
		status = "active"
	}
	normalized := map[string]any{
		"template_id":   templateID,
		"display_name":  qianchuanLiveDisplayName(advertiserID, creatorName, awemeID),
		"template_type": qianchuanLiveTemplateType,
		"status":        status,
		"bindings": map[string]any{
			"channel":       "qianchuan",
			"advertiser_id": advertiserID,
			"creator_name":  creatorName,
			"aweme_id":      awemeID,
		},
		"delivery_setting": delivery,
		"creative_setting": creative,
		"material_strategy": map[string]any{
			"source_type":          qianchuanLiveMaterialSource,
			"persist_material_ids": false,
		},
	}
	if template["display_name"] != normalized["display_name"] {
		return nil, configurationError("Qianchuan live template display_name is inconsistent", nil)
	}
	strategy := mapOrEmpty(template["material_strategy"])
	if len(strategy) != 2 || strategy["source_type"] != qianchuanLiveMaterialSource || !exactFalse(strategy["persist_material_ids"]) {
		return nil, configurationError("invalid Qianchuan live material strategy", nil)
	}
	return normalized, nil
}

func listQianchuanLiveTemplates(config map[string]any) ([]map[string]any, map[string]any, error) {
	normalized, err := ensureQianchuanLiveConfig(config)
	if err != nil {
		return nil, nil, err
	}
	templates := mapOrEmpty(normalized[qianchuanLiveTemplatesKey])
	rows := make([]map[string]any, 0, len(templates))
	for _, templateID := range sortedKeys(templates) {
		template, validateErr := validateQianchuanLiveTemplate(templates[templateID])
		if validateErr != nil {
			return nil, nil, validateErr
		}
		if template["template_id"] != templateID {
			return nil, nil, configurationError("Qianchuan live template key does not match template_id", nil)
		}
		rows = append(rows, template)
	}
	return rows, normalized, nil
}

func resolveQianchuanLiveTemplate(config map[string]any, selector string) (map[string]any, error) {
	normalized, err := ensureQianchuanLiveConfig(config)
	if err != nil {
		return nil, err
	}
	templates := mapOrEmpty(normalized[qianchuanLiveTemplatesKey])
	if value, exists := templates[selector]; exists {
		return validateQianchuanLiveTemplate(value)
	}
	matches := make([]map[string]any, 0)
	for _, value := range templates {
		template := mapOrEmpty(value)
		if template["display_name"] == selector {
			validated, validateErr := validateQianchuanLiveTemplate(value)
			if validateErr != nil {
				return nil, validateErr
			}
			matches = append(matches, validated)
		}
	}
	if len(matches) == 0 {
		return nil, configurationError("Qianchuan live template not found", map[string]any{"selector": selector})
	}
	if len(matches) > 1 {
		return nil, configurationError("Qianchuan live template name is ambiguous; use template_id", nil)
	}
	return matches[0], nil
}

func liveDecimal(value any, field string) (float64, error) {
	parsed, err := decimalNumber(value)
	if err != nil {
		return 0, configurationError(field+" must be a number", nil)
	}
	exponent, exponentErr := decimalExponent(value)
	if exponentErr != nil || parsed <= 0 || exponent < -2 {
		return 0, configurationError(field+" must be positive with at most two decimals", nil)
	}
	return parsed, nil
}

func decimalNumber(value any) (float64, error) {
	parsed, err := strconv.ParseFloat(stringValue(value), 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("invalid number")
	}
	return parsed, nil
}

func qianchuanLiveDisplayName(advertiserID, creatorName, awemeID string) string {
	return strings.Join([]string{"巨量千川", advertiserID, creatorName, awemeID, "直播全域"}, "-")
}
