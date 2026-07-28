package templates

type QianchuanProductCreateSource struct {
	ID       string
	Template map[string]any
}

type QianchuanProductCreateInput struct {
	TemplateID   string
	AdvertiserID string
	ProductName  string
	ProductIDs   any
}

type QianchuanLiveCreateSource struct {
	ID       string
	Template map[string]any
}

type QianchuanLiveCreateInput struct {
	TemplateID   string
	AdvertiserID string
	CreatorName  string
	AwemeID      string
}

func QianchuanProductCreateSources(config map[string]any) (map[string]any, []QianchuanProductCreateSource, error) {
	normalized, err := ensureQianchuanProductConfig(config)
	if err != nil {
		return nil, nil, err
	}
	sources := []QianchuanProductCreateSource{{
		ID:       qianchuanProductDefaultKey,
		Template: cloneMap(mapOrEmpty(normalized[qianchuanProductDefaultKey])),
	}}
	for _, templateID := range sortedKeys(mapOrEmpty(normalized[qianchuanProductTemplatesKey])) {
		template, resolveErr := resolveQianchuanProductTemplate(normalized, templateID)
		if resolveErr != nil {
			return nil, nil, resolveErr
		}
		sources = append(sources, QianchuanProductCreateSource{
			ID:       templateID,
			Template: template,
		})
	}
	return normalized, sources, nil
}

func BuildQianchuanProductTemplate(
	source map[string]any,
	input QianchuanProductCreateInput,
) (map[string]any, error) {
	advertiserID, err := positiveID(input.AdvertiserID, "advertiser_id")
	if err != nil {
		return nil, err
	}
	productName, err := requiredText(input.ProductName, "product_name")
	if err != nil {
		return nil, err
	}
	productIDs, err := normalizeQianchuanProductIDs(input.ProductIDs)
	if err != nil {
		return nil, err
	}
	templateID, err := requiredText(input.TemplateID, "template_id")
	if err != nil {
		return nil, err
	}
	if source == nil {
		source = defaultQianchuanProductTemplate()
	}
	deliverySource := source["delivery_setting"]
	if !pythonTruthy(deliverySource) {
		deliverySource = defaultQianchuanProductTemplate()["delivery_setting"]
	}
	delivery, err := validateQianchuanProductDelivery(deliverySource)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"template_id":   templateID,
		"display_name":  qianchuanProductDisplayName(advertiserID, productName, stringList(productIDs)),
		"template_type": qianchuanProductTemplateType,
		"status":        "active",
		"bindings": map[string]any{
			"channel":       "qianchuan",
			"advertiser_id": advertiserID,
			"product_name":  productName,
			"product_ids":   productIDs,
		},
		"delivery_setting": delivery,
		"material_strategy": map[string]any{
			"source_type":          qianchuanProductMaterialSource,
			"persist_material_ids": false,
		},
	}, nil
}

func ApplyQianchuanProductTemplate(config, candidate map[string]any) (map[string]any, error) {
	normalized, err := ensureQianchuanProductConfig(config)
	if err != nil {
		return nil, err
	}
	validated, err := validateQianchuanProductTemplate(candidate)
	if err != nil {
		return nil, err
	}
	templates, ok := normalized[qianchuanProductTemplatesKey].(map[string]any)
	if !ok {
		return nil, configurationError("qianchuan_product_templates must be an object", nil)
	}
	for _, raw := range templates {
		existing, existingOK := raw.(map[string]any)
		if existingOK && existing["display_name"] == validated["display_name"] {
			return nil, configurationError(
				"Qianchuan product template already exists: "+stringValue(validated["display_name"]),
				map[string]any{"name": validated["display_name"]},
			)
		}
	}
	templateID := stringValue(validated["template_id"])
	if _, exists := templates[templateID]; exists {
		return nil, configurationError("Qianchuan product template ID already exists", map[string]any{"template_id": templateID})
	}
	updatedTemplates := cloneMap(templates)
	updatedTemplates[templateID] = validated
	normalized[qianchuanProductTemplatesKey] = updatedTemplates
	return normalized, nil
}

func QianchuanLiveCreateSources(config map[string]any) (map[string]any, []QianchuanLiveCreateSource, error) {
	normalized, err := ensureQianchuanLiveConfig(config)
	if err != nil {
		return nil, nil, err
	}
	sources := []QianchuanLiveCreateSource{{
		ID:       qianchuanLiveDefaultKey,
		Template: cloneMap(mapOrEmpty(normalized[qianchuanLiveDefaultKey])),
	}}
	for _, templateID := range sortedKeys(mapOrEmpty(normalized[qianchuanLiveTemplatesKey])) {
		template, resolveErr := resolveQianchuanLiveTemplate(normalized, templateID)
		if resolveErr != nil {
			return nil, nil, resolveErr
		}
		sources = append(sources, QianchuanLiveCreateSource{
			ID:       templateID,
			Template: template,
		})
	}
	return normalized, sources, nil
}

func BuildQianchuanLiveTemplate(
	source map[string]any,
	input QianchuanLiveCreateInput,
) (map[string]any, error) {
	advertiserID, err := positiveID(input.AdvertiserID, "advertiser_id")
	if err != nil {
		return nil, err
	}
	creatorName, err := requiredText(input.CreatorName, "creator_name")
	if err != nil {
		return nil, err
	}
	awemeID, err := positiveID(input.AwemeID, "aweme_id")
	if err != nil {
		return nil, err
	}
	templateID, err := requiredText(input.TemplateID, "template_id")
	if err != nil {
		return nil, err
	}
	if source == nil {
		source = defaultQianchuanLiveTemplate()
	}
	deliverySource := source["delivery_setting"]
	if !pythonTruthy(deliverySource) {
		deliverySource = defaultQianchuanLiveTemplate()["delivery_setting"]
	}
	delivery, err := validateQianchuanLiveDelivery(deliverySource)
	if err != nil {
		return nil, err
	}
	creativeSource := source["creative_setting"]
	if !pythonTruthy(creativeSource) {
		creativeSource = defaultQianchuanLiveTemplate()["creative_setting"]
	}
	creative, err := validateQianchuanLiveCreative(creativeSource)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"template_id":   templateID,
		"display_name":  qianchuanLiveDisplayName(advertiserID, creatorName, awemeID),
		"template_type": qianchuanLiveTemplateType,
		"status":        "active",
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
	}, nil
}

func ApplyQianchuanLiveTemplate(config, candidate map[string]any) (map[string]any, error) {
	normalized, err := ensureQianchuanLiveConfig(config)
	if err != nil {
		return nil, err
	}
	validated, err := validateQianchuanLiveTemplate(candidate)
	if err != nil {
		return nil, err
	}
	templates, ok := normalized[qianchuanLiveTemplatesKey].(map[string]any)
	if !ok {
		return nil, configurationError("qianchuan_live_templates must be an object", nil)
	}
	for _, raw := range templates {
		existing, existingOK := raw.(map[string]any)
		if existingOK && existing["display_name"] == validated["display_name"] {
			return nil, configurationError(
				"Qianchuan live template already exists",
				map[string]any{"name": validated["display_name"]},
			)
		}
	}
	templateID := stringValue(validated["template_id"])
	if _, exists := templates[templateID]; exists {
		return nil, configurationError("Qianchuan live template ID already exists", map[string]any{"template_id": templateID})
	}
	updatedTemplates := cloneMap(templates)
	updatedTemplates[templateID] = validated
	normalized[qianchuanLiveTemplatesKey] = updatedTemplates
	return normalized, nil
}
