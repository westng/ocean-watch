package cli

import (
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/oceanengine"
	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	applicationdiscovery "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/discovery"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
	portmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/marketing"
)

type MarketingDiscoveryService interface {
	QueryProjects(context.Context, applicationdiscovery.ProjectQuery) (applicationdiscovery.Result, error)
	QueryPromotions(context.Context, applicationdiscovery.PromotionQuery) (applicationdiscovery.Result, error)
	QueryDPA(context.Context, applicationdiscovery.DPAQuery) (applicationdiscovery.Result, error)
	QueryEvents(context.Context, applicationdiscovery.EventQuery) (applicationdiscovery.Result, error)
	QueryDeepBids(context.Context, applicationdiscovery.DeepBidQuery) (applicationdiscovery.Result, error)
	QueryGoals(context.Context, applicationdiscovery.GoalQuery) (applicationdiscovery.Result, error)
	ResolveCities(context.Context, applicationdiscovery.CityQuery) (applicationdiscovery.CityResult, error)
}

type MarketingDiscoveryConfigStore interface {
	ReadWithRevision(context.Context) (map[string]any, string, error)
	CompareAndSwap(context.Context, string, map[string]any) error
}

type MarketingDiscoveryRuntime struct {
	Service        MarketingDiscoveryService
	Reader         portmarketing.DiscoveryReader
	Authorizations authapplication.AuthorizationStore
	RefreshLocker  authapplication.RefreshLocker
	OAuth          authapplication.OAuthAdapter
	ClientFactory  *oceanengine.ClientFactory
	Retry          platformretry.Policy
	Now            func() time.Time
	ConfigFactory  func(string) MarketingDiscoveryConfigStore
}

type marketingDiscoveryOptions struct {
	configPath         string
	channel            string
	authAccountID      string
	advertiserID       string
	out                string
	page               int
	pageSize           int
	name               string
	landingType        string
	marketingGoal      string
	deliveryMode       string
	projectID          string
	promotionIDs       repeatedValues
	mode               string
	platformID         string
	uniqueProductID    string
	assetType          string
	assetIDs           repeatedValues
	assetID            string
	externalAction     string
	deepExternalAction string
	adType             string
	productSetting     string
	valueOptimizedType string
	deliveryType       string
	includeAsset       bool
	cityCSV            string
	countryCodes       repeatedValues
	writeConfig        bool
	cityNames          []string
}

