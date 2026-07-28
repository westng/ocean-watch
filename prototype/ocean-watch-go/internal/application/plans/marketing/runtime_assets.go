package marketing

import (
	"context"
	"errors"
	"fmt"
	"strings"

	applicationdiscovery "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/discovery"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/configuration"
)

const (
	runtimeAssetPageSize         = 100
	runtimePromotionPageSize     = 20
	maximumReferenceProjectCount = 20
)

type RuntimeAssetRequest struct {
	AdvertiserID  string
	AuthAccountID string
	Config        map[string]any
}

type RuntimeAssetResult struct {
	Config   map[string]any
	Evidence map[string]any
}

type RuntimeAssetResolver interface {
	Resolve(context.Context, RuntimeAssetRequest) (RuntimeAssetResult, error)
}

type RuntimeDiscovery interface {
	QueryProjects(context.Context, applicationdiscovery.ProjectQuery) (applicationdiscovery.Result, error)
	QueryPromotions(context.Context, applicationdiscovery.PromotionQuery) (applicationdiscovery.Result, error)
	QueryDPA(context.Context, applicationdiscovery.DPAQuery) (applicationdiscovery.Result, error)
	QueryEvents(context.Context, applicationdiscovery.EventQuery) (applicationdiscovery.Result, error)
	QueryGoals(context.Context, applicationdiscovery.GoalQuery) (applicationdiscovery.Result, error)
}

type RuntimeAssetError struct {
	Code    string
	Message string
	Details map[string]any
}

func (err *RuntimeAssetError) Error() string { return err.Message }

type AssetResolver struct {
	Discovery RuntimeDiscovery
}

type dpaProductResult struct {
	Available bool
	RequestID string
	Status    string
}

type referenceCreative struct {
	ImageIDs    []any
	BrandInfo   map[string]any
	ProjectID   string
	PromotionID string
	RequestIDs  []string
}

func (resolver AssetResolver) Resolve(
	ctx context.Context,
	request RuntimeAssetRequest,
) (RuntimeAssetResult, error) {
	if ctx == nil {
		return RuntimeAssetResult{}, errors.New("Marketing runtime asset context is required")
	}
	if resolver.Discovery == nil {
		return RuntimeAssetResult{}, errors.New("Marketing runtime discovery service is required")
	}
	effective := configuration.CloneMap(request.Config)
	advertiserID := strings.TrimSpace(request.AdvertiserID)
	if advertiserID == "" {
		advertiserID = textValue(configuration.Value(effective, "account.advertiser_id"))
	}
	if !validPositiveID(advertiserID) {
		return RuntimeAssetResult{}, &RuntimeAssetError{
			Code: "advertiser_id_required", Message: "a decimal advertiser ID is required for runtime asset resolution",
		}
	}
	request.AdvertiserID = advertiserID

	externalActionRequired := !configuration.Missing(configuration.Value(effective, "defaults.external_action"))
	configuredEventAssets := uniqueRuntimeValues(configuration.Value(effective, "resolved_ids.event_asset_ids"))
	needsEventAsset := externalActionRequired && len(configuredEventAssets) == 0
	imageType := textValue(configuration.Value(effective, "defaults.product_info.product_image_type"))
	configuredImages := uniqueRuntimeValues(configuration.Value(effective, "resolved_ids.product_image_ids"))
	needsDPAQuery := imageType == "DPA" && len(configuredImages) == 0

	dpa := dpaProductResult{Status: "not_required"}
	if needsDPAQuery {
		dpa = resolver.queryDPAProduct(ctx, request, effective)
	}
	needsReferenceCreative := needsDPAQuery && !dpa.Available
	projects := []map[string]any{}
	projectRequestIDs := []string{}
	if needsEventAsset || needsReferenceCreative {
		var err error
		projects, projectRequestIDs, err = resolver.referenceProjects(ctx, request, effective)
		if err != nil {
			return RuntimeAssetResult{}, err
		}
	}

	eventResolution, err := resolver.resolveEventAssets(
		ctx, request, effective, projects, externalActionRequired, configuredEventAssets,
	)
	if err != nil {
		return RuntimeAssetResult{}, err
	}
	productResolution, err := resolver.resolveProductCreative(
		ctx, request, effective, projects, imageType, configuredImages, dpa,
	)
	if err != nil {
		return RuntimeAssetResult{}, err
	}
	return RuntimeAssetResult{Config: effective, Evidence: map[string]any{
		"reference_project_count": len(projects),
		"project_request_ids":     stringsToAny(projectRequestIDs),
		"event_asset":             eventResolution,
		"product_creative":        productResolution,
	}}, nil
}

