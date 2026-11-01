package contracts

// These rules are fixed to the request descriptions in the captured official
// GET documents. They are not derived from names or live tool descriptions.
type OfficialParameterCondition struct {
	Field    string   `json:"field"`
	Operator string   `json:"operator"`
	Values   []string `json:"values,omitempty"`
}

type OfficialConditionalRule struct {
	When            []OfficialParameterCondition `json:"when"`
	Required        []string                     `json:"required,omitempty"`
	Forbidden       []string                     `json:"forbidden,omitempty"`
	MaxDateSpanDays int                          `json:"max_date_span_days,omitempty"`
	Evidence        string                       `json:"evidence"`
}

func documentedConditionalRules(id string) []OfficialConditionalRule {
	rules := []OfficialConditionalRule{}
	when := func(field string, values ...string) OfficialParameterCondition {
		return OfficialParameterCondition{Field: field, Operator: "equals", Values: values}
	}
	require := func(condition OfficialParameterCondition, fields ...string) {
		rules = append(rules, OfficialConditionalRule{When: []OfficialParameterCondition{condition}, Required: fields, Evidence: "Official GET request descriptions for " + id})
	}
	switch id {
	case "tools_interest_action_id2word_v2":
		require(when("targeting_type", "ACTION"), "action_days")
	case "tools_interest_action_keyword_suggest_v2":
		require(when("targeting_type", "ACTION"), "action_days", "action_scene")
	case "qianchuan_estimate_effect_v1", "qianchuan_suggest_budget_v1":
		rules = append(rules,
			OfficialConditionalRule{When: []OfficialParameterCondition{when("live_schedule_type", "SCHEDULE_START_END")}, Required: []string{"start_time", "end_time"}, Forbidden: []string{"schedule_fixed_range"}, Evidence: "Official GET schedule type parameter descriptions"},
			OfficialConditionalRule{When: []OfficialParameterCondition{when("live_schedule_type", "SCHEDULE_FROM_NOW")}, Forbidden: []string{"start_time", "end_time", "schedule_fixed_range"}, Evidence: "Official GET schedule type parameter descriptions"},
			OfficialConditionalRule{When: []OfficialParameterCondition{when("live_schedule_type", "SCHEDULE_TIME_FIXEDRANGE")}, Required: []string{"schedule_fixed_range"}, Forbidden: []string{"start_time", "end_time", "schedule_time"}, Evidence: "Official GET fixed duration description"})
	case "qianchuan_report_search_word_get_v1":
		require(OfficialParameterCondition{Field: "filtering.word", Operator: "present"}, "filtering.word_type")
	case "qianchuan_suggest_bid_v1":
		require(when("marketing_goal", "LIVE_PROM_GOODS"), "aweme_id")
		require(when("marketing_goal", "VIDEO_PROM_GOODS"), "product_id")
	case "qianchuan_uni_aweme_suggest_budget_v1":
		require(when("marketing_goal", "VIDEO_PROM_GOODS"), "product_ids")
	case "qianchuan_uni_promotion_block_material_get_v1":
		require(when("marketing_goal", "VIDEO_PROM_GOODS"), "product_id")
	case "qianchuan_tools_log_search_v1":
		require(when("object_type", "AD"), "object_id")
	case "subscribe_accounts_list_v3":
		require(when("subscribe_package_type", "TASK"), "subscribe_task_id")
	case "qianchuan_aweme_uni_promotion_order_report_get_v1":
		rules = append(rules, OfficialConditionalRule{When: []OfficialParameterCondition{when("time_granularity", "TIME_GRANULARITY_HOURLY")}, MaxDateSpanDays: 7, Evidence: "Official GET hourly date span description"})
	}
	if id == "qianchuan_suggest_bid_v1" || id == "qianchuan_suggest_roi_goal_v1" {
		require(when("campaign_scene", "NEW_CUSTOMER_TRANSFORMATION"), "ecom_guest_type")
		require(when("ecom_guest_type", "NO_BUY_BRAND"), "brand_id")
	}
	if id == "qianchuan_aweme_estimate_profit_v1" || id == "qianchuan_aweme_suggest_bid_v1" {
		require(when("audience.audience_mode", "FANS", "LIVEFANS"), "audience.author_ids")
		rules = append(rules, OfficialConditionalRule{When: []OfficialParameterCondition{when("marketing_goal", "VIDEO_PROM_GOODS"), when("audience.audience_mode", "CUSTOM"), when("audience.district", "CITY", "COUNTY")}, Required: []string{"audience.city"}, Evidence: "Official GET custom video audience city description"})
		if id == "qianchuan_aweme_estimate_profit_v1" {
			require(when("delivery_setting.bid_mode", "PRICING_ACTION"), "delivery_setting.bid_type")
			rules = append(rules, OfficialConditionalRule{When: []OfficialParameterCondition{when("delivery_setting.bid_type", "MANUAL_BID"), when("delivery_setting.external_action", "AD_CONVERT_TYPE_LIVE_ROI")}, Required: []string{"delivery_setting.pay_roi"}, Evidence: "Official GET manual live ROI description"})
		}
	}
	if id == "qianchuan_tools_estimate_audience_v1" {
		require(when("audience.auto_extend_enabled", "1"), "audience.auto_extend_targets")
		rules = append(rules, OfficialConditionalRule{When: []OfficialParameterCondition{when("audience.audience_mode", "CUSTOM"), when("audience.district", "CITY", "COUNTY")}, Required: []string{"audience.city", "audience.location_type"}, Evidence: "Official GET custom district audience requirements"})
	}
	return rules
}
