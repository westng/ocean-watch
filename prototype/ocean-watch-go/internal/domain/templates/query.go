package templates

import "errors"

func List(config map[string]any, channel string, includeDetails bool) (map[string]any, error) {
	if channel == "" {
		channel = "all"
	}
	if channel != "all" && channel != "marketing" && channel != "qianchuan" {
		return nil, configurationError("unsupported template channel", map[string]any{"channel": channel})
	}
	channels := map[string]any{}
	byChannel := map[string]any{}
	businessCount := 0
	defaultCount := 0
	if channel == "all" || channel == "marketing" {
		marketing, err := marketingChannel(config, includeDetails)
		if err != nil {
			return nil, err
		}
		channels["marketing"] = marketing
		count := marketing["business_template_count"].(int)
		businessCount += count
		defaultCount += marketing["default_skeleton_count"].(int)
		byChannel["marketing"] = count
	}
	if channel == "all" || channel == "qianchuan" {
		qianchuan, err := qianchuanChannel(config, includeDetails)
		if err != nil {
			return nil, err
		}
		channels["qianchuan"] = qianchuan
		count := qianchuan["business_template_count"].(int)
		businessCount += count
		defaultCount += qianchuan["default_skeleton_count"].(int)
		byChannel["qianchuan"] = count
	}
	return map[string]any{
		"ok":     true,
		"source": "local_config",
		"summary": map[string]any{
			"business_template_count": businessCount,
			"default_skeleton_count":  defaultCount,
			"by_channel":              byChannel,
		},
		"channels": channels,
	}, nil
}

func Show(config map[string]any, channel, selector string) (map[string]any, error) {
	var template map[string]any
	var err error
	ready := false
	displayName := ""
	switch channel {
	case "marketing":
		template, err = showMarketingTemplate(config, selector)
		if err == nil {
			ready = template["binding_error"] == nil
		}
		displayName = "巨量营销"
	case "qianchuan":
		template, err = resolveQianchuanProductTemplate(config, selector)
		if err != nil {
			productErr := err
			if _, ok := asTemplateError(productErr); !ok {
				return nil, productErr
			}
			template, err = resolveQianchuanLiveTemplate(config, selector)
			if err != nil {
				if _, ok := asTemplateError(err); ok {
					return nil, productErr
				}
				return nil, err
			}
			template["template_kind"] = "live"
		} else {
			template["template_kind"] = "product"
		}
		template["channel"] = "qianchuan"
		ready = template["status"] == "active"
		displayName = "巨量千川"
	default:
		return nil, configurationError("unsupported template channel", map[string]any{"channel": channel})
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":                      true,
		"source":                  "local_config",
		"channel":                 channel,
		"display_name":            displayName,
		"selector":                selector,
		"ready_for_plan_creation": ready,
		"template":                template,
	}, nil
}

func qianchuanChannel(config map[string]any, includeDetails bool) (map[string]any, error) {
	productTemplates, productConfig, err := listQianchuanProductTemplates(config)
	if err != nil {
		return nil, err
	}
	liveTemplates, liveConfig, err := listQianchuanLiveTemplates(config)
	if err != nil {
		return nil, err
	}
	rows := make([]any, 0, len(productTemplates)+len(liveTemplates))
	for _, template := range productTemplates {
		if includeDetails {
			row := cloneMap(template)
			row["channel"] = "qianchuan"
			row["template_kind"] = "product"
			rows = append(rows, row)
		} else {
			rows = append(rows, compactQianchuanProductTemplate(template))
		}
	}
	for _, template := range liveTemplates {
		if includeDetails {
			row := cloneMap(template)
			row["channel"] = "qianchuan"
			row["template_kind"] = "live"
			rows = append(rows, row)
		} else {
			rows = append(rows, compactQianchuanLiveTemplate(template))
		}
	}
	productSkeleton := map[string]any{
		"channel":         "qianchuan",
		"name":            qianchuanProductDefaultKey,
		"template_kind":   "product",
		"business_usable": false,
	}
	liveSkeleton := map[string]any{
		"channel":         "qianchuan",
		"name":            qianchuanLiveDefaultKey,
		"template_kind":   "live",
		"business_usable": false,
	}
	if includeDetails {
		productSkeleton["template"] = clone(productConfig[qianchuanProductDefaultKey])
		liveSkeleton["template"] = clone(liveConfig[qianchuanLiveDefaultKey])
	}
	skeletons := []any{productSkeleton, liveSkeleton}
	return map[string]any{
		"channel":                 "qianchuan",
		"display_name":            "巨量千川",
		"business_template_count": len(rows),
		"default_skeleton_count":  len(skeletons),
		"default_skeleton":        productSkeleton,
		"default_skeletons":       skeletons,
		"templates":               rows,
	}, nil
}

func compactQianchuanProductTemplate(template map[string]any) map[string]any {
	bindings := mapOrEmpty(template["bindings"])
	delivery := mapOrEmpty(template["delivery_setting"])
	productIDs := listOrEmpty(bindings["product_ids"])
	return map[string]any{
		"channel":                 "qianchuan",
		"template_id":             template["template_id"],
		"name":                    template["display_name"],
		"status":                  template["status"],
		"advertiser_id":           bindings["advertiser_id"],
		"product_name":            bindings["product_name"],
		"product_ids":             clone(productIDs),
		"product_count":           len(productIDs),
		"template_type":           "商品全域",
		"template_kind":           "product",
		"material_source_type":    mapOrEmpty(template["material_strategy"])["source_type"],
		"daily_budget":            delivery["budget"],
		"roi_goal":                delivery["roi2_goal"],
		"smart_bid_type":          delivery["smart_bid_type"],
		"deep_external_action":    delivery["deep_external_action"],
		"ready_for_plan_creation": template["status"] == "active",
	}
}

func compactQianchuanLiveTemplate(template map[string]any) map[string]any {
	bindings := mapOrEmpty(template["bindings"])
	delivery := mapOrEmpty(template["delivery_setting"])
	return map[string]any{
		"channel":                 "qianchuan",
		"template_id":             template["template_id"],
		"name":                    template["display_name"],
		"status":                  template["status"],
		"advertiser_id":           bindings["advertiser_id"],
		"creator_name":            bindings["creator_name"],
		"aweme_id":                bindings["aweme_id"],
		"template_type":           "直播全域",
		"template_kind":           "live",
		"material_source_type":    mapOrEmpty(template["material_strategy"])["source_type"],
		"daily_budget":            delivery["budget"],
		"roi_goal":                delivery["roi2_goal"],
		"smart_bid_type":          delivery["smart_bid_type"],
		"ready_for_plan_creation": template["status"] == "active",
	}
}

func asTemplateError(err error) (*Error, bool) {
	var target *Error
	ok := errors.As(err, &target)
	return target, ok
}
