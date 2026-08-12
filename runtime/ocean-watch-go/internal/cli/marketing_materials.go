package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/oceanengine"
	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	applicationmaterials "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/materials"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
	portmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/marketing"
)

type MarketingMaterialService interface {
	QueryVideos(context.Context, applicationmaterials.VideoQuery) (applicationmaterials.VideoResult, error)
	QueryCreator(context.Context, applicationmaterials.CreatorQuery) (applicationmaterials.CreatorResult, error)
	QueryImages(context.Context, applicationmaterials.ImageQuery) (applicationmaterials.ImageResult, error)
	QueryProducts(context.Context, applicationmaterials.ProductQuery) (applicationmaterials.ProductResult, error)
}

type MarketingMaterialRuntime struct {
	Service        MarketingMaterialService
	Reader         portmarketing.MaterialReader
	Authorizations authapplication.AuthorizationStore
	RefreshLocker  authapplication.RefreshLocker
	OAuth          authapplication.OAuthAdapter
	ClientFactory  *oceanengine.ClientFactory
	Retry          platformretry.Policy
	Now            func() time.Time
	ConfigFactory  func(string) applicationmaterials.ConfigReader
}

type marketingMaterialOptions struct {
	configPath           string
	channel              string
	authAccountID        string
	advertiserID         string
	out                  string
	mode                 string
	videoIDs             repeatedValues
	materialIDs          repeatedValues
	signatures           repeatedValues
	filename             string
	date                 string
	startTime            string
	endTime              string
	page                 int
	pageSize             int
	fetchAll             bool
	awemeIDs             repeatedValues
	itemIDs              repeatedValues
	minimumRemainingDays int
	includeUnusable      bool
	path                 string
	productPlatformID    string
	productID            string
	name                 string
	imageIDs             repeatedValues
}