func parseMarketingDiscoveryOptions(action string, args []string) (marketingDiscoveryOptions, error) {
	options := marketingDiscoveryOptions{channel: "marketing", includeAsset: true}
	flags := flag.NewFlagSet("discover "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.StringVar(&options.out, "out", "", "")
	flags.StringVar(&options.channel, "channel", "marketing", "")
	flags.StringVar(&options.authAccountID, "auth-account-id", "", "")
	switch action {
	case "projects":
		options.page, options.pageSize = 1, applicationdiscovery.DefaultPageSize
		options.landingType, options.marketingGoal, options.deliveryMode = "SHOP", "VIDEO_AND_IMAGE", "PROCEDURAL"
		flags.StringVar(&options.advertiserID, "advertiser-id", "", "")
		flags.IntVar(&options.page, "page", options.page, "")
		flags.IntVar(&options.pageSize, "page-size", options.pageSize, "")
		flags.StringVar(&options.name, "name", "", "")
		flags.StringVar(&options.landingType, "landing-type", options.landingType, "")
		flags.StringVar(&options.marketingGoal, "marketing-goal", options.marketingGoal, "")
		flags.StringVar(&options.deliveryMode, "delivery-mode", options.deliveryMode, "")
	case "promotions":
		options.page, options.pageSize = 1, applicationdiscovery.DefaultPageSize
		flags.StringVar(&options.advertiserID, "advertiser-id", "", "")
		flags.IntVar(&options.page, "page", options.page, "")
		flags.IntVar(&options.pageSize, "page-size", options.pageSize, "")
		flags.StringVar(&options.name, "name", "", "")
		flags.StringVar(&options.projectID, "project-id", "", "")
		flags.Var(&options.promotionIDs, "promotion-id", "")
	case "dpa":
		options.page, options.pageSize = 1, applicationdiscovery.DefaultPageSize
		flags.StringVar(&options.mode, "mode", "", "")
		flags.IntVar(&options.page, "page", options.page, "")
		flags.IntVar(&options.pageSize, "page-size", options.pageSize, "")
	case "events":
		options.page, options.pageSize = 1, applicationdiscovery.DefaultEventPageSize
		options.assetType = "THIRD_EXTERNAL"
		flags.StringVar(&options.assetType, "asset-type", options.assetType, "")
		flags.Var(&options.assetIDs, "asset-id", "")
		flags.IntVar(&options.page, "page", options.page, "")
		flags.IntVar(&options.pageSize, "page-size", options.pageSize, "")
	case "deep-bids":
		options.productSetting = "SINGLE"
		flags.StringVar(&options.assetID, "asset-id", "", "")
		flags.StringVar(&options.externalAction, "external-action", "", "")
		flags.StringVar(&options.deepExternalAction, "deep-external-action", "", "")
		flags.StringVar(&options.deliveryMode, "delivery-mode", "", "")
		flags.StringVar(&options.landingType, "landing-type", "", "")
		flags.StringVar(&options.adType, "ad-type", "", "")
		flags.StringVar(&options.marketingGoal, "marketing-goal", "", "")
		flags.StringVar(&options.productSetting, "product-setting", options.productSetting, "")
		flags.StringVar(&options.valueOptimizedType, "value-optimized-type", "", "")
	case "goals":
		options.deliveryType = "NORMAL"
		noAssetID := false
		flags.StringVar(&options.landingType, "landing-type", "", "")
		flags.StringVar(&options.adType, "ad-type", "", "")
		flags.StringVar(&options.assetType, "asset-type", "", "")
		flags.StringVar(&options.marketingGoal, "marketing-goal", "", "")
		flags.StringVar(&options.deliveryMode, "delivery-mode", "", "")
		flags.StringVar(&options.deliveryType, "delivery-type", options.deliveryType, "")
		flags.StringVar(&options.assetID, "asset-id", "", "")
		flags.BoolVar(&noAssetID, "no-asset-id", false, "")
		if err := flags.Parse(args); err != nil {
			return marketingDiscoveryOptions{}, err
		}
		options.includeAsset = !noAssetID
		return normalizeMarketingDiscoveryOptions(action, options, flags.Args())
	case "cities":
		options.countryCodes = repeatedValues{"CN", "CHN", "156"}
		flags.StringVar(&options.cityCSV, "city-csv", "", "")
		flags.Var(&options.countryCodes, "country-code", "")
		flags.BoolVar(&options.writeConfig, "write-config", false, "")
	default:
		return marketingDiscoveryOptions{}, errors.New("unsupported Marketing discovery action")
	}
	if err := flags.Parse(args); err != nil {
		return marketingDiscoveryOptions{}, err
	}
	return normalizeMarketingDiscoveryOptions(action, options, flags.Args())
}

func normalizeMarketingDiscoveryOptions(
	action string,
	options marketingDiscoveryOptions,
	positional []string,
) (marketingDiscoveryOptions, error) {
	if len(positional) != 0 {
		return marketingDiscoveryOptions{}, errors.New("unexpected positional Marketing discovery arguments")
	}
	options.channel = strings.ToLower(strings.TrimSpace(options.channel))
	options.authAccountID = strings.TrimSpace(options.authAccountID)
	options.advertiserID = strings.TrimSpace(options.advertiserID)
	options.name = strings.TrimSpace(options.name)
	options.landingType = strings.TrimSpace(options.landingType)
	options.marketingGoal = strings.TrimSpace(options.marketingGoal)
	options.deliveryMode = strings.TrimSpace(options.deliveryMode)
	options.projectID = strings.TrimSpace(options.projectID)
	options.mode = strings.TrimSpace(options.mode)
	options.assetType = strings.TrimSpace(options.assetType)
	options.assetID = strings.TrimSpace(options.assetID)
	options.externalAction = strings.TrimSpace(options.externalAction)
	options.deepExternalAction = strings.TrimSpace(options.deepExternalAction)
	options.adType = strings.TrimSpace(options.adType)
	options.productSetting = strings.TrimSpace(options.productSetting)
	options.valueOptimizedType = strings.TrimSpace(options.valueOptimizedType)
	options.deliveryType = strings.TrimSpace(options.deliveryType)
	options.cityCSV = strings.TrimSpace(options.cityCSV)
	options.promotionIDs = splitRepeatedCSV(options.promotionIDs)
	options.assetIDs = splitRepeatedCSV(options.assetIDs)
	if options.channel != "marketing" {
		return marketingDiscoveryOptions{}, errors.New("--channel must be marketing for discover commands")
	}
	if options.authAccountID != "" {
		if err := domain.ValidateDecimalID(options.authAccountID, "auth_account_id"); err != nil {
			return marketingDiscoveryOptions{}, err
		}
	}
	if options.advertiserID != "" {
		if err := domain.ValidateDecimalID(options.advertiserID, "advertiser_id"); err != nil {
			return marketingDiscoveryOptions{}, err
		}
	}
	if options.page != 0 || options.pageSize != 0 {
		if options.page < 1 {
			return marketingDiscoveryOptions{}, errors.New("--page must be positive")
		}
		if options.pageSize < 1 || options.pageSize > applicationdiscovery.MaxPageSize {
			return marketingDiscoveryOptions{}, errors.New("--page-size must be between 1 and 100")
		}
	}
	for index, value := range options.promotionIDs {
		if err := domain.ValidateDecimalID(value, fmt.Sprintf("promotion_id[%d]", index)); err != nil {
			return marketingDiscoveryOptions{}, err
		}
	}
	for index, value := range options.assetIDs {
		if err := domain.ValidateDecimalID(value, fmt.Sprintf("asset_id[%d]", index)); err != nil {
			return marketingDiscoveryOptions{}, err
		}
	}
	if options.projectID != "" {
		if err := domain.ValidateDecimalID(options.projectID, "project_id"); err != nil {
			return marketingDiscoveryOptions{}, err
		}
	}
	if options.assetID != "" {
		if err := domain.ValidateDecimalID(options.assetID, "asset_id"); err != nil {
			return marketingDiscoveryOptions{}, err
		}
	}
	if action == "dpa" && !containsString([]string{"meta", "dict", "ebp-detail", "asset-detail"}, options.mode) {
		return marketingDiscoveryOptions{}, errors.New("--mode must be meta, dict, ebp-detail, or asset-detail")
	}
	switch action {
	case "projects":
		if err := applicationdiscovery.ValidateProjectFilters(
			options.landingType, options.marketingGoal, options.deliveryMode,
		); err != nil {
			return marketingDiscoveryOptions{}, err
		}
	case "events":
		if err := applicationdiscovery.ValidateEventAssetType(options.assetType); err != nil {
			return marketingDiscoveryOptions{}, err
		}
	case "deep-bids":
		if err := applicationdiscovery.ValidateDeepBidFilters(applicationdiscovery.DeepBidQuery{
			ExternalAction: options.externalAction, DeepExternalAction: options.deepExternalAction,
			DeliveryMode: options.deliveryMode, LandingType: options.landingType, AdType: options.adType,
			MarketingGoal: options.marketingGoal, ProductSetting: options.productSetting,
			ValueOptimizedType: options.valueOptimizedType,
		}, false); err != nil {
			return marketingDiscoveryOptions{}, err
		}
	case "goals":
		if err := applicationdiscovery.ValidateGoalFilters(applicationdiscovery.GoalQuery{
			LandingType: options.landingType, AdType: options.adType, AssetType: options.assetType,
			MarketingGoal: options.marketingGoal, DeliveryMode: options.deliveryMode,
			DeliveryType: options.deliveryType,
		}, false); err != nil {
			return marketingDiscoveryOptions{}, err
		}
	}
	if action == "cities" {
		if options.cityCSV == "" {
			return marketingDiscoveryOptions{}, errors.New("--city-csv is required")
		}
		codes := repeatedValues{}
		for _, value := range options.countryCodes {
			if value = strings.TrimSpace(value); value != "" {
				codes = append(codes, value)
			}
		}
		if len(codes) == 0 {
			return marketingDiscoveryOptions{}, errors.New("at least one --country-code is required")
		}
		options.countryCodes = codes
	}
	return options, nil
}

func (runner Runner) runMarketingDiscovery(
	ctx context.Context,
	action string,
	args []string,
	cwd string,
	userHome string,
	stateRoot string,
	getenv func(string) string,
	credentialStore authapplication.CredentialStore,
	stdout io.Writer,
) int {
	options, err := parseMarketingDiscoveryOptions(action, args)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("invalid_arguments", err.Error(), 2, nil))
		return 2
	}
	if action == "cities" {
		options.cityNames, err = readMarketingCityNames(options.cityCSV)
		if err != nil {
			WriteDomainError(stdout, domain.NewError("invalid_arguments", err.Error(), 2, nil))
			return 2
		}
	}
	configPath := filepath.Clean(filesystem.ResolveConfigPath(options.configPath, cwd, getenv, userHome))
	runtime := runner.MarketingDiscovery
	store := MarketingDiscoveryConfigStore(filesystem.ConfigStore{Path: configPath})
	if runtime.ConfigFactory != nil {
		store = runtime.ConfigFactory(configPath)
	}
	if store == nil {
		WriteDomainError(stdout, domain.NewError("configuration_error", "Marketing discovery config store is unavailable", 2, nil))
		return 2
	}
	rawConfig, revision, err := store.ReadWithRevision(ctx)
	if err != nil {
		WriteDomainError(stdout, domain.WrapError("configuration_error", "failed to read Marketing discovery config", 2, err))
		return 2
	}
	options, err = resolveMarketingDiscoveryConfig(action, options, rawConfig)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
		return 2
	}
	service := runtime.Service
	if service == nil {
		factory := runtime.ClientFactory
		if factory == nil {
			factory, err = oceanengine.NewClientFactory(oceanengine.FactoryOptions{})
			if err != nil {
				WriteDomainError(stdout, domain.NewError("unexpected_error", err.Error(), 1, nil))
				return 1
			}
		}
		authorizations := runtime.Authorizations
		if authorizations == nil {
			authorizations = filesystem.AuthorizationStore{Root: stateRoot}
		}
		refreshLocker := runtime.RefreshLocker
		if refreshLocker == nil {
			refreshLocker = filesystem.RefreshLocker{StateRoot: stateRoot}
		}
		oauth := runtime.OAuth
		if oauth == nil {
			oauth = oceanengine.OAuthAdapter{Factory: factory}
		}
		reader := runtime.Reader
		if reader == nil {
			reader = oceanengine.MarketingDiscoveryAdapter{Factory: factory, Retry: runtime.Retry}
		}
		service = applicationdiscovery.Service{
			Tokens: &authapplication.TokenManager{
				Credentials: credentialStore, Authorizations: authorizations,
				Locks: refreshLocker, OAuth: oauth, Now: runtime.Now,
			},
			Reader: reader,
		}
	}
	return runMarketingDiscoveryQuery(ctx, action, options, rawConfig, revision, configPath, store, service, stdout)
}

