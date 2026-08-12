package marketing

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
)

type PayloadOptions struct {
	AdvertiserID    string
	Budget          any
	CPABid          any
	ROIGoal         any
	VideoIDs        []string
	MaterialDate    string
	ProductName     string
	ProductID       string
	ProjectName     string
	PromotionName   string
	ProjectID       string
	GroupIndex      int
	Index           int
	Suffix          string
	CreatorID       string
	AppendCreatorID bool
	Now             time.Time
}

type Payloads struct {
	Project       map[string]any `json:"project_payload"`
	Promotion     map[string]any `json:"promotion_payload"`
	MissingFields []string       `json:"missing_fields"`
}

func (payloads Payloads) JSON() (json.RawMessage, json.RawMessage, error) {
	project, err := json.Marshal(payloads.Project)
	if err != nil {
		return nil, nil, fmt.Errorf("encode Marketing project payload: %w", err)
	}
	promotion, err := json.Marshal(payloads.Promotion)
	if err != nil {
		return nil, nil, fmt.Errorf("encode Marketing promotion payload: %w", err)
	}
	return project, promotion, nil
}

func BuildPayloads(config map[string]any, options PayloadOptions) (Payloads, error) {
	defaults, ok := config["defaults"].(map[string]any)
	if !ok {
		return Payloads{}, errors.New("Marketing defaults must be an object")
	}
	advertiserID := strings.TrimSpace(options.AdvertiserID)
	if advertiserID == "" {
		advertiserID = textValue(configuration.Value(config, "account.advertiser_id"))
	}
	advertiserNumber, err := positiveJSONNumber(advertiserID, "advertiser_id")
	if err != nil {
		return Payloads{}, err
	}
	materialDate := strings.TrimSpace(options.MaterialDate)
	if materialDate == "" {
		now := options.Now
		if now.IsZero() {
			now = time.Now()
		}
		yesterday := now.AddDate(0, 0, -1)
		materialDate = fmt.Sprintf("%d.%d", int(yesterday.Month()), yesterday.Day())
	}
	productName := strings.TrimSpace(options.ProductName)
	if productName == "" {
		productName = textValue(defaults["product_name"])
	}
	productID := strings.TrimSpace(options.ProductID)
	if productID == "" {
		productID = textValue(defaults["product_id"])
	}
	budget := options.Budget
	if budget == nil {
		budget = configuration.Clone(defaults["daily_budget"])
	}
	cpaBid := options.CPABid
	if cpaBid == nil {
		cpaBid = configuration.Clone(defaults["cpa_bid"])
	}
	videoIDs := append([]string(nil), options.VideoIDs...)
	if len(videoIDs) == 0 {
		videoIDs = stringSlice(configuration.Value(config, "materials.video_ids"))
	}
	groupIndex := options.GroupIndex
	if groupIndex == 0 {
		groupIndex = 1
	}
	index := options.Index
	if index == 0 {
		index = 1
	}
	suffix := strings.TrimSpace(options.Suffix)
	if suffix == "" {
		suffix = "01"
	}
	nameValues := map[string]string{
		"material_date": materialDate,
		"product_name":  productName,
		"group_index":   fmt.Sprint(groupIndex),
		"index":         fmt.Sprint(index),
		"suffix":        suffix,
		"creator_id":    strings.TrimSpace(options.CreatorID),
		"aweme_id":      strings.TrimSpace(options.CreatorID),
	}
	projectName := strings.TrimSpace(options.ProjectName)
	if projectName == "" {
		template := textValue(defaults["project_name_template"])
		projectName = renderMarketingName(template, nameValues)
		if options.AppendCreatorID && options.CreatorID != "" && !containsCreatorNameToken(template) {
			projectName += "_" + strings.TrimSpace(options.CreatorID)
		}
	}
	promotionName := strings.TrimSpace(options.PromotionName)
	if promotionName == "" {
		template := textValue(defaults["promotion_name_template"])
		promotionName = renderMarketingName(template, nameValues)
		if options.AppendCreatorID && options.CreatorID != "" && !containsCreatorNameToken(template) {
			promotionName += "_" + strings.TrimSpace(options.CreatorID)
		}
	}

	uniqueProductID := configuration.Value(config, "resolved_ids.unique_product_id")
	productPlatformID := configuration.Value(config, "resolved_ids.product_platform_id")
	relatedProduct := map[string]any{"product_setting": "SINGLE"}
	if !configuration.Missing(uniqueProductID) {
		relatedProduct["unique_product_id"] = integerIfDecimal(uniqueProductID)
	} else if !configuration.Missing(productID) && !configuration.Missing(productPlatformID) {
		relatedProduct["products"] = []any{map[string]any{
			"product_id": productID, "product_platform_id": configuration.Clone(productPlatformID),
		}}
	} else if !configuration.Missing(productID) {
		relatedProduct["product_id"] = productID
	}
	if configuration.Missing(uniqueProductID) && !configuration.Missing(productPlatformID) {
		relatedProduct["product_platform_id"] = configuration.Clone(productPlatformID)
	}

	audience := map[string]any{
		"district":       configuration.Clone(defaults["district"]),
		"region_version": configuration.Clone(defaults["region_version"]),
		"city":           anySlice(configuration.Value(config, "resolved_ids.city_ids")),
		"location_type":  configuration.Clone(defaults["location_type"]),
		"gender":         configuration.Clone(defaults["gender"]),
	}
	if ages := defaults["ages"]; !configuration.Missing(ages) {
		audience["age"] = configuration.Clone(ages)
	}
	if hide := defaults["hide_if_converted"]; !configuration.Missing(hide) {
		if hide == "UNLIMITED" {
			hide = "NO_EXCLUDE"
		}
		audience["hide_if_converted"] = configuration.Clone(hide)
	}

	delivery := map[string]any{
		"schedule_type": configuration.Clone(defaults["schedule_type"]),
		"budget_mode":   configuration.Clone(defaults["budget_mode"]),
		"budget":        configuration.Clone(budget),
		"bid_type":      defaultText(defaults["bid_type"], "CUSTOM"),
		"pricing":       configuration.Clone(defaults["pricing"]),
	}
	if cpaBid != nil {
		delivery["cpa_bid"] = configuration.Clone(cpaBid)
	}
	roiGoal := options.ROIGoal
	if roiGoal == nil {
		roiGoal = configuration.Clone(defaults["roi_goal"])
	}
	deepBidType := defaults["deep_bid_type"]
	if deepBidType != "DEEP_BID_DEFAULT" && roiGoal != nil {
		delivery["roi_goal"] = configuration.Clone(roiGoal)
	}
	if !configuration.Missing(deepBidType) {
		delivery["deep_bid_type"] = configuration.Clone(deepBidType)
	}

	optimizeGoal := map[string]any{}
	if externalAction := defaults["external_action"]; !configuration.Missing(externalAction) {
		optimizeGoal["external_action"] = configuration.Clone(externalAction)
	}
	if deepExternalAction := defaults["deep_external_action"]; !configuration.Missing(deepExternalAction) {
		optimizeGoal["deep_external_action"] = configuration.Clone(deepExternalAction)
	}
	if assetIDs := anySlice(configuration.Value(config, "resolved_ids.event_asset_ids")); len(assetIDs) != 0 {
		optimizeGoal["asset_ids"] = assetIDs
	}

	project := map[string]any{
		"advertiser_id":    advertiserNumber,
		"name":             projectName,
		"operation":        configuration.Clone(defaults["operation"]),
		"delivery_mode":    configuration.Clone(defaults["delivery_mode"]),
		"landing_type":     configuration.Clone(defaults["landing_type"]),
		"asset_type":       defaultText(defaults["asset_type"], "THIRDPARTY"),
		"marketing_goal":   configuration.Clone(defaults["marketing_goal"]),
		"ad_type":          configuration.Clone(defaults["ad_type"]),
		"related_product":  relatedProduct,
		"delivery_range":   map[string]any{"inventory_catalog": "UNIVERSAL_SMART"},
		"delivery_setting": delivery,
		"optimize_goal":    optimizeGoal,
		"audience":         audience,
		"track_url_setting": map[string]any{
			"track_url":        anySlice(configuration.Value(config, "tracking_urls.track_url")),
			"action_track_url": anySlice(configuration.Value(config, "tracking_urls.action_track_url")),
		},
	}

	productInfoDefaults := configuration.Object(defaults["product_info"])
	productInfo := map[string]any{
		"titles":         nonEmptyListOrDefault(productInfoDefaults["titles"], productName),
		"selling_points": nonEmptyListOrDefault(productInfoDefaults["selling_points"], productName),
	}
	productNameType := productInfoDefaults["product_name_type"]
	productImageType := productInfoDefaults["product_image_type"]
	productSellingPointType := productInfoDefaults["product_selling_point_type"]
	if !configuration.Missing(productNameType) {
		productInfo["product_name_type"] = configuration.Clone(productNameType)
		if productNameType == "DPA" {
			delete(productInfo, "titles")
		}
	}
	if !configuration.Missing(productImageType) {
		productInfo["product_image_type"] = configuration.Clone(productImageType)
	}
	if !configuration.Missing(productSellingPointType) {
		productInfo["product_selling_point_type"] = configuration.Clone(productSellingPointType)
		if productSellingPointType == "DPA" {
			delete(productInfo, "selling_points")
		}
	}
	productImageIDs := anySlice(configuration.Value(config, "resolved_ids.product_image_ids"))
	if productImageType == "DPA" {
		fields := anySlice(productInfoDefaults["product_image_fields"])
		if len(fields) == 0 {
			fields = []any{"images_url"}
		}
		productInfo["product_image_fields"] = fields
	} else if len(productImageIDs) != 0 {
		productInfo["image_ids"] = productImageIDs
	}
	if productNameType == "DPA" {
		fields := anySlice(productInfoDefaults["product_name_fields"])
		if len(fields) == 0 {
			fields = []any{"name"}
		}
		productInfo["product_name_fields"] = fields
	}
	if productSellingPointType == "DPA" {
		fields := anySlice(productInfoDefaults["product_selling_point_fields"])
		if len(fields) == 0 {
			fields = []any{"selling_points"}
		}
		productInfo["product_selling_point_fields"] = fields
	}

	coverList, coverMap := videoCovers(configuration.Value(config, "materials.video_cover_ids"))
	videoMaterials := make([]any, 0, len(videoIDs))
	for index, videoID := range videoIDs {
		material := map[string]any{
			"video_id": videoID, "image_mode": configuration.Clone(defaults["video_image_mode"]),
		}
		coverID := coverMap[videoID]
		if configuration.Missing(coverID) && index < len(coverList) {
			coverID = coverList[index]
		}
		if !configuration.Missing(coverID) {
			material["video_cover_id"] = configuration.Clone(coverID)
		}
		videoMaterials = append(videoMaterials, material)
	}
	titleMaterials := []any{}
	for _, title := range anySlice(config["titles"]) {
		titleMaterials = append(titleMaterials, map[string]any{"title": configuration.Clone(title)})
	}
	promotionMaterials := map[string]any{
		"video_material_list":        videoMaterials,
		"title_material_list":        titleMaterials,
		"external_url_material_list": []any{pathText(config, "links.landing_page_url")},
		"open_url_type":              defaultText(defaults["open_url_type"], "CUSTOM"),
		"open_url":                   pathText(config, "links.open_url"),
		"component_material_list":    []any{},
		"product_info":               productInfo,
	}
	if buttons := anySlice(defaults["call_to_action_buttons"]); len(buttons) != 0 {
		promotionMaterials["call_to_action_buttons"] = buttons
	}
	projectID := any("{{project_id}}")
	if strings.TrimSpace(options.ProjectID) != "" {
		projectID, err = positiveJSONNumber(options.ProjectID, "project_id")
		if err != nil {
			return Payloads{}, err
		}
	}
	promotion := map[string]any{
		"advertiser_id":       advertiserNumber,
		"project_id":          projectID,
		"name":                promotionName,
		"operation":           configuration.Clone(defaults["operation"]),
		"source":              configuration.Clone(defaults["source"]),
		"promotion_materials": promotionMaterials,
	}
	if !configuration.Missing(uniqueProductID) {
		promotion["promotion_related_product"] = []any{map[string]any{
			"unique_product_id": integerIfDecimal(uniqueProductID),
		}}
	}
	if brandInfo := cleanMap(configuration.Value(config, "resolved_ids.brand_info")); len(brandInfo) != 0 {
		promotion["brand_info"] = brandInfo
	}
	payloads := Payloads{Project: project, Promotion: promotion}
	payloads.MissingFields = MarketingPayloadMissingFields(config, payloads)
	return payloads, nil
}

