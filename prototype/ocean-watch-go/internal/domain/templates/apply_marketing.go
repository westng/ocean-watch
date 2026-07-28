package templates

import (
	"fmt"
	"strings"
)

type MarketingPlanTemplateSelection struct {
	Name            string
	AdvertiserID    string
	Channel         string
	AllowNoTemplate bool
}

func ApplyMarketingPlanTemplate(
	config map[string]any,
	selection MarketingPlanTemplateSelection,
) (map[string]any, error) {
	effective := cloneMap(config)
	base := marketingDefaultBundle(config)
	for _, section := range marketingTemplateSections {
		effective[section] = cloneMap(mapOrEmpty(base[section]))
	}
	effective["titles"] = clone(listOrEmpty(base["titles"]))

	name := strings.TrimSpace(selection.Name)
	if name == "" {
		if !selection.AllowNoTemplate {
			return nil, configurationError(
				"no business plan template selected; pass an explicit plan template",
				nil,
			)
		}
		effective["_selected_plan_template"] = nil
		return effective, nil
	}
	templates := mapOrEmpty(config["plan_templates"])
	raw, exists := templates[name]
	if !exists {
		available := strings.Join(sortedKeys(templates), ", ")
		if available == "" {
			available = "<none>"
		}
		return nil, configurationError(
			fmt.Sprintf("unknown plan template: %s; available: %s", name, available),
			map[string]any{"template": name},
		)
	}
	rawTemplate, ok := raw.(map[string]any)
	if !ok {
		return nil, configurationError(
			"Marketing plan template must be an object",
			map[string]any{"template": name},
		)
	}
	template, err := normalizeMarketingTemplate(config, name, rawTemplate)
	if err != nil {
		return nil, err
	}
	schemaVersion, err := parseVersion(
		config["plan_template_schema_version"],
		1,
		"plan_template_schema_version",
	)
	if err != nil {
		return nil, err
	}
	bindings := mapOrEmpty(template["bindings"])
	if schemaVersion >= 2 {
		if bindingError := marketingBindingError(bindings); bindingError != nil {
			return nil, configurationError(fmt.Sprint(bindingError), nil)
		}
	}
	strategy := mapOrEmpty(template["material_strategy"])
	if schemaVersion >= 3 {
		if strategyError := marketingMaterialStrategyError(strategy); strategyError != "" {
			return nil, configurationError(strategyError, nil)
		}
		if fixedFields := marketingFixedMaterialFields(template); len(fixedFields) != 0 {
			return nil, configurationError(
				"plan templates cannot store runtime material IDs: "+strings.Join(fixedFields, ", "),
				map[string]any{"fields": anyStrings(fixedFields)},
			)
		}
	}

	boundChannel := stringValue(bindings["channel"])
	if boundChannel == "" {
		boundChannel = "marketing"
	}
	requestedChannel := strings.TrimSpace(selection.Channel)
	if requestedChannel == "" {
		requestedChannel = stringValue(mapOrEmpty(config["account"])["channel"])
	}
	if requestedChannel == "" {
		requestedChannel = stringValue(config["default_channel"])
	}
	if requestedChannel == "" {
		requestedChannel = "marketing"
	}
	if boundChannel != requestedChannel {
		return nil, configurationError(fmt.Sprintf(
			"plan template %s is bound to channel %s, not channel %s",
			name,
			boundChannel,
			requestedChannel,
		), nil)
	}

	boundAdvertiserID := bindings["advertiser_id"]
	requestedAdvertiserID := any(strings.TrimSpace(selection.AdvertiserID))
	if isMissing(requestedAdvertiserID) {
		requestedAdvertiserID = mapOrEmpty(config["account"])["advertiser_id"]
	}
	if !isMissing(boundAdvertiserID) && !isMissing(requestedAdvertiserID) &&
		stringValue(boundAdvertiserID) != stringValue(requestedAdvertiserID) {
		return nil, configurationError(fmt.Sprintf(
			"plan template %s is bound to advertiser %s, not advertiser %s",
			name,
			stringValue(boundAdvertiserID),
			stringValue(requestedAdvertiserID),
		), nil)
	}

	overrides := mapOrEmpty(template["overrides"])
	for _, section := range marketingTemplateSections {
		if override, exists := overrides[section]; exists {
			effective[section] = deepMerge(
				mapOrEmpty(effective[section]),
				mapOrEmpty(override),
			)
		}
	}
	effective["titles"] = clone(listOrEmpty(mapOrEmpty(template["copy_materials"])["titles"]))
	effective["material_strategy"] = cloneMap(strategy)

	account := cloneMap(mapOrEmpty(effective["account"]))
	if !isMissing(boundAdvertiserID) {
		account["advertiser_id"] = clone(boundAdvertiserID)
	}
	account["channel"] = boundChannel
	effective["account"] = account
	defaults := cloneMap(mapOrEmpty(effective["defaults"]))
	if !isMissing(bindings["product_name"]) {
		defaults["product_name"] = clone(bindings["product_name"])
	}
	if !isMissing(bindings["product_id"]) {
		defaults["product_id"] = clone(bindings["product_id"])
		resolvedProductID := mapOrEmpty(effective["resolved_ids"])["unique_product_id"]
		if !isMissing(resolvedProductID) &&
			stringValue(resolvedProductID) != stringValue(bindings["product_id"]) {
			return nil, configurationError(fmt.Sprintf(
				"plan template %s binds product %s, but resolved_ids.unique_product_id is %s",
				name,
				stringValue(bindings["product_id"]),
				stringValue(resolvedProductID),
			), nil)
		}
	}
	effective["defaults"] = defaults
	effective["_selected_plan_template"] = map[string]any{
		"name":              name,
		"display_name":      clone(template["display_name"]),
		"bindings":          cloneMap(bindings),
		"material_strategy": cloneMap(strategy),
		"legacy":            template["legacy"],
	}
	return effective, nil
}