func resolveMarketingDiscoveryConfig(
	action string,
	options marketingDiscoveryOptions,
	raw map[string]any,
) (marketingDiscoveryOptions, error) {
	config, selected, err := configuration.Runtime(raw, "marketing", "query")
	if err != nil {
		return marketingDiscoveryOptions{}, err
	}
	if selected.ID != "marketing" {
		return marketingDiscoveryOptions{}, errors.New("Marketing discovery config resolved another channel")
	}
	if options.advertiserID == "" {
		options.advertiserID = marketingDiscoveryString(config, "account.advertiser_id")
	}
	if err := domain.ValidateDecimalID(options.advertiserID, "account.advertiser_id"); err != nil {
		return marketingDiscoveryOptions{}, err
	}
	switch action {
	case "dpa":
		options.platformID = marketingDiscoveryString(config, "resolved_ids.product_platform_id")
		options.uniqueProductID = marketingDiscoveryString(config, "resolved_ids.unique_product_id")
		if err := domain.ValidateDecimalID(options.platformID, "resolved_ids.product_platform_id"); err != nil {
			return marketingDiscoveryOptions{}, err
		}
		if options.mode == "ebp-detail" || options.mode == "asset-detail" {
			if err := domain.ValidateDecimalID(options.uniqueProductID, "resolved_ids.unique_product_id"); err != nil {
				return marketingDiscoveryOptions{}, err
			}
		}
	case "deep-bids":
		if options.assetID == "" {
			options.assetID = marketingDiscoveryFirstString(config, "resolved_ids.event_asset_ids")
		}
		options.externalAction = marketingDiscoveryDefault(options.externalAction, config, "defaults.external_action")
		options.deepExternalAction = marketingDiscoveryDefault(options.deepExternalAction, config, "defaults.deep_external_action")
		options.deliveryMode = marketingDiscoveryDefault(options.deliveryMode, config, "defaults.delivery_mode")
		options.landingType = marketingDiscoveryDefault(options.landingType, config, "defaults.landing_type")
		options.adType = marketingDiscoveryDefault(options.adType, config, "defaults.ad_type")
		options.marketingGoal = marketingDiscoveryDefault(options.marketingGoal, config, "defaults.marketing_goal")
		options.valueOptimizedType = marketingDiscoveryDefault(options.valueOptimizedType, config, "defaults.value_optimized_type")
	case "goals":
		options.landingType = marketingDiscoveryDefault(options.landingType, config, "defaults.landing_type")
		options.adType = marketingDiscoveryDefault(options.adType, config, "defaults.ad_type")
		options.assetType = marketingDiscoveryDefault(options.assetType, config, "defaults.asset_type")
		options.marketingGoal = marketingDiscoveryDefault(options.marketingGoal, config, "defaults.marketing_goal")
		options.deliveryMode = marketingDiscoveryDefault(options.deliveryMode, config, "defaults.delivery_mode")
		if options.includeAsset && options.assetID == "" {
			options.assetID = marketingDiscoveryString(config, "resolved_ids.product_platform_id")
		}
	}
	return options, nil
}