func (resolver AssetResolver) referenceProjects(
	ctx context.Context,
	request RuntimeAssetRequest,
	config map[string]any,
) ([]map[string]any, []string, error) {
	projects := []map[string]any{}
	requestIDs := []string{}
	for page := 1; ; page++ {
		result, err := resolver.Discovery.QueryProjects(ctx, applicationdiscovery.ProjectQuery{
			CredentialScope: applicationdiscovery.CredentialScope{
				AdvertiserID: request.AdvertiserID, AuthAccountID: request.AuthAccountID,
			},
			LandingType:   textValue(configuration.Value(config, "defaults.landing_type")),
			MarketingGoal: textValue(configuration.Value(config, "defaults.marketing_goal")),
			DeliveryMode:  textValue(configuration.Value(config, "defaults.delivery_mode")),
			Page:          page, PageSize: runtimeAssetPageSize,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("list same-product Marketing projects: %w", err)
		}
		if result.RequestID != "" {
			requestIDs = append(requestIDs, result.RequestID)
		}
		rows, err := responseRows(result.Response, "list")
		if err != nil {
			return nil, nil, fmt.Errorf("map Marketing project candidates: %w", err)
		}
		for _, row := range rows {
			if runtimeProjectMatches(config, row) {
				projects = append(projects, row)
			}
		}
		done, err := responsePageDone(result.Response, page, runtimeAssetPageSize, len(rows))
		if err != nil {
			return nil, nil, fmt.Errorf("map Marketing project pagination: %w", err)
		}
		if done {
			break
		}
	}
	return projects, requestIDs, nil
}

func (resolver AssetResolver) queryDPAProduct(
	ctx context.Context,
	request RuntimeAssetRequest,
	config map[string]any,
) dpaProductResult {
	productID := textValue(configuration.Value(config, "resolved_ids.unique_product_id"))
	if !validPositiveID(productID) {
		return dpaProductResult{Status: "non_decimal_product_id"}
	}
	result, err := resolver.Discovery.QueryDPA(ctx, applicationdiscovery.DPAQuery{
		CredentialScope: applicationdiscovery.CredentialScope{
			AdvertiserID: request.AdvertiserID, AuthAccountID: request.AuthAccountID,
		},
		Mode: "asset-detail", UniqueProductID: productID,
	})
	if err != nil {
		return dpaProductResult{Status: "query_failed"}
	}
	fields := runtimeStrings(configuration.Value(config, "defaults.product_info.product_image_fields"))
	available := containsRuntimeField(configuration.Value(result.Response, "data"), fields)
	status := "empty"
	if available {
		status = "available"
	}
	return dpaProductResult{Available: available, RequestID: result.RequestID, Status: status}
}

func (resolver AssetResolver) resolveEventAssets(
	ctx context.Context,
	request RuntimeAssetRequest,
	config map[string]any,
	projects []map[string]any,
	required bool,
	configured []any,
) (map[string]any, error) {
	if !required {
		return map[string]any{"status": "not_required"}, nil
	}
	if len(configured) != 0 {
		return map[string]any{
			"status": "configured", "source": "template", "asset_ids": runtimeStringValues(configured),
		}, nil
	}
	projectCandidates := []any{}
	for _, project := range projects {
		projectCandidates = append(projectCandidates,
			uniqueRuntimeValues(configuration.Value(project, "optimize_goal.asset_ids"))...)
	}
	projectCandidates = uniqueRuntimeValues(projectCandidates)
	if len(projectCandidates) != 0 {
		valid, requestIDs, err := resolver.validEventAssets(ctx, request, config, projectCandidates)
		if err != nil {
			return nil, err
		}
		switch len(valid) {
		case 1:
			setRuntimePath(config, "resolved_ids", "event_asset_ids", []any{valid[0]})
			return map[string]any{
				"status": "resolved", "source": "matching_project",
				"asset_ids": runtimeStringValues(valid), "request_ids": stringsToAny(requestIDs),
			}, nil
		case 0:
		default:
			return nil, &RuntimeAssetError{
				Code:    "event_asset_selection_required",
				Message: "multiple event assets are used by matching projects",
				Details: map[string]any{"candidate_event_asset_ids": runtimeStringValues(valid)},
			}
		}
	}

	candidates, listRequestIDs, err := resolver.eventCandidates(ctx, request)
	if err != nil {
		return nil, err
	}
	ids := make([]any, 0, len(candidates))
	byID := map[string]map[string]any{}
	for _, candidate := range candidates {
		id := configuration.Value(candidate, "asset_id")
		if configuration.Missing(id) {
			continue
		}
		ids = append(ids, id)
		byID[textValue(id)] = candidate
	}
	valid, goalRequestIDs, err := resolver.validEventAssets(ctx, request, config, ids)
	if err != nil {
		return nil, err
	}
	if len(valid) == 1 {
		setRuntimePath(config, "resolved_ids", "event_asset_ids", []any{valid[0]})
		return map[string]any{
			"status": "resolved", "source": "event_asset_list",
			"asset_ids":   runtimeStringValues(valid),
			"request_ids": stringsToAny(append(listRequestIDs, goalRequestIDs...)),
		}, nil
	}
	details := make([]any, 0, len(valid))
	for _, value := range valid {
		row := byID[textValue(value)]
		details = append(details, map[string]any{
			"asset_id": textValue(value), "asset_name": textValue(row["asset_name"]),
		})
	}
	if len(valid) == 0 {
		return nil, &RuntimeAssetError{
			Code:    "event_asset_unavailable",
			Message: "no event asset supports the selected optimization goal",
			Details: map[string]any{"candidate_event_assets": details},
		}
	}
	return nil, &RuntimeAssetError{
		Code:    "event_asset_selection_required",
		Message: "multiple event assets support the selected optimization goal",
		Details: map[string]any{"candidate_event_assets": details},
	}
}

func (resolver AssetResolver) eventCandidates(
	ctx context.Context,
	request RuntimeAssetRequest,
) ([]map[string]any, []string, error) {
	rows := []map[string]any{}
	requestIDs := []string{}
	for page := 1; ; page++ {
		result, err := resolver.Discovery.QueryEvents(ctx, applicationdiscovery.EventQuery{
			CredentialScope: applicationdiscovery.CredentialScope{
				AdvertiserID: request.AdvertiserID, AuthAccountID: request.AuthAccountID,
			},
			AssetType: "THIRD_EXTERNAL", Page: page, PageSize: runtimeAssetPageSize,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("list Marketing event assets: %w", err)
		}
		if result.RequestID != "" {
			requestIDs = append(requestIDs, result.RequestID)
		}
		pageRows, err := responseRows(result.Response, "asset_list")
		if err != nil {
			return nil, nil, fmt.Errorf("map Marketing event assets: %w", err)
		}
		rows = append(rows, pageRows...)
		done, err := responsePageDone(result.Response, page, runtimeAssetPageSize, len(pageRows))
		if err != nil {
			return nil, nil, fmt.Errorf("map Marketing event pagination: %w", err)
		}
		if done {
			break
		}
	}
	return rows, requestIDs, nil
}

func (resolver AssetResolver) validEventAssets(
	ctx context.Context,
	request RuntimeAssetRequest,
	config map[string]any,
	candidates []any,
) ([]any, []string, error) {
	valid := []any{}
	requestIDs := []string{}
	for _, candidate := range uniqueRuntimeValues(candidates) {
		assetID := textValue(candidate)
		if !validPositiveID(assetID) {
			return nil, nil, errors.New("Marketing event asset response contains an invalid asset_id")
		}
		result, err := resolver.Discovery.QueryGoals(ctx, applicationdiscovery.GoalQuery{
			CredentialScope: applicationdiscovery.CredentialScope{
				AdvertiserID: request.AdvertiserID, AuthAccountID: request.AuthAccountID,
			},
			LandingType:   textValue(configuration.Value(config, "defaults.landing_type")),
			AdType:        textValue(configuration.Value(config, "defaults.ad_type")),
			AssetType:     textValue(configuration.Value(config, "defaults.asset_type")),
			MarketingGoal: textValue(configuration.Value(config, "defaults.marketing_goal")),
			DeliveryMode:  textValue(configuration.Value(config, "defaults.delivery_mode")),
			DeliveryType:  "NORMAL", AssetID: assetID, IncludeAsset: true,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("validate Marketing event asset %s: %w", assetID, err)
		}
		if result.RequestID != "" {
			requestIDs = append(requestIDs, result.RequestID)
		}
		if eventAssetSupportsGoal(config, result.Response) {
			valid = append(valid, candidate)
		}
	}
	return valid, requestIDs, nil
}

func (resolver AssetResolver) resolveProductCreative(
	ctx context.Context,
	request RuntimeAssetRequest,
	config map[string]any,
	projects []map[string]any,
	imageType string,
	configuredImages []any,
	dpa dpaProductResult,
) (map[string]any, error) {
	if imageType != "DPA" {
		return map[string]any{
			"status": "not_required", "source": "template_custom", "image_count": len(configuredImages),
		}, nil
	}
	if len(configuredImages) != 0 {
		applyRuntimeProductImages(config, configuredImages, configuration.Object(
			configuration.Value(config, "resolved_ids.brand_info"),
		))
		return map[string]any{
			"status": "resolved", "source": "template_image_ids", "image_count": len(configuredImages),
		}, nil
	}
	if dpa.Available {
		return compactRuntimeMap(map[string]any{
			"status": "validated", "source": "dpa_product_fields",
			"fields":     stringsToAny(runtimeStrings(configuration.Value(config, "defaults.product_info.product_image_fields"))),
			"request_id": dpa.RequestID,
		}), nil
	}
	reference, err := resolver.referenceProductCreative(ctx, request, projects)
	if err != nil {
		return nil, err
	}
	if reference != nil {
		applyRuntimeProductImages(config, reference.ImageIDs, reference.BrandInfo)
		return compactRuntimeMap(map[string]any{
			"status": "resolved", "source": "matching_promotion", "image_count": len(reference.ImageIDs),
			"reference_project_id":   reference.ProjectID,
			"reference_promotion_id": reference.PromotionID,
			"request_ids":            stringsToAny(reference.RequestIDs),
			"dpa_status":             dpa.Status, "dpa_request_id": dpa.RequestID,
		}), nil
	}
	return nil, &RuntimeAssetError{
		Code:    "product_creative_asset_required",
		Message: "DPA product image fields are unavailable and no matching promotion image was found",
		Details: compactRuntimeMap(map[string]any{
			"advertiser_id": request.AdvertiserID,
			"product_id":    textValue(configuration.Value(config, "resolved_ids.unique_product_id")),
			"dpa_status":    dpa.Status, "dpa_request_id": dpa.RequestID,
		}),
	}
}

func (resolver AssetResolver) referenceProductCreative(
	ctx context.Context,
	request RuntimeAssetRequest,
	projects []map[string]any,
) (*referenceCreative, error) {
	requestIDs := []string{}
	if len(projects) > maximumReferenceProjectCount {
		projects = projects[:maximumReferenceProjectCount]
	}
	for _, project := range projects {
		projectID := textValue(project["project_id"])
		if !validPositiveID(projectID) {
			return nil, errors.New("same-product Marketing project is missing project_id")
		}
		for page := 1; ; page++ {
			result, err := resolver.Discovery.QueryPromotions(ctx, applicationdiscovery.PromotionQuery{
				CredentialScope: applicationdiscovery.CredentialScope{
					AdvertiserID: request.AdvertiserID, AuthAccountID: request.AuthAccountID,
				},
				ProjectID: projectID, Page: page, PageSize: runtimePromotionPageSize,
			})
			if err != nil {
				return nil, fmt.Errorf("list same-product Marketing promotions: %w", err)
			}
			if result.RequestID != "" {
				requestIDs = append(requestIDs, result.RequestID)
			}
			rows, err := responseRows(result.Response, "list")
			if err != nil {
				return nil, fmt.Errorf("map Marketing promotion candidates: %w", err)
			}
			for _, row := range rows {
				imageIDs := uniqueRuntimeValues(configuration.Value(row, "promotion_materials.product_info.image_ids"))
				if len(imageIDs) == 0 {
					continue
				}
				return &referenceCreative{
					ImageIDs: imageIDs, BrandInfo: cleanRuntimeMap(configuration.Object(row["brand_info"])),
					ProjectID: projectID, PromotionID: textValue(row["promotion_id"]),
					RequestIDs: append([]string(nil), requestIDs...),
				}, nil
			}
			done, err := responsePageDone(result.Response, page, runtimePromotionPageSize, len(rows))
			if err != nil {
				return nil, fmt.Errorf("map Marketing promotion pagination: %w", err)
			}
			if done {
				break
			}
		}
	}
	return nil, nil
}

func runtimeProjectMatches(config map[string]any, project map[string]any) bool {
	expectedProduct := configuration.Value(config, "resolved_ids.unique_product_id")
	if !configuration.Missing(expectedProduct) &&
		textValue(configuration.Value(project, "related_product.unique_product_id")) != textValue(expectedProduct) {
		return false
	}
	for _, fields := range [][2]string{
		{"landing_type", "landing_type"}, {"marketing_goal", "marketing_goal"},
		{"delivery_mode", "delivery_mode"}, {"ad_type", "ad_type"}, {"asset_type", "asset_type"},
	} {
		expected := configuration.Value(config, "defaults."+fields[0])
		actual := project[fields[1]]
		if !configuration.Missing(expected) && !configuration.Missing(actual) && textValue(expected) != textValue(actual) {
			return false
		}
	}
	expectedAction := configuration.Value(config, "defaults.external_action")
	actualAction := configuration.Value(project, "optimize_goal.external_action")
	return configuration.Missing(expectedAction) || configuration.Missing(actualAction) ||
		textValue(expectedAction) == textValue(actualAction)
}

func eventAssetSupportsGoal(config map[string]any, response map[string]any) bool {
	expected := textValue(configuration.Value(config, "defaults.external_action"))
	for _, raw := range configuration.List(configuration.Value(response, "data.goals")) {
		goal := configuration.Object(raw)
		if expected == "" || textValue(goal["external_action"]) == expected {
			return true
		}
	}
	return false
}

func responseRows(response map[string]any, key string) ([]map[string]any, error) {
	data, ok := response["data"].(map[string]any)
	if !ok {
		return nil, errors.New("official response is missing data")
	}
	raw, exists := data[key]
	if !exists || raw == nil {
		return []map[string]any{}, nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("official response data.%s is not a list", key)
	}
	rows := make([]map[string]any, 0, len(values))
	for _, value := range values {
		row, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("official response data.%s contains a non-object row", key)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func responsePageDone(response map[string]any, page, pageSize, rowCount int) (bool, error) {
	value := configuration.Value(response, "data.page_info.total_page")
	if configuration.Missing(value) {
		return true, nil
	}
	total, err := configuration.Integer(value)
	if err != nil || total < page {
		return false, errors.New("official response contains contradictory pagination")
	}
	return page >= total, nil
}

func containsRuntimeField(value any, fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	wanted := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		wanted[field] = struct{}{}
	}
	var walk func(any) bool
	walk = func(current any) bool {
		switch typed := current.(type) {
		case map[string]any:
			for key, child := range typed {
				if _, ok := wanted[key]; ok && !configuration.Missing(child) {
					return true
				}
				if walk(child) {
					return true
				}
			}
		case []any:
			for _, child := range typed {
				if walk(child) {
					return true
				}
			}
		}
		return false
	}
	return walk(value)
}

func applyRuntimeProductImages(config map[string]any, imageIDs []any, brandInfo map[string]any) {
	defaults := ensureRuntimeObject(config, "defaults")
	productInfo := ensureRuntimeObject(defaults, "product_info")
	productInfo["product_image_type"] = "CUSTOM"
	delete(productInfo, "product_image_fields")
	resolved := ensureRuntimeObject(config, "resolved_ids")
	resolved["product_image_ids"] = uniqueRuntimeValues(imageIDs)
	if cleaned := cleanRuntimeMap(brandInfo); len(cleaned) != 0 {
		resolved["brand_info"] = cleaned
	}
}

func setRuntimePath(config map[string]any, section, key string, value any) {
	ensureRuntimeObject(config, section)[key] = value
}

func ensureRuntimeObject(parent map[string]any, key string) map[string]any {
	if existing, ok := parent[key].(map[string]any); ok {
		return existing
	}
	created := map[string]any{}
	parent[key] = created
	return created
}

func uniqueRuntimeValues(value any) []any {
	values := configuration.List(value)
	if values == nil {
		if configuration.Missing(value) {
			return []any{}
		}
		values = []any{value}
	}
	result := make([]any, 0, len(values))
	seen := map[string]struct{}{}
	for _, item := range values {
		if configuration.Missing(item) {
			continue
		}
		key := textValue(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, configuration.Clone(item))
	}
	return result
}

func runtimeStrings(value any) []string {
	values := uniqueRuntimeValues(value)
	result := make([]string, 0, len(values))
	for _, item := range values {
		result = append(result, textValue(item))
	}
	return result
}

func runtimeStringValues(values []any) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		result = append(result, textValue(value))
	}
	return result
}

func stringsToAny(values []string) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

func cleanRuntimeMap(value map[string]any) map[string]any {
	result := map[string]any{}
	for key, item := range value {
		if configuration.Missing(item) {
			continue
		}
		result[key] = configuration.Clone(item)
	}
	return result
}

func compactRuntimeMap(value map[string]any) map[string]any {
	result := map[string]any{}
	for key, item := range value {
		if item == nil || item == "" {
			continue
		}
		if values, ok := item.([]any); ok && len(values) == 0 {
			continue
		}
		result[key] = item
	}
	return result
}