func MarketingPayloadMissingFields(config map[string]any, payloads Payloads) []string {
	missing := []string{}
	project := payloads.Project
	promotion := payloads.Promotion
	if configuration.ContainsUnresolved(project["name"]) {
		missing = append(missing, "defaults.project_name_template")
	}
	if configuration.ContainsUnresolved(promotion["name"]) {
		missing = append(missing, "defaults.promotion_name_template")
	}
	trackSettings := configuration.Object(project["track_url_setting"])
	if opaqueLinkMissing(trackSettings["track_url"]) {
		missing = append(missing, "tracking_urls.track_url")
	}
	if opaqueLinkMissing(trackSettings["action_track_url"]) {
		missing = append(missing, "tracking_urls.action_track_url")
	}
	if configuration.Missing(configuration.Value(project, "audience.city")) {
		missing = append(missing, "resolved_ids.city_ids")
	}
	uniqueProductID := configuration.Value(config, "resolved_ids.unique_product_id")
	if configuration.Missing(uniqueProductID) && configuration.Missing(configuration.Value(config, "resolved_ids.product_platform_id")) {
		missing = append(missing, "resolved_ids.product_platform_id")
	}
	materials := configuration.Object(configuration.Value(promotion, "promotion_materials"))
	if configuration.Missing(materials["video_material_list"]) {
		missing = append(missing, "materials.video_ids")
	}
	productInfo := configuration.Object(materials["product_info"])
	if productInfo["product_image_type"] == "DPA" {
		if configuration.ContainsUnresolved(productInfo["product_image_fields"]) {
			missing = append(missing, "defaults.product_info.product_image_fields")
		}
	} else if configuration.Missing(configuration.Value(config, "resolved_ids.product_image_ids")) {
		missing = append(missing, "resolved_ids.product_image_ids")
	}
	if opaqueLinkMissing(configuration.Value(config, "links.landing_page_url")) {
		missing = append(missing, "links.landing_page_url")
	}
	if opaqueLinkMissing(configuration.Value(config, "links.open_url")) {
		missing = append(missing, "links.open_url")
	}
	missing = append(missing, marketingLinkPassthroughErrors(config, project, promotion)...)
	if configuration.ContainsUnresolved(promotion["source"]) {
		missing = append(missing, "defaults.source")
	}
	titles := anySlice(materials["title_material_list"])
	if len(titles) == 0 || invalidMarketingTitles(titles) {
		missing = append(missing, "titles")
	}
	if sellingPoints := anySlice(productInfo["selling_points"]); len(sellingPoints) != 0 && invalidSellingPoints(sellingPoints) {
		missing = append(missing, "defaults.product_info.selling_points")
	}
	if configuration.Missing(uniqueProductID) && configuration.ContainsUnresolved(configuration.Value(config, "defaults.product_id")) {
		missing = append(missing, "defaults.product_id")
	}
	if !configuration.Missing(configuration.Value(config, "defaults.external_action")) &&
		configuration.Missing(configuration.Value(config, "resolved_ids.event_asset_ids")) {
		missing = append(missing, "resolved_ids.event_asset_ids")
	}
	return uniqueOrderedStrings(missing)
}