func marketingDiscoveryDefault(current string, config map[string]any, path string) string {
	if strings.TrimSpace(current) != "" {
		return strings.TrimSpace(current)
	}
	return marketingDiscoveryString(config, path)
}

func marketingDiscoveryString(config map[string]any, path string) string {
	value := marketingDiscoveryValue(config, path)
	if value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" || configuration.Missing(text) {
		return ""
	}
	return text
}

func marketingDiscoveryFirstString(config map[string]any, path string) string {
	value := marketingDiscoveryValue(config, path)
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	case []string:
		for _, item := range typed {
			if item = strings.TrimSpace(item); item != "" {
				return item
			}
		}
	}
	return ""
}

func marketingDiscoveryValue(config map[string]any, path string) any {
	if value := configuration.Value(config, path); !configuration.Missing(value) {
		return value
	}
	return configuration.Value(config, "default_plan_template."+path)
}

func runMarketingDiscoveryQuery(
	ctx context.Context,
	action string,
	options marketingDiscoveryOptions,
	rawConfig map[string]any,
	revision string,
	configPath string,
	store MarketingDiscoveryConfigStore,
	service MarketingDiscoveryService,
	stdout io.Writer,
) int {
	if service == nil {
		WriteDomainError(stdout, domain.NewError("unexpected_error", "Marketing discovery service is unavailable", 1, nil))
		return 1
	}
	scope := applicationdiscovery.CredentialScope{
		AdvertiserID: options.advertiserID, AuthAccountID: options.authAccountID,
	}
	var result any
	var err error
	exitCode := 0
	switch action {
	case "projects":
		var response applicationdiscovery.Result
		response, err = service.QueryProjects(ctx, applicationdiscovery.ProjectQuery{
			CredentialScope: scope, Name: options.name, LandingType: options.landingType,
			MarketingGoal: options.marketingGoal, DeliveryMode: options.deliveryMode,
			Page: options.page, PageSize: options.pageSize,
		})
		result = response
		if response.ResponseCode != 0 {
			exitCode = 1
		}
	case "promotions":
		var response applicationdiscovery.Result
		response, err = service.QueryPromotions(ctx, applicationdiscovery.PromotionQuery{
			CredentialScope: scope, Name: options.name, ProjectID: options.projectID,
			PromotionIDs: []string(options.promotionIDs), Page: options.page, PageSize: options.pageSize,
		})
		result = response
		if response.ResponseCode != 0 {
			exitCode = 1
		}
	case "dpa":
		var response applicationdiscovery.Result
		response, err = service.QueryDPA(ctx, applicationdiscovery.DPAQuery{
			CredentialScope: scope, Mode: options.mode, PlatformID: options.platformID,
			UniqueProductID: options.uniqueProductID, Page: options.page, PageSize: options.pageSize,
		})
		result = response
		if response.ResponseCode != 0 {
			exitCode = 1
		}
	case "events":
		var response applicationdiscovery.Result
		response, err = service.QueryEvents(ctx, applicationdiscovery.EventQuery{
			CredentialScope: scope, AssetType: options.assetType, AssetIDs: []string(options.assetIDs),
			Page: options.page, PageSize: options.pageSize,
		})
		result = response
		if response.ResponseCode != 0 {
			exitCode = 1
		}
	case "deep-bids":
		var response applicationdiscovery.Result
		response, err = service.QueryDeepBids(ctx, applicationdiscovery.DeepBidQuery{
			CredentialScope: scope, AssetID: options.assetID, ExternalAction: options.externalAction,
			DeepExternalAction: options.deepExternalAction, DeliveryMode: options.deliveryMode,
			LandingType: options.landingType, AdType: options.adType, MarketingGoal: options.marketingGoal,
			ProductSetting: options.productSetting, ValueOptimizedType: options.valueOptimizedType,
		})
		result = response
		if response.ResponseCode != 0 {
			exitCode = 1
		}
	case "goals":
		var response applicationdiscovery.Result
		response, err = service.QueryGoals(ctx, applicationdiscovery.GoalQuery{
			CredentialScope: scope, LandingType: options.landingType, AdType: options.adType,
			AssetType: options.assetType, MarketingGoal: options.marketingGoal,
			DeliveryMode: options.deliveryMode, DeliveryType: options.deliveryType,
			AssetID: options.assetID, IncludeAsset: options.includeAsset,
		})
		result = response
		if response.ResponseCode != 0 {
			exitCode = 1
		}
	case "cities":
		var response applicationdiscovery.CityResult
		response, err = service.ResolveCities(ctx, applicationdiscovery.CityQuery{
			CredentialScope: scope, CityCSV: options.cityCSV, CityNames: options.cityNames,
			CountryCodes: []string(options.countryCodes),
		})
		if err == nil && response.ResolvedCount > 0 && len(response.Missing) == 0 {
			if options.writeConfig {
				err = commitMarketingCities(ctx, store, revision, rawConfig, response)
				if err == nil {
					response.ConfigUpdated = configPath
				}
			}
		} else if err == nil {
			exitCode = 1
		}
		result = response
	default:
		err = errors.New("unsupported Marketing discovery action")
	}
	if err != nil {
		mapped := marketingDiscoveryError(err)
		WriteDomainError(stdout, mapped)
		return mapped.ExitCode
	}
	if err := WriteJSONDestination(stdout, result, options.out); err != nil {
		WriteDomainError(stdout, domain.WrapError("configuration_error", "failed to write Marketing discovery result", 2, err))
		return 2
	}
	return exitCode
}

