package templates

import (
	"sort"
	"strconv"
	"strings"
)

var marketingTemplateSections = []string{
	"defaults", "materials", "resolved_ids", "links", "tracking_urls",
}

var marketingRequiredBindings = []string{
	"channel", "advertiser_id", "platform", "traffic_source", "product_id", "product_name",
}

func marketingChannel(config map[string]any, includeDetails bool) (map[string]any, error) {
	listed, err := listMarketingTemplates(config)
	if err != nil {
		return nil, err
	}
	rows := make([]any, 0, len(listed))
	for _, row := range listed {
		if includeDetails {
			detail := cloneMap(row)
			detail["channel"] = "marketing"
			rows = append(rows, detail)
		} else {
			rows = append(rows, compactMarketingTemplate(row))
		}
	}
	base := marketingDefaultTemplateSummary(config)
	var skeleton map[string]any
	if includeDetails {
		skeleton = cloneMap(base)
		skeleton["channel"] = "marketing"
	} else {
		skeleton = map[string]any{
			"channel":         "marketing",
			"name":            base["name"],
			"business_usable": false,
		}
	}
	return map[string]any{
		"channel":                 "marketing",
		"display_name":            "巨量营销",
		"business_template_count": len(rows),
		"default_skeleton_count":  1,
		"default_skeleton":        skeleton,
		"templates":               rows,
	}, nil
}

func compactMarketingTemplate(row map[string]any) map[string]any {
	delivery := mapOrEmpty(row["delivery_settings"])
	sourceType := stringValue(row["material_source_type"])
	copyMaterials := mapOrEmpty(row["copy_materials"])
	return map[string]any{
		"channel":                 "marketing",
		"name":                    row["name"],
		"advertiser_id":           row["advertiser_id"],
		"product_name":            row["product_name"],
		"product_id":              row["product_id"],
		"template_type":           map[bool]string{true: "混剪素材", false: "原生素材"}[sourceType == "ACCOUNT_UPLOAD"],
		"material_source_type":    row["material_source_type"],
		"platform":                row["platform"],
		"traffic_source":          row["traffic_source"],
		"daily_budget":            delivery["daily_budget"],
		"roi_goal":                delivery["roi_goal"],
		"gender":                  delivery["gender"],
		"ages":                    delivery["ages"],
		"copy_title_count":        copyMaterials["title_count"],
		"ready_for_plan_creation": row["binding_error"] == nil,
	}
}