func opaqueLinkMissing(value any) bool {
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

func marketingLinkPassthroughErrors(config, project, promotion map[string]any) []string {
	trackSettings := configuration.Object(project["track_url_setting"])
	materials := configuration.Object(configuration.Value(promotion, "promotion_materials"))
	expectedLandingPage := textValue(configuration.Value(config, "links.landing_page_url"))
	comparisons := []struct {
		field    string
		expected any
		actual   any
	}{
		{"links.landing_page_url", []any{expectedLandingPage}, materials["external_url_material_list"]},
		{"links.open_url", configuration.Value(config, "links.open_url"), materials["open_url"]},
		{"tracking_urls.track_url", anySlice(configuration.Value(config, "tracking_urls.track_url")), trackSettings["track_url"]},
		{"tracking_urls.action_track_url", anySlice(configuration.Value(config, "tracking_urls.action_track_url")), trackSettings["action_track_url"]},
	}
	errors := []string{}
	for _, comparison := range comparisons {
		if !reflect.DeepEqual(comparison.expected, comparison.actual) {
			errors = append(errors, comparison.field)
		}
	}
	return errors
}

func renderMarketingName(template string, values map[string]string) string {
	result := template
	for key, value := range values {
		result = strings.ReplaceAll(result, "{"+key+"}", value)
	}
	return result
}

func containsCreatorNameToken(template string) bool {
	return strings.Contains(template, "{creator_id}") || strings.Contains(template, "{aweme_id}")
}

func positiveJSONNumber(value, field string) (json.Number, error) {
	value = strings.TrimSpace(value)
	if value == "" || value[0] == '0' {
		return "", fmt.Errorf("%s must be a positive decimal ID", field)
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return "", fmt.Errorf("%s must be a positive decimal ID", field)
		}
	}
	return json.Number(value), nil
}