func parseMarketingMaterialOptions(action string, args []string) (marketingMaterialOptions, error) {
	options := marketingMaterialOptions{
		channel: "marketing", page: 1, pageSize: applicationmaterials.DefaultPageSize,
		minimumRemainingDays: 1, path: applicationmaterials.ProductEndpoint,
	}
	flags := flag.NewFlagSet("materials "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.StringVar(&options.channel, "channel", "marketing", "")
	flags.StringVar(&options.authAccountID, "auth-account-id", "", "")
	flags.StringVar(&options.out, "out", "", "")
	switch action {
	case "videos":
		options.mode = "library-get"
		flags.StringVar(&options.advertiserID, "advertiser-id", "", "")
		flags.Var(&options.videoIDs, "video-id", "")
		flags.Var(&options.materialIDs, "material-id", "")
		flags.Var(&options.signatures, "signature", "")
		flags.StringVar(&options.filename, "filename", "", "")
		flags.StringVar(&options.date, "date", "", "")
		flags.StringVar(&options.startTime, "start-time", "", "")
		flags.StringVar(&options.endTime, "end-time", "", "")
		flags.IntVar(&options.page, "page", 1, "")
		flags.IntVar(&options.pageSize, "page-size", applicationmaterials.DefaultPageSize, "")
		flags.BoolVar(&options.fetchAll, "fetch-all", false, "")
		flags.StringVar(&options.mode, "mode", "library-get", "")
	case "creator":
		options.mode = "authorized"
		options.pageSize = applicationmaterials.DefaultCreatorPageSize
		flags.StringVar(&options.advertiserID, "advertiser-id", "", "")
		flags.Var(&options.awemeIDs, "aweme-id", "")
		flags.StringVar(&options.mode, "source", "authorized", "")
		flags.Var(&options.itemIDs, "item-id", "")
		flags.IntVar(&options.minimumRemainingDays, "minimum-remaining-days", 1, "")
		flags.IntVar(&options.pageSize, "page-size", applicationmaterials.DefaultCreatorPageSize, "")
		flags.BoolVar(&options.includeUnusable, "include-unusable", false, "")
	case "images":
		options.mode = "ad-get"
		flags.Var(&options.imageIDs, "image-id", "")
		flags.Var(&options.materialIDs, "material-id", "")
		flags.IntVar(&options.page, "page", 1, "")
		flags.IntVar(&options.pageSize, "page-size", applicationmaterials.DefaultPageSize, "")
		flags.StringVar(&options.mode, "mode", "ad-get", "")
	case "products":
		flags.StringVar(&options.path, "path", applicationmaterials.ProductEndpoint, "")
		flags.IntVar(&options.page, "page", 1, "")
		flags.IntVar(&options.pageSize, "page-size", applicationmaterials.DefaultPageSize, "")
		flags.StringVar(&options.productPlatformID, "product-platform-id", "", "")
		flags.StringVar(&options.productID, "product-id", "", "")
		flags.StringVar(&options.name, "name", "", "")
	default:
		return marketingMaterialOptions{}, errors.New("unsupported Marketing material action")
	}
	if err := flags.Parse(args); err != nil {
		return marketingMaterialOptions{}, err
	}
	if len(flags.Args()) != 0 {
		return marketingMaterialOptions{}, errors.New("unexpected positional Marketing material arguments")
	}
	options.channel = strings.TrimSpace(options.channel)
	options.authAccountID = strings.TrimSpace(options.authAccountID)
	options.advertiserID = strings.TrimSpace(options.advertiserID)
	options.out = strings.TrimSpace(options.out)
	options.mode = strings.TrimSpace(options.mode)
	options.filename = strings.TrimSpace(options.filename)
	options.date = strings.TrimSpace(options.date)
	options.startTime = strings.TrimSpace(options.startTime)
	options.endTime = strings.TrimSpace(options.endTime)
	options.path = strings.TrimSpace(options.path)
	options.productPlatformID = strings.TrimSpace(options.productPlatformID)
	options.productID = strings.TrimSpace(options.productID)
	options.name = strings.TrimSpace(options.name)
	options.videoIDs = splitRepeatedCSV(options.videoIDs)
	options.materialIDs = splitRepeatedCSV(options.materialIDs)
	options.signatures = splitRepeatedCSV(options.signatures)
	options.awemeIDs = splitRepeatedCSV(options.awemeIDs)
	options.itemIDs = splitRepeatedCSV(options.itemIDs)
	options.imageIDs = splitRepeatedCSV(options.imageIDs)
	if err := validateMarketingMaterialOptions(action, options); err != nil {
		return marketingMaterialOptions{}, err
	}
	return options, nil
}

func validateMarketingMaterialOptions(action string, options marketingMaterialOptions) error {
	if options.channel != "marketing" {
		return errors.New("materials commands only support --channel marketing")
	}
	if options.authAccountID != "" {
		if err := domain.ValidateDecimalID(options.authAccountID, "auth_account_id"); err != nil {
			return err
		}
	}
	if options.advertiserID != "" {
		if err := domain.ValidateDecimalID(options.advertiserID, "advertiser_id"); err != nil {
			return err
		}
	}
	switch action {
	case "videos":
		if !containsString([]string{"ad-get", "library-get", "cover-suggest"}, options.mode) {
			return errors.New("--mode must be ad-get, library-get, or cover-suggest")
		}
		if err := validateMarketingPage(options.page, options.pageSize); err != nil {
			return err
		}
		if options.mode == "library-get" && nonEmptyRepeated(options.videoIDs, options.materialIDs, options.signatures) > 1 {
			return errors.New("use only one of --material-id, --video-id, or --signature for library-get filtering")
		}
		if err := validateMarketingIDValues(options.materialIDs, "material_id"); err != nil {
			return err
		}
		if err := validateMarketingDateOptions(options); err != nil {
			return err
		}
		if len(options.videoIDs) > applicationmaterials.MaxBatchSize || len(options.materialIDs) > applicationmaterials.MaxBatchSize || len(options.signatures) > applicationmaterials.MaxBatchSize {
			return errors.New("video material filters accept at most 100 values")
		}
	case "creator":
		if options.mode != "authorized" && options.mode != "homepage" {
			return errors.New("--source must be authorized or homepage")
		}
		if options.mode == "homepage" && len(options.awemeIDs) != 1 {
			return errors.New("homepage source requires exactly one --aweme-id")
		}
		if options.minimumRemainingDays < 0 {
			return errors.New("--minimum-remaining-days must not be negative")
		}
		if options.pageSize < 1 || options.pageSize > applicationmaterials.MaxPageSize {
			return errors.New("--page-size must be between 1 and 100")
		}
		if err := validateMarketingIDValues(options.itemIDs, "item_id"); err != nil {
			return err
		}
		if len(options.awemeIDs) > applicationmaterials.MaxBatchSize || len(options.itemIDs) > applicationmaterials.MaxBatchSize {
			return errors.New("creator filters accept at most 100 values")
		}
	case "images":
		if options.mode != "ad-get" && options.mode != "library-get" {
			return errors.New("--mode must be ad-get or library-get")
		}
		if err := validateMarketingPage(options.page, options.pageSize); err != nil {
			return err
		}
		if options.mode == "library-get" && len(options.imageIDs) != 0 && len(options.materialIDs) != 0 {
			return errors.New("use only one of --image-id or --material-id for library-get filtering")
		}
		if err := validateMarketingIDValues(options.materialIDs, "material_id"); err != nil {
			return err
		}
		if len(options.imageIDs) > applicationmaterials.MaxBatchSize || len(options.materialIDs) > applicationmaterials.MaxBatchSize {
			return errors.New("image material filters accept at most 100 values")
		}
	case "products":
		if options.path != applicationmaterials.ProductEndpoint {
			return fmt.Errorf("--path is frozen to %s", applicationmaterials.ProductEndpoint)
		}
		if err := validateMarketingPage(options.page, options.pageSize); err != nil {
			return err
		}
		if options.productID != "" {
			return domain.ValidateDecimalID(options.productID, "product_id")
		}
	}
	return nil
}

func validateMarketingPage(page, pageSize int) error {
	if page < 1 {
		return errors.New("--page must be positive")
	}
	if pageSize < 1 || pageSize > applicationmaterials.MaxPageSize {
		return errors.New("--page-size must be between 1 and 100")
	}
	return nil
}

func validateMarketingDateOptions(options marketingMaterialOptions) error {
	if options.date != "" {
		value := strings.ToLower(options.date)
		if value != "today" && value != "yesterday" {
			if _, err := time.Parse("2006-01-02", options.date); err != nil {
				return errors.New("--date must be today, yesterday, or yyyy-mm-dd")
			}
		}
	}
	start, end := datePrefix(options.startTime), datePrefix(options.endTime)
	if start != "" {
		if _, err := time.Parse("2006-01-02", start); err != nil {
			return errors.New("--start-time must use yyyy-mm-dd")
		}
	}
	if end != "" {
		if _, err := time.Parse("2006-01-02", end); err != nil {
			return errors.New("--end-time must use yyyy-mm-dd")
		}
	}
	if start != "" && end != "" && start > end {
		return errors.New("--start-time must not be after --end-time")
	}
	return nil
}

func validateMarketingIDValues(values []string, field string) error {
	for index, value := range values {
		if err := domain.ValidateDecimalID(value, fmt.Sprintf("%s[%d]", field, index)); err != nil {
			return err
		}
	}
	return nil
}

func nonEmptyRepeated(groups ...repeatedValues) int {
	count := 0
	for _, group := range groups {
		if len(group) != 0 {
			count++
		}
	}
	return count
}

func datePrefix(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 10 {
		return value[:10]
	}
	return value
}

func (runner Runner) runMarketingMaterials(
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
	options, err := parseMarketingMaterialOptions(action, args)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("invalid_arguments", err.Error(), 2, nil))
		return 2
	}
	configPath := filepath.Clean(filesystem.ResolveConfigPath(options.configPath, cwd, getenv, userHome))
	runtime := runner.MarketingMaterials
	configReader := applicationmaterials.ConfigReader(filesystem.ConfigStore{Path: configPath})
	if runtime.ConfigFactory != nil {
		configReader = runtime.ConfigFactory(configPath)
	}
	if configReader == nil {
		WriteDomainError(stdout, domain.NewError("configuration_error", "Marketing material config reader is unavailable", 2, nil))
		return 2
	}
	rawConfig, err := configReader.Read(ctx)
	if err != nil {
		WriteDomainError(stdout, domain.WrapError("configuration_error", "failed to read Marketing material config", 2, err))
		return 2
	}
	options, err = resolveMarketingMaterialConfig(action, options, rawConfig)
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
			reader = oceanengine.MarketingMaterialsAdapter{Factory: factory, Retry: runtime.Retry}
		}
		service = applicationmaterials.Service{
			Tokens: &authapplication.TokenManager{
				Credentials: credentialStore, Authorizations: authorizations,
				Locks: refreshLocker, OAuth: oauth, Now: runtime.Now,
			},
			Reader: reader, Now: runtime.Now,
		}
	}
	return runMarketingMaterialQuery(ctx, action, options, service, stdout)
}

