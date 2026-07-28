package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	authapplication "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	domainmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/marketing"
	portmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/marketing"
)

const (
	ProjectEndpoint        = "/v3.0/project/list/"
	PromotionEndpoint      = "/v3.0/promotion/list/"
	DPAMetaEndpoint        = "/2/dpa/meta/get/"
	DPADictEndpoint        = "/2/dpa/dict/get/"
	DPAEbpDetailEndpoint   = "/v3.0/dpa/ebp/product/detail/get/"
	DPAAssetDetailEndpoint = "/2/dpa/asset_v2/detail/read/"
	EventEndpoint          = "/2/tools/event/all_assets/list/"
	DeepBidEndpoint        = "/v3.0/event_manager/deep_bid_type/get/"
	GoalEndpoint           = "/v3.0/event_manager/optimized_goal/get_v2/"
	AdminEndpoint          = "/2/tools/admin/info/"
	DefaultPageSize        = 20
	DefaultEventPageSize   = 100
	MaxPageSize            = 100
)

var ProjectFields = []string{
	"project_id", "advertiser_id", "name", "landing_type", "marketing_goal",
	"delivery_mode", "ad_type", "asset_type", "related_product", "optimize_goal",
	"delivery_setting", "delivery_range", "track_url_setting", "audience", "status",
	"project_create_time", "project_modify_time",
}

var PromotionFields = []string{
	"promotion_id", "project_id", "advertiser_id", "promotion_name", "status",
	"status_first", "status_second", "opt_status", "source", "budget", "budget_mode",
	"bid", "cpa_bid", "roi_goal", "first_roi_goal", "promotion_materials", "brand_info",
	"promotion_create_time", "promotion_modify_time",
}

type TokenProvider interface {
	Ensure(context.Context, authapplication.TokenQuery) (authapplication.TokenLease, error)
}

type Service struct {
	Tokens TokenProvider
	Reader portmarketing.DiscoveryReader
}

type CredentialScope struct {
	AdvertiserID  string
	AuthAccountID string
}

type Result struct {
	Endpoint        string         `json:"endpoint"`
	Method          string         `json:"method,omitempty"`
	Params          map[string]any `json:"params"`
	Payload         any            `json:"payload,omitempty"`
	ResponseCode    int64          `json:"response_code"`
	ResponseMessage string         `json:"response_message"`
	RequestID       string         `json:"request_id"`
	GoalSummary     any            `json:"goal_summary,omitempty"`
	Response        map[string]any `json:"response"`
}

type ProjectQuery struct {
	CredentialScope
	Name          string
	LandingType   string
	MarketingGoal string
	DeliveryMode  string
	Page          int
	PageSize      int
}

type PromotionQuery struct {
	CredentialScope
	Name         string
	ProjectID    string
	PromotionIDs []string
	Page         int
	PageSize     int
}

type DPAQuery struct {
	CredentialScope
	Mode            string
	PlatformID      string
	UniqueProductID string
	Page            int
	PageSize        int
}

type EventQuery struct {
	CredentialScope
	AssetType string
	AssetIDs  []string
	Page      int
	PageSize  int
}

type DeepBidQuery struct {
	CredentialScope
	AssetID            string
	ExternalAction     string
	DeepExternalAction string
	DeliveryMode       string
	LandingType        string
	AdType             string
	MarketingGoal      string
	ProductSetting     string
	ValueOptimizedType string
}

type GoalQuery struct {
	CredentialScope
	LandingType   string
	AdType        string
	AssetType     string
	MarketingGoal string
	DeliveryMode  string
	DeliveryType  string
	AssetID       string
	IncludeAsset  bool
}

type AdminQuery struct {
	CredentialScope
	Code string
}

type CityQuery struct {
	CredentialScope
	CityCSV      string
	CityNames    []string
	CountryCodes []string
}

type ResolvedCity struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