func integerIfDecimal(value any) any {
	text := textValue(value)
	if text == "" {
		return configuration.Clone(value)
	}
	for _, character := range text {
		if character < '0' || character > '9' {
			return configuration.Clone(value)
		}
	}
	return json.Number(text)
}

func textValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func pathText(config map[string]any, path string) string {
	value := configuration.Value(config, path)
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func defaultText(value any, fallback string) any {
	if configuration.Missing(value) {
		return fallback
	}
	return configuration.Clone(value)
}

func anySlice(value any) []any {
	switch typed := value.(type) {
	case nil:
		return []any{}
	case []any:
		return configuration.Clone(typed).([]any)
	case []string:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = item
		}
		return result
	default:
		return []any{}
	}
}

func stringSlice(value any) []string {
	values := anySlice(value)
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, textValue(value))
	}
	return result
}

func nonEmptyListOrDefault(value any, fallback string) []any {
	items := anySlice(value)
	if len(items) == 0 {
		return []any{fallback}
	}
	return items
}

func videoCovers(value any) ([]any, map[string]any) {
	if mapped, ok := value.(map[string]any); ok {
		return []any{}, configuration.CloneMap(mapped)
	}
	return anySlice(value), map[string]any{}
}

func cleanMap(value any) map[string]any {
	mapped, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	result := map[string]any{}
	for key, item := range mapped {
		if configuration.Missing(item) {
			continue
		}
		if list, ok := item.([]any); ok && len(list) == 0 {
			continue
		}
		if nested, ok := item.(map[string]any); ok && len(nested) == 0 {
			continue
		}
		result[key] = configuration.Clone(item)
	}
	return result
}

func invalidMarketingTitles(values []any) bool {
	for _, value := range values {
		row, ok := value.(map[string]any)
		if !ok || configuration.ContainsUnresolved(row["title"]) {
			return true
		}
		length := utf8.RuneCountInString(textValue(row["title"]))
		if length < 5 || length > 30 {
			return true
		}
	}
	return false
}

func invalidSellingPoints(values []any) bool {
	if len(values) > 10 {
		return true
	}
	for _, value := range values {
		positions := 0
		for _, character := range textValue(value) {
			if character < 128 {
				positions++
			} else {
				positions += 2
			}
		}
		if positions < 12 || positions > 18 {
			return true
		}
	}
	return false
}

func uniqueOrderedStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