func resolveMarketingMaterialConfig(
	action string,
	options marketingMaterialOptions,
	raw map[string]any,
) (marketingMaterialOptions, error) {
	config, selected, err := configuration.Runtime(raw, "marketing", "query")
	if err != nil {
		return marketingMaterialOptions{}, err
	}
	if selected.ID != "marketing" {
		return marketingMaterialOptions{}, errors.New("Marketing material config resolved another channel")
	}
	if options.advertiserID == "" {
		options.advertiserID = marketingConfigString(config, "account.advertiser_id")
	}
	switch action {
	case "videos":
		if len(options.videoIDs) == 0 && (options.mode == "ad-get" || options.mode == "cover-suggest") {
			options.videoIDs, err = marketingConfigStrings(config, "materials.video_ids")
		}
	case "images":
		if len(options.imageIDs) == 0 && options.mode == "ad-get" {
			options.imageIDs, err = marketingConfigStrings(config, "resolved_ids.product_image_ids")
		}
	case "products":
		if options.productPlatformID == "" {
			options.productPlatformID = marketingConfigString(config, "resolved_ids.product_platform_id")
		}
		if options.productID == "" {
			options.productID = marketingConfigString(config, "defaults.product_id")
		}
	}
	if err != nil {
		return marketingMaterialOptions{}, err
	}
	if err := domain.ValidateDecimalID(options.advertiserID, "account.advertiser_id"); err != nil {
		return marketingMaterialOptions{}, err
	}
	if err := validateMarketingMaterialOptions(action, options); err != nil {
		return marketingMaterialOptions{}, err
	}
	return options, nil
}