func listMarketingTemplates(config map[string]any) ([]map[string]any, error) {
	rawTemplates := mapOrEmpty(config["plan_templates"])
	rows := make([]map[string]any, 0, len(rawTemplates))
	for _, name := range sortedKeys(rawTemplates) {
		template, ok := rawTemplates[name].(map[string]any)
		if !ok {
			return nil, configurationError("Marketing plan template must be an object", map[string]any{"template": name})
		}
		normalized, err := normalizeMarketingTemplate(config, name, template)
		if err != nil {
			return nil, err
		}
		bindings := mapOrEmpty(normalized["bindings"])
		strategy := mapOrEmpty(normalized["material_strategy"])
		overrides := mapOrEmpty(normalized["overrides"])
		base := marketingDefaultBundle(config)
		effectiveDefaults := deepMerge(mapOrEmpty(base["defaults"]), mapOrEmpty(overrides["defaults"]))
		productInfo := mapOrEmpty(effectiveDefaults["product_info"])
		copyMaterials := mapOrEmpty(normalized["copy_materials"])
		titles := listOrEmpty(copyMaterials["titles"])
		resolvedIDs := mapOrEmpty(overrides["resolved_ids"])
		productImageIDs := listOrEmpty(resolvedIDs["product_image_ids"])
		ages := listOrEmpty(effectiveDefaults["ages"])
		imageFields := listOrEmpty(productInfo["product_image_fields"])
		row := map[string]any{
			"name":              name,
			"channel":           bindings["channel"],
			"advertiser_id":     bindings["advertiser_id"],
			"platform":          bindings["platform"],
			"traffic_source":    bindings["traffic_source"],
			"product_id":        bindings["product_id"],
			"product_name":      bindings["product_name"],
			"product_image_ids": clone(productImageIDs),
			"product_image": map[string]any{
				"type":                      productInfo["product_image_type"],
				"fields":                    clone(imageFields),
				"manual_image_ids_required": productInfo["product_image_type"] != "DPA",
			},
			"delivery_settings": map[string]any{
				"daily_budget": effectiveDefaults["daily_budget"],
				"roi_goal":     effectiveDefaults["roi_goal"],
				"gender":       effectiveDefaults["gender"],
				"ages":         clone(ages),
			},
			"material_source_type": strategy["source_type"],
			"material_source_name": marketingMaterialSourceLabel(strategy["source_type"]),
			"material_strategy":    clone(strategy),
			"copy_materials": map[string]any{
				"configured":           len(titles) != 0,
				"title_count":          len(titles),
				"titles":               clone(titles),
				"copied_from_template": copyMaterials["copied_from_template"],
			},
			"bindings":      clone(bindings),
			"legacy":        normalized["legacy"],
			"binding_error": marketingBindingError(bindings),
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func MarketingGuideRows(config map[string]any) ([]any, error) {
	listed, err := listMarketingTemplates(config)
	if err != nil {
		return nil, err
	}
	rows := make([]any, 0, len(listed))
	for _, template := range listed {
		copyMaterials := mapOrEmpty(template["copy_materials"])
		titles := listOrEmpty(copyMaterials["titles"])
		rows = append(rows, map[string]any{
			"name":                      template["name"],
			"channel":                   template["channel"],
			"advertiser_id":             template["advertiser_id"],
			"platform":                  template["platform"],
			"product_id":                template["product_id"],
			"product_name":              template["product_name"],
			"material_source_type":      template["material_source_type"],
			"material_strategy_error":   nullableMarketingStrategyError(template["material_strategy"]),
			"copy_materials_configured": len(titles) != 0,
			"copy_title_count":          len(titles),
			"binding_error":             template["binding_error"],
		})
	}
	return rows, nil
}

func nullableMarketingStrategyError(value any) any {
	if message := marketingMaterialStrategyError(value); message != "" {
		return message
	}
	return nil
}

func normalizeMarketingTemplate(config map[string]any, name string, template map[string]any) (map[string]any, error) {
	schemaVersion, err := parseVersion(config["plan_template_schema_version"], 1, "plan_template_schema_version")
	if err != nil {
		return nil, err
	}
	_, hasBindings := template["bindings"]
	_, hasOverrides := template["overrides"]
	if hasBindings || hasOverrides {
		overrides := cloneMap(mapOrEmpty(template["overrides"]))
		legacyTitles, hasLegacyTitles := overrides["titles"]
		delete(overrides, "titles")
		copyMaterials := cloneMap(mapOrEmpty(template["copy_materials"]))
		if _, exists := copyMaterials["titles"]; hasLegacyTitles && !exists {
			copyMaterials["titles"] = clone(legacyTitles)
		}
		bindings := cloneMap(mapOrEmpty(template["bindings"]))
		if _, exists := bindings["channel"]; !exists {
			channel := mapOrEmpty(config["account"])["channel"]
			if !pythonTruthy(channel) {
				channel = config["default_channel"]
			}
			if !pythonTruthy(channel) {
				channel = "marketing"
			}
			bindings["channel"] = channel
		}
		strategy := cloneMap(mapOrEmpty(template["material_strategy"]))
		if len(strategy) == 0 && schemaVersion < 3 {
			strategy, err = legacyMarketingMaterialStrategy(config, template)
			if err != nil {
				return nil, err
			}
		}
		displayName := any(name)
		if value, exists := template["display_name"]; exists {
			displayName = value
		}
		return map[string]any{
			"name":              name,
			"display_name":      displayName,
			"bindings":          bindings,
			"copy_materials":    copyMaterials,
			"material_strategy": strategy,
			"created_from":      clone(template["created_from"]),
			"overrides":         overrides,
			"legacy":            false,
		}, nil
	}
	overrides := map[string]any{}
	for _, section := range marketingTemplateSections {
		if value, exists := template[section]; exists {
			overrides[section] = clone(value)
		}
	}
	if value, exists := template["titles"]; exists {
		overrides["titles"] = clone(value)
	}
	strategy, err := legacyMarketingMaterialStrategy(config, map[string]any{"overrides": overrides})
	if err != nil {
		return nil, err
	}
	displayName := any(name)
	if value, exists := template["display_name"]; exists {
		displayName = value
	}
	return map[string]any{
		"name":              name,
		"display_name":      displayName,
		"bindings":          legacyMarketingBindings(config, template),
		"copy_materials":    map[string]any{"titles": clone(listOrEmpty(template["titles"]))},
		"material_strategy": strategy,
		"created_from":      clone(template["created_from"]),
		"overrides":         overrides,
		"legacy":            true,
	}, nil
}

func legacyMarketingMaterialStrategy(config, template map[string]any) (map[string]any, error) {
	overrides := mapOrEmpty(template["overrides"])
	maximum := mapOrEmpty(overrides["defaults"])["max_videos_per_project"]
	if maximum == nil {
		maximum = mapOrEmpty(mapOrEmpty(config["default_plan_template"])["defaults"])["max_videos_per_project"]
	}
	if maximum == nil {
		defaults := mapOrEmpty(config["defaults"])
		if value, exists := defaults["max_videos_per_project"]; exists {
			maximum = value
		} else {
			maximum = 5
		}
	}
	if !pythonTruthy(maximum) {
		maximum = 5
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(stringValue(maximum)))
	if err != nil {
		return nil, configurationError("max_videos_per_project must be an integer", nil)
	}
	return map[string]any{
		"source_type":            "ACCOUNT_UPLOAD",
		"selection_mode":         "MANUAL",
		"max_materials_per_unit": parsed,
	}, nil
}

func legacyMarketingBindings(config, template map[string]any) map[string]any {
	defaults := mapOrEmpty(template["defaults"])
	channel := mapOrEmpty(config["account"])["channel"]
	if !pythonTruthy(channel) {
		channel = "marketing"
	}
	productID := template["product_id"]
	if !pythonTruthy(productID) {
		productID = defaults["product_id"]
	}
	productName := defaults["product_name"]
	if !pythonTruthy(productName) {
		productName = template["product_label"]
	}
	return map[string]any{
		"channel":        channel,
		"advertiser_id":  mapOrEmpty(config["account"])["advertiser_id"],
		"platform":       template["platform"],
		"traffic_source": template["traffic_source"],
		"product_id":     productID,
		"product_name":   productName,
	}
}

func marketingDefaultBundle(config map[string]any) map[string]any {
	if configured, ok := config["default_plan_template"].(map[string]any); ok {
		return cloneMap(configured)
	}
	bundle := map[string]any{}
	for _, section := range marketingTemplateSections {
		bundle[section] = cloneMap(mapOrEmpty(config[section]))
	}
	bundle["titles"] = clone(listOrEmpty(config["titles"]))
	return bundle
}

func marketingDefaultTemplateSummary(config map[string]any) map[string]any {
	base := marketingDefaultBundle(config)
	defaults := mapOrEmpty(base["defaults"])
	productInfo := mapOrEmpty(defaults["product_info"])
	resolvedIDs := mapOrEmpty(base["resolved_ids"])
	sections := make([]string, 0, len(base))
	for key, value := range base {
		if pythonTruthy(value) {
			sections = append(sections, key)
		}
	}
	sort.Strings(sections)
	return map[string]any{
		"name":                         "default_plan_template",
		"type":                         "creation_base_only",
		"business_usable":              false,
		"selectable_for_plan_creation": false,
		"purpose":                      "Base configuration for the business-template creation wizard.",
		"delivery_settings": map[string]any{
			"daily_budget": defaults["daily_budget"],
			"roi_goal":     defaults["roi_goal"],
			"gender":       defaults["gender"],
			"ages":         clone(listOrEmpty(defaults["ages"])),
		},
		"product_image": map[string]any{
			"type":                      productInfo["product_image_type"],
			"fields":                    clone(listOrEmpty(productInfo["product_image_fields"])),
			"manual_image_ids_required": productInfo["product_image_type"] != "DPA",
		},
		"regions": map[string]any{
			"city_count": len(listOrEmpty(resolvedIDs["city_ids"])),
			"city_names": clone(listOrEmpty(resolvedIDs["city_names"])),
		},
		"sections": sections,
	}
}

func marketingBindingError(bindings map[string]any) any {
	missing := make([]string, 0)
	for _, field := range marketingRequiredBindings {
		if isMissing(bindings[field]) {
			missing = append(missing, field)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return "template bindings missing: " + strings.Join(missing, ", ")
}

func marketingMaterialSourceLabel(value any) any {
	switch value {
	case "ACCOUNT_UPLOAD":
		return "上传素材"
	case "CREATOR_AUTHORIZED":
		return "达人素材"
	default:
		return value
	}
}

func showMarketingTemplate(config map[string]any, selector string) (map[string]any, error) {
	rows, err := listMarketingTemplates(config)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row["name"] == selector {
			result := cloneMap(row)
			result["channel"] = "marketing"
			return result, nil
		}
	}
	return nil, configurationError("Marketing plan template not found", map[string]any{
		"channel": "marketing", "selector": selector,
	})
}