type CityAttempt struct {
	CountryCode     string         `json:"country_code"`
	ResponseCode    int64          `json:"response_code"`
	ResponseMessage string         `json:"response_message"`
	NodeCount       int            `json:"node_count"`
	Resolved        []ResolvedCity `json:"resolved"`
	Missing         []string       `json:"missing"`
	RawResponse     any            `json:"raw_response"`
}

type CityResult struct {
	CityCSV         string         `json:"city_csv"`
	BestCountryCode string         `json:"best_country_code,omitempty"`
	ResolvedCount   int            `json:"resolved_count"`
	Missing         []string       `json:"missing"`
	Resolved        []ResolvedCity `json:"resolved"`
	Attempts        []CityAttempt  `json:"attempts"`
	ConfigUpdated   string         `json:"config_updated,omitempty"`
}

func (service Service) QueryProjects(ctx context.Context, query ProjectQuery) (Result, error) {
	if err := service.validate(); err != nil {
		return Result{}, err
	}
	query.Name = strings.TrimSpace(query.Name)
	query.LandingType = normalizeDefault(query.LandingType, "SHOP")
	query.MarketingGoal = normalizeDefault(query.MarketingGoal, "VIDEO_AND_IMAGE")
	query.DeliveryMode = normalizeDefault(query.DeliveryMode, "PROCEDURAL")
	query.Page, query.PageSize = normalizePage(query.Page, query.PageSize, DefaultPageSize)
	if err := validateScope(query.CredentialScope); err != nil {
		return Result{}, err
	}
	if err := validatePage(query.Page, query.PageSize); err != nil {
		return Result{}, err
	}
	if err := ValidateProjectFilters(query.LandingType, query.MarketingGoal, query.DeliveryMode); err != nil {
		return Result{}, err
	}
	lease, err := service.token(ctx, query.CredentialScope)
	if err != nil {
		return Result{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return Result{}, err
	}
	filtering := map[string]any{}
	if query.Name != "" {
		filtering["name"] = query.Name
	}
	filtering["landing_type"] = query.LandingType
	filtering["marketing_goal"] = query.MarketingGoal
	filtering["delivery_mode"] = query.DeliveryMode
	params := map[string]any{
		"advertiser_id": query.AdvertiserID, "fields": append([]string(nil), ProjectFields...),
		"filtering": filtering, "page": query.Page, "page_size": query.PageSize,
	}
	envelope, err := service.Reader.FetchProjects(ctx, portmarketing.ProjectDiscoveryRequest{
		DiscoveryScope: portmarketing.DiscoveryScope{AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken},
		Fields:         ProjectFields, Name: query.Name, LandingType: query.LandingType,
		MarketingGoal: query.MarketingGoal, DeliveryMode: query.DeliveryMode,
		Page: query.Page, PageSize: query.PageSize,
	})
	if err != nil {
		return Result{}, fmt.Errorf("query Marketing projects: %w", err)
	}
	return diagnosticResult(ProjectEndpoint, "", params, nil, envelope), nil
}

func (service Service) QueryPromotions(ctx context.Context, query PromotionQuery) (Result, error) {
	if err := service.validate(); err != nil {
		return Result{}, err
	}
	query.Name, query.ProjectID = strings.TrimSpace(query.Name), strings.TrimSpace(query.ProjectID)
	query.PromotionIDs = uniqueStrings(query.PromotionIDs)
	query.Page, query.PageSize = normalizePage(query.Page, query.PageSize, DefaultPageSize)
	if err := validateScope(query.CredentialScope); err != nil {
		return Result{}, err
	}
	if err := validatePage(query.Page, query.PageSize); err != nil {
		return Result{}, err
	}
	if query.ProjectID != "" {
		if err := domain.ValidateDecimalID(query.ProjectID, "project_id"); err != nil {
			return Result{}, err
		}
	}
	if err := validateIDs(query.PromotionIDs, "promotion_id"); err != nil {
		return Result{}, err
	}
	lease, err := service.token(ctx, query.CredentialScope)
	if err != nil {
		return Result{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return Result{}, err
	}
	filtering := map[string]any{}
	if query.Name != "" {
		filtering["name"] = query.Name
	}
	if query.ProjectID != "" {
		filtering["project_id"] = json.Number(query.ProjectID)
	}
	if len(query.PromotionIDs) != 0 {
		filtering["ids"] = numbers(query.PromotionIDs)
	}
	params := map[string]any{
		"advertiser_id": query.AdvertiserID, "filtering": nullableMap(filtering),
		"fields": append([]string(nil), PromotionFields...), "page": query.Page, "page_size": query.PageSize,
	}
	envelope, err := service.Reader.FetchPromotions(ctx, portmarketing.PromotionDiscoveryRequest{
		DiscoveryScope: portmarketing.DiscoveryScope{AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken},
		Fields:         PromotionFields, Name: query.Name, ProjectID: query.ProjectID,
		PromotionIDs: query.PromotionIDs, Page: query.Page, PageSize: query.PageSize,
	})
	if err != nil {
		return Result{}, fmt.Errorf("query Marketing promotions: %w", err)
	}
	return diagnosticResult(PromotionEndpoint, "", params, nil, envelope), nil
}

func (service Service) QueryDPA(ctx context.Context, query DPAQuery) (Result, error) {
	if err := service.validate(); err != nil {
		return Result{}, err
	}
	query.Mode = strings.TrimSpace(query.Mode)
	query.PlatformID, query.UniqueProductID = strings.TrimSpace(query.PlatformID), strings.TrimSpace(query.UniqueProductID)
	query.Page, query.PageSize = normalizePage(query.Page, query.PageSize, DefaultPageSize)
	if err := validateScope(query.CredentialScope); err != nil {
		return Result{}, err
	}
	if !contains([]string{"meta", "dict", "ebp-detail", "asset-detail"}, query.Mode) {
		return Result{}, errors.New("mode must be meta, dict, ebp-detail, or asset-detail")
	}
	if query.Mode != "asset-detail" {
		if err := domain.ValidateDecimalID(query.PlatformID, "product_platform_id"); err != nil {
			return Result{}, err
		}
	}
	if query.Mode == "ebp-detail" || query.Mode == "asset-detail" {
		if err := domain.ValidateDecimalID(query.UniqueProductID, "unique_product_id"); err != nil {
			return Result{}, err
		}
	}
	if err := validatePage(query.Page, query.PageSize); err != nil {
		return Result{}, err
	}
	lease, err := service.token(ctx, query.CredentialScope)
	if err != nil {
		return Result{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return Result{}, err
	}
	request := portmarketing.DPADiscoveryRequest{
		DiscoveryScope: portmarketing.DiscoveryScope{AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken},
		Mode:           query.Mode, PlatformID: query.PlatformID, UniqueProductID: query.UniqueProductID,
		Page: query.Page, PageSize: query.PageSize,
	}
	endpoint, method := DPAMetaEndpoint, "GET"
	params := map[string]any{"advertiser_id": query.AdvertiserID, "platform_id": json.Number(query.PlatformID)}
	var payload any = json.RawMessage("null")
	switch query.Mode {
	case "dict":
		endpoint = DPADictEndpoint
	case "ebp-detail":
		endpoint = DPAEbpDetailEndpoint
		params = map[string]any{
			"account_id": query.AdvertiserID, "account_type": "EBP", "platform_id": json.Number(query.PlatformID),
			"filtering": map[string]any{"product_id": query.UniqueProductID}, "page": query.Page, "page_size": query.PageSize,
		}
	case "asset-detail":
		endpoint, method, params = DPAAssetDetailEndpoint, "POST", nil
		payload = map[string]any{
			"advertiser_id": query.AdvertiserID, "asset_ids": []any{},
			"unique_product_ids": []json.Number{json.Number(query.UniqueProductID)},
		}
	}
	envelope, err := service.Reader.FetchDPA(ctx, request)
	if err != nil {
		return Result{}, fmt.Errorf("query Marketing DPA discovery: %w", err)
	}
	return diagnosticResult(endpoint, method, params, payload, envelope), nil
}

func (service Service) QueryEvents(ctx context.Context, query EventQuery) (Result, error) {
	if err := service.validate(); err != nil {
		return Result{}, err
	}
	query.AssetType = normalizeDefault(query.AssetType, "THIRD_EXTERNAL")
	query.AssetIDs = uniqueStrings(query.AssetIDs)
	query.Page, query.PageSize = normalizePage(query.Page, query.PageSize, DefaultEventPageSize)
	if err := validateScope(query.CredentialScope); err != nil {
		return Result{}, err
	}
	if err := validatePage(query.Page, query.PageSize); err != nil {
		return Result{}, err
	}
	if err := ValidateEventAssetType(query.AssetType); err != nil {
		return Result{}, err
	}
	if err := validateIDs(query.AssetIDs, "asset_id"); err != nil {
		return Result{}, err
	}
	lease, err := service.token(ctx, query.CredentialScope)
	if err != nil {
		return Result{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return Result{}, err
	}
	filtering := map[string]any{"asset_type": query.AssetType}
	if len(query.AssetIDs) != 0 {
		filtering["asset_ids"] = numbers(query.AssetIDs)
	}
	params := map[string]any{"advertiser_id": query.AdvertiserID, "filtering": filtering, "page": query.Page, "page_size": query.PageSize}
	envelope, err := service.Reader.FetchEvents(ctx, portmarketing.EventDiscoveryRequest{
		DiscoveryScope: portmarketing.DiscoveryScope{AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken},
		AssetType:      query.AssetType, AssetIDs: query.AssetIDs, Page: query.Page, PageSize: query.PageSize,
	})
	if err != nil {
		return Result{}, fmt.Errorf("query Marketing event assets: %w", err)
	}
	return diagnosticResult(EventEndpoint, "", params, nil, envelope), nil
}

func (service Service) QueryDeepBids(ctx context.Context, query DeepBidQuery) (Result, error) {
	if err := service.validate(); err != nil {
		return Result{}, err
	}
	query = normalizeDeepBid(query)
	if err := validateScope(query.CredentialScope); err != nil {
		return Result{}, err
	}
	if query.AssetID != "" {
		if err := domain.ValidateDecimalID(query.AssetID, "asset_id"); err != nil {
			return Result{}, err
		}
	}
	if err := ValidateDeepBidFilters(query, true); err != nil {
		return Result{}, err
	}
	lease, err := service.token(ctx, query.CredentialScope)
	if err != nil {
		return Result{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return Result{}, err
	}
	params := compactMap(map[string]any{
		"advertiser_id": query.AdvertiserID, "asset_id": numberOrNil(query.AssetID),
		"external_action": query.ExternalAction, "deep_external_action": nullableString(query.DeepExternalAction),
		"delivery_mode": nullableString(query.DeliveryMode), "landing_type": nullableString(query.LandingType),
		"ad_type": nullableString(query.AdType), "marketing_goal": nullableString(query.MarketingGoal),
		"product_setting": nullableString(query.ProductSetting), "value_optimized_type": nullableString(query.ValueOptimizedType),
	})
	envelope, err := service.Reader.FetchDeepBids(ctx, portmarketing.DeepBidDiscoveryRequest{
		DiscoveryScope: portmarketing.DiscoveryScope{AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken},
		AssetID:        query.AssetID, ExternalAction: query.ExternalAction, DeepExternalAction: query.DeepExternalAction,
		DeliveryMode: query.DeliveryMode, LandingType: query.LandingType, AdType: query.AdType,
		MarketingGoal: query.MarketingGoal, ProductSetting: query.ProductSetting, ValueOptimizedType: query.ValueOptimizedType,
	})
	if err != nil {
		return Result{}, fmt.Errorf("query Marketing deep bid types: %w", err)
	}
	return diagnosticResult(DeepBidEndpoint, "", params, nil, envelope), nil
}

func (service Service) QueryGoals(ctx context.Context, query GoalQuery) (Result, error) {
	if err := service.validate(); err != nil {
		return Result{}, err
	}
	query = normalizeGoal(query)
	if err := validateScope(query.CredentialScope); err != nil {
		return Result{}, err
	}
	if query.IncludeAsset && query.AssetID != "" {
		if err := domain.ValidateDecimalID(query.AssetID, "asset_id"); err != nil {
			return Result{}, err
		}
	}
	if err := ValidateGoalFilters(query, true); err != nil {
		return Result{}, err
	}
	lease, err := service.token(ctx, query.CredentialScope)
	if err != nil {
		return Result{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return Result{}, err
	}
	params := compactMap(map[string]any{
		"advertiser_id": query.AdvertiserID, "landing_type": query.LandingType, "ad_type": query.AdType,
		"asset_type": nullableString(query.AssetType), "marketing_goal": nullableString(query.MarketingGoal),
		"delivery_mode": nullableString(query.DeliveryMode), "delivery_type": nullableString(query.DeliveryType),
	})
	if query.IncludeAsset {
		params["asset_id"] = numberOrNil(query.AssetID)
	}
	envelope, err := service.Reader.FetchGoals(ctx, portmarketing.GoalDiscoveryRequest{
		DiscoveryScope: portmarketing.DiscoveryScope{AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken},
		LandingType:    query.LandingType, AdType: query.AdType, AssetType: query.AssetType,
		MarketingGoal: query.MarketingGoal, DeliveryMode: query.DeliveryMode, DeliveryType: query.DeliveryType,
		AssetID: query.AssetID, IncludeAsset: query.IncludeAsset,
	})
	if err != nil {
		return Result{}, fmt.Errorf("query Marketing optimization goals: %w", err)
	}
	result := diagnosticResult(GoalEndpoint, "", params, nil, envelope)
	result.GoalSummary = summarizeGoals(envelope.Response["data"])
	return result, nil
}

func (service Service) QueryAdmin(ctx context.Context, query AdminQuery) (domainmarketing.AdminEnvelope, error) {
	if err := service.validate(); err != nil {
		return domainmarketing.AdminEnvelope{}, err
	}
	query.Code = strings.TrimSpace(query.Code)
	if err := validateScope(query.CredentialScope); err != nil {
		return domainmarketing.AdminEnvelope{}, err
	}
	if query.Code == "" {
		return domainmarketing.AdminEnvelope{}, errors.New("country code is required")
	}
	lease, err := service.token(ctx, query.CredentialScope)
	if err != nil {
		return domainmarketing.AdminEnvelope{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return domainmarketing.AdminEnvelope{}, err
	}
	result, err := service.Reader.FetchAdminInfo(ctx, portmarketing.AdminDiscoveryRequest{
		DiscoveryScope: portmarketing.DiscoveryScope{AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken},
		Codes:          []string{query.Code},
	})
	if err != nil {
		return domainmarketing.AdminEnvelope{}, fmt.Errorf("query Marketing administrative regions: %w", err)
	}
	return result, nil
}

func (service Service) ResolveCities(ctx context.Context, query CityQuery) (CityResult, error) {
	if err := service.validate(); err != nil {
		return CityResult{}, err
	}
	query.CityCSV = strings.TrimSpace(query.CityCSV)
	query.CityNames = trimmedValues(query.CityNames)
	query.CountryCodes = uniqueStrings(query.CountryCodes)
	if err := validateScope(query.CredentialScope); err != nil {
		return CityResult{}, err
	}
	if query.CityCSV == "" {
		return CityResult{}, errors.New("city_csv is required")
	}
	if len(query.CityNames) == 0 {
		return CityResult{}, errors.New("city CSV contains no city names")
	}
	if len(query.CountryCodes) == 0 {
		return CityResult{}, errors.New("at least one country code is required")
	}
	lease, err := service.token(ctx, query.CredentialScope)
	if err != nil {
		return CityResult{}, err
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return CityResult{}, err
	}
	result := CityResult{
		CityCSV: query.CityCSV, Missing: append([]string(nil), query.CityNames...),
		Resolved: []ResolvedCity{}, Attempts: []CityAttempt{},
	}
	for _, countryCode := range query.CountryCodes {
		envelope, fetchErr := service.Reader.FetchAdminInfo(ctx, portmarketing.AdminDiscoveryRequest{
			DiscoveryScope: portmarketing.DiscoveryScope{
				AdvertiserID: query.AdvertiserID, AccessToken: lease.AccessToken,
			},
			Codes: []string{countryCode},
		})
		if fetchErr != nil {
			return CityResult{}, fmt.Errorf("query Marketing administrative regions for %s: %w", countryCode, fetchErr)
		}
		flat := flattenAdminNodes(envelope.Nodes)
		mapping := map[string]string{}
		for _, node := range flat {
			if node.Name == "" || node.Code == "" {
				continue
			}
			mapping[node.Name] = node.Code
			mapping[normalizeCityName(node.Name)] = node.Code
		}
		resolved := []ResolvedCity{}
		missing := []string{}
		for _, name := range query.CityNames {
			code := mapping[name]
			if code == "" {
				code = mapping[normalizeCityName(name)]
			}
			if code == "" {
				missing = append(missing, name)
				continue
			}
			resolved = append(resolved, ResolvedCity{Name: name, Code: code})
		}
		var raw any
		if len(flat) == 0 {
			raw = envelope.Response
		}
		attempt := CityAttempt{
			CountryCode: countryCode, ResponseCode: envelope.Code, ResponseMessage: envelope.Message,
			NodeCount: len(flat), Resolved: resolved, Missing: missing, RawResponse: raw,
		}
		result.Attempts = append(result.Attempts, attempt)
		result.BestCountryCode = countryCode
		result.Resolved, result.Missing = resolved, missing
		result.ResolvedCount = len(resolved)
		if len(resolved) != 0 && len(missing) == 0 {
			break
		}
	}
	return result, nil
}

func (service Service) validate() error {
	if service.Tokens == nil || service.Reader == nil {
		return errors.New("Marketing discovery service dependencies are incomplete")
	}
	return nil
}

func (service Service) token(ctx context.Context, scope CredentialScope) (authapplication.TokenLease, error) {
	return service.Tokens.Ensure(ctx, authapplication.TokenQuery{Channel: "marketing", AdvertiserID: scope.AdvertiserID, AuthAccountID: scope.AuthAccountID})
}

func diagnosticResult(endpoint, method string, params map[string]any, payload any, envelope domainmarketing.DiscoveryEnvelope) Result {
	return Result{Endpoint: endpoint, Method: method, Params: params, Payload: payload,
		ResponseCode: envelope.Code, ResponseMessage: envelope.Message, RequestID: envelope.RequestID, Response: envelope.Response}
}

func flattenAdminNodes(nodes []domainmarketing.AdminNode) []domainmarketing.AdminNode {
	result := []domainmarketing.AdminNode{}
	for _, node := range nodes {
		result = append(result, node)
		result = append(result, flattenAdminNodes(node.Children)...)
	}
	return result
}

func normalizeCityName(value string) string {
	result := strings.TrimSpace(value)
	for _, suffix := range []string{"壮族自治区", "回族自治区", "维吾尔自治区", "自治区", "省", "市"} {
		if strings.HasSuffix(result, suffix) {
			return strings.TrimSuffix(result, suffix)
		}
	}
	return result
}

func trimmedValues(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func validateScope(scope CredentialScope) error {
	if err := domain.ValidateDecimalID(strings.TrimSpace(scope.AdvertiserID), "advertiser_id"); err != nil {
		return err
	}
	if strings.TrimSpace(scope.AuthAccountID) != "" {
		return domain.ValidateDecimalID(strings.TrimSpace(scope.AuthAccountID), "auth_account_id")
	}
	return nil
}

func normalizePage(page, size, defaultSize int) (int, int) {
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = defaultSize
	}
	return page, size
}

func validatePage(page, size int) error {
	if page < 1 {
		return errors.New("page must be positive")
	}
	if size < 1 || size > MaxPageSize {
		return errors.New("page_size must be between 1 and 100")
	}
	return nil
}

func validateIDs(values []string, field string) error {
	for index, value := range values {
		if err := domain.ValidateDecimalID(value, fmt.Sprintf("%s[%d]", field, index)); err != nil {
			return err
		}
	}
	return nil
}

func validateEnum(value, field string, allowed map[string]struct{}) error {
	if _, ok := allowed[value]; !ok {
		return fmt.Errorf("%s has unsupported value %q", field, value)
	}
	return nil
}

func validateOptionalEnum(value, field string, allowed map[string]struct{}) error {
	if value == "" {
		return nil
	}
	return validateEnum(value, field, allowed)
}

func ValidateProjectFilters(landingType, marketingGoal, deliveryMode string) error {
	checks := []struct {
		value, field string
		allowed      map[string]struct{}
	}{
		{landingType, "landing_type", projectLandingTypes},
		{marketingGoal, "marketing_goal", projectMarketingGoals},
		{deliveryMode, "delivery_mode", deliveryModes},
	}
	for _, check := range checks {
		if err := validateEnum(check.value, check.field, check.allowed); err != nil {
			return err
		}
	}
	return nil
}

func ValidateEventAssetType(assetType string) error {
	return validateEnum(assetType, "asset_type", eventAssetTypes)
}

func ValidateDeepBidFilters(query DeepBidQuery, requireExternalAction bool) error {
	checks := []struct {
		value, field string
		allowed      map[string]struct{}
	}{
		{query.ExternalAction, "external_action", externalActions},
		{query.DeepExternalAction, "deep_external_action", deepExternalActions},
		{query.DeliveryMode, "delivery_mode", deliveryModes},
		{query.LandingType, "landing_type", deepBidLandingTypes},
		{query.AdType, "ad_type", adTypes},
		{query.MarketingGoal, "marketing_goal", deepBidMarketingGoals},
		{query.ProductSetting, "product_setting", productSettings},
		{query.ValueOptimizedType, "value_optimized_type", valueOptimizedTypes},
	}
	for _, check := range checks {
		if err := validateOptionalEnum(check.value, check.field, check.allowed); err != nil {
			return err
		}
	}
	if requireExternalAction && strings.TrimSpace(query.ExternalAction) == "" {
		return errors.New("external_action is required")
	}
	return nil
}

func ValidateGoalFilters(query GoalQuery, requireLandingAndAdType bool) error {
	checks := []struct {
		value, field string
		allowed      map[string]struct{}
	}{
		{query.LandingType, "landing_type", goalLandingTypes},
		{query.AdType, "ad_type", adTypes},
		{query.AssetType, "asset_type", goalAssetTypes},
		{query.MarketingGoal, "marketing_goal", goalMarketingGoals},
		{query.DeliveryMode, "delivery_mode", deliveryModes},
		{query.DeliveryType, "delivery_type", deliveryTypes},
	}
	for _, check := range checks {
		if err := validateOptionalEnum(check.value, check.field, check.allowed); err != nil {
			return err
		}
	}
	if requireLandingAndAdType &&
		(strings.TrimSpace(query.LandingType) == "" || strings.TrimSpace(query.AdType) == "") {
		return errors.New("landing_type and ad_type are required")
	}
	return nil
}

func normalizeDefault(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func normalizeDeepBid(query DeepBidQuery) DeepBidQuery {
	query.AssetID = strings.TrimSpace(query.AssetID)
	query.ExternalAction = strings.TrimSpace(query.ExternalAction)
	query.DeepExternalAction = strings.TrimSpace(query.DeepExternalAction)
	query.DeliveryMode = strings.TrimSpace(query.DeliveryMode)
	query.LandingType = strings.TrimSpace(query.LandingType)
	query.AdType = strings.TrimSpace(query.AdType)
	query.MarketingGoal = strings.TrimSpace(query.MarketingGoal)
	query.ProductSetting = normalizeDefault(query.ProductSetting, "SINGLE")
	query.ValueOptimizedType = strings.TrimSpace(query.ValueOptimizedType)
	return query
}

func normalizeGoal(query GoalQuery) GoalQuery {
	query.LandingType = strings.TrimSpace(query.LandingType)
	query.AdType = strings.TrimSpace(query.AdType)
	query.AssetType = strings.TrimSpace(query.AssetType)
	query.MarketingGoal = strings.TrimSpace(query.MarketingGoal)
	query.DeliveryMode = strings.TrimSpace(query.DeliveryMode)
	query.DeliveryType = normalizeDefault(query.DeliveryType, "NORMAL")
	query.AssetID = strings.TrimSpace(query.AssetID)
	return query
}

func uniqueStrings(values []string) []string {
	result, seen := make([]string, 0, len(values)), map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func numbers(values []string) []json.Number {
	result := make([]json.Number, 0, len(values))
	for _, value := range values {
		result = append(result, json.Number(value))
	}
	return result
}

func numberOrNil(value string) any {
	if value == "" {
		return nil
	}
	return json.Number(value)
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableMap(value map[string]any) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func compactMap(value map[string]any) map[string]any {
	result := map[string]any{}
	for key, item := range value {
		if item != nil {
			result[key] = item
		}
	}
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func summarizeGoals(value any) []map[string]any {
	result := []map[string]any{}
	seen := map[string]struct{}{}
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			if hasAny(typed, "external_action", "external_action_name", "deep_external_action", "deep_external_action_name", "convert_type", "convert_type_name", "value", "label") {
				compact := compactMap(map[string]any{
					"external_action":      firstValue(typed, "external_action", "convert_type", "value"),
					"external_action_name": firstValue(typed, "external_action_name", "convert_type_name", "label"),
					"deep_external_action": typed["deep_external_action"], "deep_external_action_name": typed["deep_external_action_name"],
					"deep_bid_type": typed["deep_bid_type"], "pricing": typed["pricing"], "bid_type": typed["bid_type"],
				})
				if len(compact) != 0 {
					encoded, _ := json.Marshal(compact)
					key := string(encoded)
					if _, exists := seen[key]; !exists {
						seen[key] = struct{}{}
						result = append(result, compact)
					}
				}
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return result
}

func hasAny(value map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := value[key]; ok {
			return true
		}
	}
	return false
}

func firstValue(value map[string]any, keys ...string) any {
	for _, key := range keys {
		if item := value[key]; item != nil && item != "" {
			return item
		}
	}
	return nil
}

func enumSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

var (
	projectLandingTypes   = enumSet("APP", "DPA", "LINK", "MICRO_GAME", "NATIVE_ACTION", "QUICK_APP", "SHOP")
	projectMarketingGoals = enumSet("LIVE", "VIDEO_AND_IMAGE")
	deliveryModes         = enumSet("MANUAL", "PROCEDURAL")
	eventAssetTypes       = enumSet("THIRD_EXTERNAL", "TETRIS_EXTERNAL", "APP", "QUICK_APP", "MINI_PROGRAME")
	deepBidLandingTypes   = enumSet("APP", "ARTICLE", "BRAND_EXTERNAL", "DPA", "GOODS", "LINK", "LIVE", "MICRO_GAME", "NATIVE_ACTION", "QUICK_APP", "SHOP", "STORE")
	goalLandingTypes      = enumSet("APP", "DPA", "LINK", "MICRO_GAME", "NATIVE_ACTION", "QUICK_APP", "SHOP")
	adTypes               = enumSet("ALL", "SEARCH")
	deepBidMarketingGoals = enumSet("LIVE", "VIDEO_AND_IMAGE")
	goalMarketingGoals    = deepBidMarketingGoals
	productSettings       = enumSet("MULTI_PRODUCTS", "NO_MAP", "SINGLE")
	valueOptimizedTypes   = enumSet("ACTION", "OFF")
	deliveryTypes         = enumSet("NORMAL", "DURATION")
	goalAssetTypes        = enumSet("APP", "AWEME", "ENTERPRISE", "MICRO_APP", "MINI_PROGRAM", "ORANGE", "QUICK_APP", "THIRDPARTY", "WECHAT_APP")
)
