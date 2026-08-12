package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/oceanengine"
	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	applicationqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/qianchuan"
	applicationtemplates "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/templates"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

type QianchuanReadService interface {
	QueryProducts(context.Context, applicationqianchuan.ProductQuery, string) (applicationqianchuan.ProductResult, error)
	ListPlans(context.Context, applicationqianchuan.PlanListQuery) (applicationqianchuan.PlanListResult, error)
	ShowPlan(context.Context, applicationqianchuan.CredentialScope, string) (applicationqianchuan.PlanDetailResult, error)
	ListPlanMaterials(context.Context, applicationqianchuan.PlanMaterialsQuery) (applicationqianchuan.PlanMaterialsResult, error)
	ListAuthorizedCreators(context.Context, applicationqianchuan.AuthorizedCreatorQuery) (applicationqianchuan.AuthorizedCreatorResult, error)
	QueryCreatorVideos(context.Context, applicationqianchuan.CreatorVideoQuery) (applicationqianchuan.CreatorVideoResult, error)
}

type QianchuanReadRuntime struct {
	Service        QianchuanReadService
	Reader         portqianchuan.Reader
	Authorizations authapplication.AuthorizationStore
	RefreshLocker  authapplication.RefreshLocker
	OAuth          authapplication.OAuthAdapter
	ClientFactory  *oceanengine.ClientFactory
	Retry          platformretry.Policy
	Now            func() time.Time
}

type repeatedValues []string

func (values *repeatedValues) String() string {
	return strings.Join(*values, ",")
}

func (values *repeatedValues) Set(value string) error {
	*values = append(*values, value)
	return nil
}

type qianchuanProductOptions struct {
	configPath     string
	advertiserID   string
	authAccountID  string
	productIDs     repeatedValues
	name           string
	tab            string
	awemeID        string
	onlyUnpromoted bool
	orderField     string
	orderType      string
	platform       string
	pageSize       int
	maxPages       int
	out            string
}