func commitMarketingCities(
	ctx context.Context,
	store MarketingDiscoveryConfigStore,
	revision string,
	raw map[string]any,
	result applicationdiscovery.CityResult,
) error {
	updated := configuration.CloneMap(raw)
	resolved, exists := updated["resolved_ids"]
	if !exists || resolved == nil {
		resolved = map[string]any{}
		updated["resolved_ids"] = resolved
	}
	resolvedIDs, ok := resolved.(map[string]any)
	if !ok {
		return errors.New("resolved_ids must be an object")
	}
	cityIDs := make([]any, 0, len(result.Resolved))
	cityNames := make([]any, 0, len(result.Resolved))
	for _, city := range result.Resolved {
		cityIDs = append(cityIDs, city.Code)
		cityNames = append(cityNames, city.Name)
	}
	resolvedIDs["city_ids"] = cityIDs
	resolvedIDs["city_names"] = cityNames
	return store.CompareAndSwap(ctx, revision, updated)
}

func readMarketingCityNames(path string) ([]string, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read city CSV: %w", err)
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read city CSV header: %w", err)
	}
	if len(header) != 0 {
		header[0] = strings.TrimPrefix(header[0], "\ufeff")
	}
	cityColumn := -1
	for index, value := range header {
		value = strings.TrimSpace(value)
		if value == "city" || value == "城市" {
			cityColumn = index
			break
		}
	}
	if cityColumn < 0 {
		return nil, errors.New("city CSV must contain a city or 城市 header")
	}
	names := []string{}
	for {
		row, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read city CSV row: %w", readErr)
		}
		if cityColumn >= len(row) {
			continue
		}
		if name := strings.TrimSpace(row[cityColumn]); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, errors.New("city CSV contains no city names")
	}
	return names, nil
}

func marketingDiscoveryError(err error) *domain.Error {
	var domainErr *domain.Error
	if errors.As(err, &domainErr) {
		return domainErr
	}
	if errors.Is(err, context.Canceled) {
		return domain.NewError("interrupted", "operation interrupted", 130, nil)
	}
	if strings.Contains(err.Error(), "configuration changed while this operation was running") ||
		strings.Contains(err.Error(), "resolved_ids must be an object") {
		return domain.NewError("configuration_error", err.Error(), 2, nil)
	}
	var envelope *oceanengine.EnvelopeError
	if errors.As(err, &envelope) {
		details := map[string]any{}
		if envelope.Code != 0 {
			details["api_code"] = envelope.Code
		}
		if envelope.HTTPStatus != 0 {
			details["http_status"] = envelope.HTTPStatus
		}
		if envelope.RequestID != "" {
			details["request_id"] = envelope.RequestID
		}
		return domain.NewError("api_error", envelope.Error(), 1, details)
	}
	return domain.NewError("marketing_discovery_failed", err.Error(), 1, nil)
}