func marketingConfigString(config map[string]any, path string) string {
	value := configuration.Value(config, path)
	if value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func marketingConfigStrings(config map[string]any, path string) (repeatedValues, error) {
	value := configuration.Value(config, path)
	if value == nil {
		return repeatedValues{}, nil
	}
	result := repeatedValues{}
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if item == nil {
				continue
			}
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" && text != "<nil>" {
				result = append(result, text)
			}
		}
	case []string:
		result = append(result, typed...)
	default:
		return nil, fmt.Errorf("%s must be a list", path)
	}
	return splitRepeatedCSV(result), nil
}

func runMarketingMaterialQuery(
	ctx context.Context,
	action string,
	options marketingMaterialOptions,
	service MarketingMaterialService,
	stdout io.Writer,
) int {
	if service == nil {
		WriteDomainError(stdout, domain.NewError("unexpected_error", "Marketing material service is unavailable", 1, nil))
		return 1
	}
	scope := applicationmaterials.CredentialScope{
		AdvertiserID: options.advertiserID, AuthAccountID: options.authAccountID,
	}
	var result any
	var err error
	switch action {
	case "videos":
		result, err = service.QueryVideos(ctx, applicationmaterials.VideoQuery{
			CredentialScope: scope, Mode: options.mode, VideoIDs: []string(options.videoIDs),
			MaterialIDs: []string(options.materialIDs), Signatures: []string(options.signatures),
			Filename: options.filename, Date: options.date, StartTime: options.startTime,
			EndTime: options.endTime, Page: options.page, PageSize: options.pageSize, FetchAll: options.fetchAll,
		})
	case "creator":
		result, err = service.QueryCreator(ctx, applicationmaterials.CreatorQuery{
			CredentialScope: scope, Source: options.mode, AwemeIDs: []string(options.awemeIDs),
			ItemIDs: []string(options.itemIDs), MinimumRemainingDays: options.minimumRemainingDays,
			PageSize: options.pageSize, MaxPages: applicationmaterials.DefaultMaxPages,
			IncludeUnusable: options.includeUnusable,
		})
	case "images":
		result, err = service.QueryImages(ctx, applicationmaterials.ImageQuery{
			CredentialScope: scope, Mode: options.mode, ImageIDs: []string(options.imageIDs),
			MaterialIDs: []string(options.materialIDs), Page: options.page, PageSize: options.pageSize,
		})
	case "products":
		result, err = service.QueryProducts(ctx, applicationmaterials.ProductQuery{
			CredentialScope: scope, Path: options.path, ProductPlatformID: options.productPlatformID,
			ProductID: options.productID, Name: options.name, Page: options.page, PageSize: options.pageSize,
		})
	default:
		err = errors.New("unsupported Marketing material action")
	}
	if err != nil {
		mapped := marketingMaterialError(err)
		WriteDomainError(stdout, mapped)
		return mapped.ExitCode
	}
	if err := WriteJSONDestination(stdout, result, options.out); err != nil {
		WriteDomainError(stdout, domain.WrapError("configuration_error", "failed to write Marketing material result", 2, err))
		return 2
	}
	return 0
}

func marketingMaterialError(err error) *domain.Error {
	var domainErr *domain.Error
	if errors.As(err, &domainErr) {
		return domainErr
	}
	if errors.Is(err, context.Canceled) {
		return domain.NewError("interrupted", "operation interrupted", 130, nil)
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
	return domain.NewError("marketing_material_query_failed", err.Error(), 1, nil)
}