func parseQianchuanProductOptions(action string, args []string) (qianchuanProductOptions, error) {
	options := qianchuanProductOptions{
		tab: "ALL", orderField: "AUDIT_TIME", orderType: "DESC",
		pageSize: applicationqianchuan.DefaultPageSize, maxPages: applicationqianchuan.DefaultMaxPages,
	}
	flags := flag.NewFlagSet("qc-products "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.StringVar(&options.advertiserID, "advertiser-id", "", "")
	flags.StringVar(&options.authAccountID, "auth-account-id", "", "")
	flags.Var(&options.productIDs, "product-id", "")
	flags.StringVar(&options.name, "name", "", "")
	flags.StringVar(&options.tab, "tab", "ALL", "")
	flags.StringVar(&options.awemeID, "aweme-id", "", "")
	flags.BoolVar(&options.onlyUnpromoted, "only-unpromoted", false, "")
	flags.StringVar(&options.orderField, "order-field", "AUDIT_TIME", "")
	flags.StringVar(&options.orderType, "order-type", "DESC", "")
	flags.StringVar(&options.platform, "platform", "", "")
	flags.IntVar(&options.pageSize, "page-size", applicationqianchuan.DefaultPageSize, "")
	flags.IntVar(&options.maxPages, "max-pages", applicationqianchuan.DefaultMaxPages, "")
	flags.StringVar(&options.out, "out", "", "")
	if err := flags.Parse(args); err != nil {
		return qianchuanProductOptions{}, err
	}
	if len(flags.Args()) != 0 {
		return qianchuanProductOptions{}, errors.New("unexpected positional Qianchuan product arguments")
	}
	options.advertiserID = strings.TrimSpace(options.advertiserID)
	options.authAccountID = strings.TrimSpace(options.authAccountID)
	options.name = strings.TrimSpace(options.name)
	options.awemeID = strings.TrimSpace(options.awemeID)
	options.productIDs = splitRepeatedCSV(options.productIDs)
	if err := validateCLIPositiveID(options.advertiserID, "advertiser_id", true); err != nil {
		return qianchuanProductOptions{}, err
	}
	for index, productID := range options.productIDs {
		if err := validateCLIPositiveID(productID, "product_id["+strconv.Itoa(index)+"]", true); err != nil {
			return qianchuanProductOptions{}, err
		}
	}
	if err := validateCLIPositiveID(options.awemeID, "aweme_id", false); err != nil {
		return qianchuanProductOptions{}, err
	}
	if action == "search" && len(options.productIDs) == 0 && options.name == "" {
		return qianchuanProductOptions{}, errors.New("search requires --product-id or --name")
	}
	if action != "list" && action != "search" {
		return qianchuanProductOptions{}, errors.New("unsupported Qianchuan product action")
	}
	if !containsString([]string{"ALL", "BREAKTHROUGH_PRODUCT", "GOOD_BOOST", "NEW_PRODUCT", "SEARCH_TREND"}, options.tab) {
		return qianchuanProductOptions{}, errors.New("--tab is not supported")
	}
	if !containsString([]string{"SELL_NUM", "STOCK", "AUDIT_TIME"}, options.orderField) {
		return qianchuanProductOptions{}, errors.New("--order-field is not supported")
	}
	if !containsString([]string{"ASC", "DESC"}, options.orderType) {
		return qianchuanProductOptions{}, errors.New("--order-type must be ASC or DESC")
	}
	if options.platform != "" && !containsString([]string{"ECP_AWEME", "QIANCHUAN"}, options.platform) {
		return qianchuanProductOptions{}, errors.New("--platform must be ECP_AWEME or QIANCHUAN")
	}
	if options.pageSize < 1 || options.pageSize > applicationqianchuan.MaxProductPageSize {
		return qianchuanProductOptions{}, errors.New("--page-size must be between 1 and 100")
	}
	if options.maxPages < 1 || options.maxPages > applicationqianchuan.MaxAllowedPages {
		return qianchuanProductOptions{}, errors.New("--max-pages must be between 1 and 100")
	}
	return options, nil
}

type qianchuanPlanOptions struct {
	configPath    string
	advertiserID  string
	authAccountID string
	adID          string
	maxPages      int
	top           int
	full          bool
	out           string
}

type qianchuanMaterialOptions struct {
	configPath    string
	advertiserID  string
	authAccountID string
	query         string
	planTemplate  string
	douyinID      string
	creatorName   string
	pageSize      int
	maxPages      int
	out           string
}

func parseQianchuanMaterialOptions(action string, args []string) (qianchuanMaterialOptions, error) {
	options := qianchuanMaterialOptions{maxPages: applicationqianchuan.DefaultMaxPages}
	flags := flag.NewFlagSet("qc-materials "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.StringVar(&options.advertiserID, "advertiser-id", "", "")
	flags.StringVar(&options.authAccountID, "auth-account-id", "", "")
	flags.StringVar(&options.query, "query", "", "")
	flags.StringVar(&options.planTemplate, "plan-template", "", "")
	flags.StringVar(&options.douyinID, "douyin-id", "", "")
	flags.StringVar(&options.creatorName, "creator-name", "", "")
	flags.IntVar(&options.pageSize, "page-size", 0, "")
	flags.IntVar(&options.maxPages, "max-pages", applicationqianchuan.DefaultMaxPages, "")
	flags.StringVar(&options.out, "out", "", "")
	if err := flags.Parse(args); err != nil {
		return qianchuanMaterialOptions{}, err
	}
	if len(flags.Args()) != 0 {
		return qianchuanMaterialOptions{}, errors.New("unexpected positional Qianchuan material arguments")
	}
	options.advertiserID = strings.TrimSpace(options.advertiserID)
	options.authAccountID = strings.TrimSpace(options.authAccountID)
	options.query = strings.TrimSpace(options.query)
	options.planTemplate = strings.TrimSpace(options.planTemplate)
	options.douyinID = strings.TrimSpace(options.douyinID)
	options.creatorName = strings.TrimSpace(options.creatorName)
	if options.maxPages < 1 || options.maxPages > applicationqianchuan.MaxAllowedPages {
		return qianchuanMaterialOptions{}, errors.New("--max-pages must be between 1 and 100")
	}
	switch action {
	case "authorized-creators":
		if err := validateCLIPositiveID(options.advertiserID, "advertiser_id", true); err != nil {
			return qianchuanMaterialOptions{}, err
		}
		if options.planTemplate != "" || options.douyinID != "" || options.creatorName != "" {
			return qianchuanMaterialOptions{}, errors.New("creator video arguments are not supported by authorized-creators")
		}
		if options.pageSize == 0 {
			options.pageSize = applicationqianchuan.MaxCreatorPageSize
		}
		if options.pageSize < 1 || options.pageSize > applicationqianchuan.MaxCreatorPageSize {
			return qianchuanMaterialOptions{}, errors.New("--page-size must be between 1 and 100")
		}
	case "creator-videos":
		if options.advertiserID != "" || options.query != "" {
			return qianchuanMaterialOptions{}, errors.New("authorized creator list arguments are not supported by creator-videos")
		}
		if options.planTemplate == "" {
			return qianchuanMaterialOptions{}, errors.New("plan_template is required")
		}
		if options.douyinID == "" {
			return qianchuanMaterialOptions{}, errors.New("douyin_id is required")
		}
		if options.pageSize == 0 {
			options.pageSize = applicationqianchuan.MaxCreatorVideoCount
		}
		if options.pageSize < 1 || options.pageSize > applicationqianchuan.MaxCreatorVideoCount {
			return qianchuanMaterialOptions{}, errors.New("--page-size must be between 1 and 50")
		}
	default:
		return qianchuanMaterialOptions{}, errors.New("unsupported Qianchuan material action")
	}
	return options, nil
}

func parseQianchuanPlanOptions(action string, args []string) (qianchuanPlanOptions, error) {
	options := qianchuanPlanOptions{maxPages: applicationqianchuan.DefaultMaxPages}
	flags := flag.NewFlagSet("qc-plans "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.StringVar(&options.advertiserID, "advertiser-id", "", "")
	flags.StringVar(&options.authAccountID, "auth-account-id", "", "")
	flags.StringVar(&options.adID, "ad-id", "", "")
	flags.IntVar(&options.maxPages, "max-pages", applicationqianchuan.DefaultMaxPages, "")
	flags.IntVar(&options.top, "top", 0, "")
	flags.BoolVar(&options.full, "full", false, "")
	flags.StringVar(&options.out, "out", "", "")
	if err := flags.Parse(args); err != nil {
		return qianchuanPlanOptions{}, err
	}
	if len(flags.Args()) != 0 {
		return qianchuanPlanOptions{}, errors.New("unexpected positional Qianchuan plan arguments")
	}
	options.advertiserID = strings.TrimSpace(options.advertiserID)
	options.authAccountID = strings.TrimSpace(options.authAccountID)
	options.adID = strings.TrimSpace(options.adID)
	if err := validateCLIPositiveID(options.advertiserID, "advertiser_id", true); err != nil {
		return qianchuanPlanOptions{}, err
	}
	if action != "list" && action != "show" && action != "materials" {
		return qianchuanPlanOptions{}, errors.New("unsupported Qianchuan plan action")
	}
	if action == "show" || action == "materials" {
		if err := validateCLIPositiveID(options.adID, "ad_id", true); err != nil {
			return qianchuanPlanOptions{}, err
		}
	}
	if options.top < 0 {
		return qianchuanPlanOptions{}, errors.New("--top must be zero or a positive integer")
	}
	if options.maxPages < 1 || options.maxPages > applicationqianchuan.MaxAllowedPages {
		return qianchuanPlanOptions{}, errors.New("--max-pages must be between 1 and 100")
	}
	return options, nil
}

func RunQianchuanRead(
	ctx context.Context,
	domainName string,
	action string,
	args []string,
	service QianchuanReadService,
	stdout io.Writer,
) int {
	if service == nil {
		WriteDomainError(stdout, domain.NewError("unexpected_error", "Qianchuan read service is unavailable", 1, nil))
		return 1
	}
	var result any
	var destination string
	var err error
	exitCode := 0
	switch domainName {
	case "qc-materials":
		options, parseErr := parseQianchuanMaterialOptions(action, args)
		if parseErr != nil {
			WriteDomainError(stdout, domain.NewError("invalid_arguments", parseErr.Error(), 2, nil))
			return 2
		}
		destination = options.out
		switch action {
		case "authorized-creators":
			var creators applicationqianchuan.AuthorizedCreatorResult
			creators, err = service.ListAuthorizedCreators(ctx, applicationqianchuan.AuthorizedCreatorQuery{
				CredentialScope: applicationqianchuan.CredentialScope{
					AdvertiserID: options.advertiserID, AuthAccountID: options.authAccountID,
				},
				SearchKeyword: options.query, PageSize: options.pageSize, MaxPages: options.maxPages,
			})
			result = creators
			if creators.Truncated {
				exitCode = 1
			}
		case "creator-videos":
			result, err = service.QueryCreatorVideos(ctx, applicationqianchuan.CreatorVideoQuery{
				PlanTemplate: options.planTemplate, DouyinID: options.douyinID,
				CreatorName: options.creatorName, AuthAccountID: options.authAccountID,
				PageSize: options.pageSize, MaxPages: options.maxPages,
			})
		}
	case "qc-products":
		options, parseErr := parseQianchuanProductOptions(action, args)
		if parseErr != nil {
			WriteDomainError(stdout, domain.NewError("invalid_arguments", parseErr.Error(), 2, nil))
			return 2
		}
		destination = options.out
		result, err = service.QueryProducts(ctx, applicationqianchuan.ProductQuery{
			CredentialScope: applicationqianchuan.CredentialScope{
				AdvertiserID: options.advertiserID, AuthAccountID: options.authAccountID,
			},
			ProductIDs: []string(options.productIDs), ProductName: options.name,
			Tab: options.tab, AwemeID: options.awemeID, OnlyUnpromoted: options.onlyUnpromoted,
			OrderField: options.orderField, OrderType: options.orderType, Platform: options.platform,
			PageSize: options.pageSize, MaxPages: options.maxPages,
		}, "qianchuan_product_"+action)
	case "qc-plans":
		options, parseErr := parseQianchuanPlanOptions(action, args)
		if parseErr != nil {
			WriteDomainError(stdout, domain.NewError("invalid_arguments", parseErr.Error(), 2, nil))
			return 2
		}
		destination = options.out
		scope := applicationqianchuan.CredentialScope{
			AdvertiserID: options.advertiserID, AuthAccountID: options.authAccountID,
		}
		switch action {
		case "list":
			result, err = service.ListPlans(ctx, applicationqianchuan.PlanListQuery{
				CredentialScope: scope, PageSize: applicationqianchuan.DefaultPageSize,
				MaxPages: options.maxPages, Top: options.top, Full: options.full,
			})
		case "show":
			result, err = service.ShowPlan(ctx, scope, options.adID)
		case "materials":
			result, err = service.ListPlanMaterials(ctx, applicationqianchuan.PlanMaterialsQuery{
				CredentialScope: scope, AdID: options.adID,
				PageSize: applicationqianchuan.DefaultPageSize, MaxPages: options.maxPages,
			})
		}
	default:
		WriteDomainError(stdout, domain.NewError("invalid_arguments", "unsupported Qianchuan read domain", 2, nil))
		return 2
	}
	if err != nil {
		mapped := qianchuanReadError(err)
		WriteDomainError(stdout, mapped)
		return mapped.ExitCode
	}
	if err := WriteJSONDestination(stdout, result, destination); err != nil {
		WriteDomainError(stdout, domain.WrapError("configuration_error", "failed to write Qianchuan result", 2, err))
		return 2
	}
	return exitCode
}

func (runner Runner) runQianchuanRead(
	ctx context.Context,
	domainName string,
	action string,
	args []string,
	configPath string,
	stateRoot string,
	credentialsStore authapplication.CredentialStore,
	stdout io.Writer,
) int {
	runtime := runner.QianchuanReads
	service := runtime.Service
	if service == nil {
		factory := runtime.ClientFactory
		if factory == nil {
			var err error
			factory, err = oceanengine.NewClientFactory(oceanengine.FactoryOptions{
				SharedQianchuanControl: filesystem.QianchuanRequestController{Root: stateRoot},
			})
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
			reader = oceanengine.QianchuanReadAdapter{Factory: factory, Retry: runtime.Retry}
		}
		tokens := &authapplication.TokenManager{
			Credentials: credentialsStore, Authorizations: authorizations,
			Locks: refreshLocker, OAuth: oauth, Now: runtime.Now,
		}
		service = applicationqianchuan.Service{
			Tokens: tokens, Reader: reader, Now: runtime.Now,
			Templates: applicationtemplates.Query{
				Store: filesystem.ConfigStore{Path: filepath.Clean(configPath)}, Path: filepath.Clean(configPath),
			},
		}
	}
	return RunQianchuanRead(ctx, domainName, action, args, service, stdout)
}

func qianchuanReadError(err error) *domain.Error {
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
	return domain.NewError("qianchuan_query_failed", err.Error(), 1, nil)
}

func splitRepeatedCSV(values []string) repeatedValues {
	result := repeatedValues{}
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				result = append(result, item)
			}
		}
	}
	return result
}

func validateCLIPositiveID(value, field string, required bool) error {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return errors.New(field + " is required")
		}
		return nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return errors.New(field + " must be a positive decimal integer within int64 range")
	}
	return nil
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
