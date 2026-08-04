package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/oceanengine"
	authapplication "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/auth"
	applicationreports "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/reports"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	platformretry "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/retry"
	portreports "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/reports"
)

type QianchuanReportService interface {
	MaterialReport(context.Context, applicationreports.MaterialQuery) (applicationreports.MaterialResult, error)
	PlanReport(context.Context, applicationreports.PlanQuery) (applicationreports.PlanResult, error)
}

type QianchuanUnifiedReportService interface {
	QianchuanSchema(context.Context, applicationreports.QianchuanSchemaQuery) (applicationreports.QianchuanSchemaResult, error)
	QianchuanAllPromotion(context.Context, applicationreports.QianchuanAggregateQuery) (applicationreports.QianchuanAggregateResult, error)
	QianchuanUniPromotion(context.Context, applicationreports.QianchuanAggregateQuery) (applicationreports.QianchuanAggregateResult, error)
	QianchuanCustom(context.Context, applicationreports.QianchuanCustomQuery) (applicationreports.QianchuanCustomResult, error)
	QianchuanRoom(context.Context, applicationreports.QianchuanDimensionQuery) (applicationreports.QianchuanDimensionResult, error)
	QianchuanAuthor(context.Context, applicationreports.QianchuanDimensionQuery) (applicationreports.QianchuanDimensionResult, error)
}

type QianchuanReportRuntime struct {
	Service        QianchuanReportService
	Reader         portreports.QianchuanReader
	Authorizations authapplication.AuthorizationStore
	RefreshLocker  authapplication.RefreshLocker
	OAuth          authapplication.OAuthAdapter
	ClientFactory  *oceanengine.ClientFactory
	Retry          platformretry.Policy
	Now            func() time.Time
}

type qianchuanReportOptions struct {
	configPath    string
	advertiserID  string
	authAccountID string
	startDate     string
	endDate       string
	top           int
	status        string
	out           string
	fields        repeatedValues
	materialIDs   repeatedValues
	materialType  string
	materialModes repeatedValues
	videoSources  repeatedValues
	orderField    string
	orderType     string
	pageSize      int
	maxPages      int
	dataTopics    repeatedValues
	dimensions    repeatedValues
	metrics       repeatedValues
	filterValues  repeatedValues
	filters       []applicationreports.QianchuanFilter
	dataPeriod    string
	adlabScene    string
	marketingGoal string
	orderPlatform string
	reportMode    string
	dimensionID   string
	dimension     string
	smartBidType  string
}

func parseQianchuanReportOptions(
	action string,
	args []string,
	now func() time.Time,
) (qianchuanReportOptions, error) {
	today := reportToday(now)
	options := qianchuanReportOptions{
		startDate: today, endDate: today, top: 10, status: "ALL",
		orderField: "stat_cost", orderType: "DESC",
		pageSize: applicationreports.DefaultPageSize, maxPages: applicationreports.DefaultMaxPages,
		marketingGoal: "ALL", orderPlatform: "QIANCHUAN", reportMode: "uni",
		adlabScene: "OVERALL_PROJECT",
		dimension:  "TIME_GRANULARITY_DAILY",
	}
	flags := flag.NewFlagSet("qc-reports "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.StringVar(&options.advertiserID, "advertiser-id", "", "")
	flags.StringVar(&options.authAccountID, "auth-account-id", "", "")
	flags.StringVar(&options.startDate, "start-date", today, "")
	flags.StringVar(&options.endDate, "end-date", today, "")
	flags.IntVar(&options.top, "top", 10, "")
	flags.StringVar(&options.out, "out", "", "")
	flags.StringVar(&options.marketingGoal, "marketing-goal", "ALL", "")
	switch action {
	case "plans":
		flags.StringVar(&options.status, "status", "ALL", "")
	case "materials":
		flags.Var(&options.fields, "field", "")
		flags.Var(&options.materialIDs, "material-id", "")
		flags.StringVar(&options.materialType, "material-type", "", "")
		flags.Var(&options.materialModes, "material-mode", "")
		flags.Var(&options.videoSources, "video-source", "")
		flags.StringVar(&options.orderField, "order-field", "stat_cost", "")
		flags.StringVar(&options.orderType, "order-type", "DESC", "")
		flags.IntVar(&options.pageSize, "page-size", applicationreports.DefaultPageSize, "")
		flags.IntVar(&options.maxPages, "max-pages", applicationreports.DefaultMaxPages, "")
	case "account", "uni-account":
		flags.Var(&options.metrics, "field", "")
		flags.StringVar(&options.orderPlatform, "order-platform", "QIANCHUAN", "")
		if action == "account" {
			flags.StringVar(&options.adlabScene, "adlab-scene", "OVERALL_PROJECT", "")
			flags.StringVar(&options.dataPeriod, "data-period", "", "")
		}
	case "schema":
		flags.Var(&options.dataTopics, "data-topic", "")
		flags.StringVar(&options.dataPeriod, "data-period", "", "")
	case "custom", "products":
		flags.Var(&options.dataTopics, "data-topic", "")
		flags.Var(&options.dimensions, "dimension", "")
		flags.Var(&options.metrics, "metric", "")
		flags.Var(&options.filterValues, "filter", "")
		flags.StringVar(&options.dataPeriod, "data-period", "", "")
		flags.StringVar(&options.orderField, "order-field", "stat_cost", "")
		flags.StringVar(&options.orderType, "order-type", "DESC", "")
		flags.IntVar(&options.pageSize, "page-size", applicationreports.DefaultPageSize, "")
		flags.IntVar(&options.maxPages, "max-pages", applicationreports.DefaultMaxPages, "")
		if action == "products" {
			flags.StringVar(&options.reportMode, "report-mode", "uni", "")
		}
	case "rooms", "authors":
		name := "aweme-id"
		if action == "rooms" {
			name = "room-id"
		}
		flags.StringVar(&options.dimensionID, name, "", "")
		flags.Var(&options.metrics, "metric", "")
		flags.StringVar(&options.orderPlatform, "order-platform", "QIANCHUAN", "")
		flags.StringVar(&options.smartBidType, "smart-bid-type", "", "")
		flags.StringVar(&options.dimension, "dimension", "TIME_GRANULARITY_DAILY", "")
		defaultOrder := "stat_cost"
		if action == "rooms" {
			defaultOrder = "stat_cost_for_roi2"
		}
		flags.StringVar(&options.orderField, "order-field", defaultOrder, "")
		flags.StringVar(&options.orderType, "order-type", "DESC", "")
		flags.IntVar(&options.pageSize, "page-size", applicationreports.DefaultPageSize, "")
		flags.IntVar(&options.maxPages, "max-pages", applicationreports.DefaultMaxPages, "")
	default:
		return qianchuanReportOptions{}, errors.New("unsupported Qianchuan report action")
	}
	if err := flags.Parse(args); err != nil {
		return qianchuanReportOptions{}, err
	}
	if len(flags.Args()) != 0 {
		return qianchuanReportOptions{}, errors.New("unexpected positional Qianchuan report arguments")
	}
	options.advertiserID = strings.TrimSpace(options.advertiserID)
	options.authAccountID = strings.TrimSpace(options.authAccountID)
	options.startDate = strings.TrimSpace(options.startDate)
	options.endDate = strings.TrimSpace(options.endDate)
	options.status = strings.TrimSpace(options.status)
	options.materialType = strings.TrimSpace(options.materialType)
	options.orderField = strings.TrimSpace(options.orderField)
	options.orderType = strings.TrimSpace(options.orderType)
	options.fields = splitRepeatedCSV(options.fields)
	options.materialIDs = splitRepeatedCSV(options.materialIDs)
	options.materialModes = splitRepeatedCSV(options.materialModes)
	options.videoSources = splitRepeatedCSV(options.videoSources)
	options.dataTopics = splitRepeatedCSV(options.dataTopics)
	options.dimensions = splitRepeatedCSV(options.dimensions)
	options.metrics = splitRepeatedCSV(options.metrics)
	options.marketingGoal = strings.TrimSpace(options.marketingGoal)
	options.orderPlatform = strings.TrimSpace(options.orderPlatform)
	options.reportMode = strings.TrimSpace(options.reportMode)
	options.dimensionID = strings.TrimSpace(options.dimensionID)
	options.dimension = strings.TrimSpace(options.dimension)
	options.smartBidType = strings.TrimSpace(options.smartBidType)
	options.dataPeriod = strings.TrimSpace(options.dataPeriod)
	options.adlabScene = strings.TrimSpace(options.adlabScene)
	if err := validateCLIPositiveID(options.advertiserID, "advertiser_id", true); err != nil {
		return qianchuanReportOptions{}, err
	}
	if err := validateAccountReportDates(options.startDate, options.endDate); err != nil {
		return qianchuanReportOptions{}, err
	}
	if options.top < 0 {
		return qianchuanReportOptions{}, errors.New("--top must be zero or a positive integer")
	}
	if action == "plans" {
		if options.status == "" {
			options.status = "ALL"
		}
		return options, nil
	}
	if action != "materials" {
		return validateQianchuanUnifiedOptions(action, options)
	}
	for index, value := range options.materialIDs {
		if err := validateCLIPositiveID(value, "material_id["+strconv.Itoa(index)+"]", true); err != nil {
			return qianchuanReportOptions{}, err
		}
	}
	if options.materialType != "" && !containsString([]string{"video", "image", "carousel"}, options.materialType) {
		return qianchuanReportOptions{}, errors.New("--material-type must be video, image, or carousel")
	}
	if options.orderType != "ASC" && options.orderType != "DESC" {
		return qianchuanReportOptions{}, errors.New("--order-type must be ASC or DESC")
	}
	if options.orderField == "" {
		return qianchuanReportOptions{}, errors.New("--order-field must not be empty")
	}
	if options.pageSize < 1 || options.pageSize > applicationreports.MaxMaterialPageSize {
		return qianchuanReportOptions{}, errors.New("--page-size must be between 1 and 100")
	}
	if options.maxPages < 1 || options.maxPages > applicationreports.MaxAllowedPages {
		return qianchuanReportOptions{}, errors.New("--max-pages must be between 1 and 500")
	}
	return options, nil
}

func validateQianchuanUnifiedOptions(action string, options qianchuanReportOptions) (qianchuanReportOptions, error) {
	if action == "account" {
		if options.adlabScene != "OVERALL_PROJECT" && options.adlabScene != "UNI_PROJECT" {
			return options, errors.New("--adlab-scene must be OVERALL_PROJECT or UNI_PROJECT")
		}
		if options.adlabScene != "OVERALL_PROJECT" && options.dataPeriod != "" {
			return options, errors.New("--data-period is supported only for OVERALL_PROJECT")
		}
	}
	if action == "schema" && len(options.dataTopics) == 0 {
		return options, errors.New("--data-topic is required")
	}
	if action == "custom" && (len(options.dataTopics) != 1 || len(options.dimensions) == 0 || len(options.metrics) == 0) {
		return options, errors.New("custom requires one --data-topic and at least one --dimension and --metric")
	}
	if action == "products" && options.reportMode != "uni" && options.reportMode != "overall" {
		return options, errors.New("--report-mode must be uni or overall")
	}
	if action == "rooms" || action == "authors" {
		if err := validateCLIPositiveID(options.dimensionID, "dimension_id", true); err != nil {
			return options, err
		}
		if options.dimension != "TIME_GRANULARITY_DAILY" && options.dimension != "TIME_GRANULARITY_HOURLY" {
			return options, errors.New("--dimension must be TIME_GRANULARITY_DAILY or TIME_GRANULARITY_HOURLY")
		}
		if options.orderPlatform != "ALL" && options.orderPlatform != "QIANCHUAN" && options.orderPlatform != "ECP_AWEME" {
			return options, errors.New("--order-platform must be ALL, QIANCHUAN, or ECP_AWEME")
		}
		if options.smartBidType != "" && options.smartBidType != "SMART_BID_CUSTOM" && options.smartBidType != "SMART_BID_CONSERVATIVE" {
			return options, errors.New("--smart-bid-type must be SMART_BID_CUSTOM or SMART_BID_CONSERVATIVE")
		}
	}
	if action == "custom" || action == "products" {
		if !applicationreports.QianchuanUnifiedPageSizes[options.pageSize] {
			return options, errors.New("--page-size must be one of 10, 20, 50, 100, or 200")
		}
	} else if (action == "rooms" || action == "authors") && (options.pageSize < 1 || options.pageSize > 100) {
		return options, errors.New("--page-size must be between 1 and 100")
	}
	if action == "custom" || action == "products" || action == "rooms" || action == "authors" {
		if options.maxPages < 1 || options.maxPages > applicationreports.QianchuanUnifiedMaxPages {
			return options, errors.New("--max-pages must be between 1 and 500")
		}
		if options.orderField == "" || (options.orderType != "ASC" && options.orderType != "DESC") {
			return options, errors.New("--order-field and --order-type ASC or DESC are required")
		}
	}
	if options.dataPeriod != "" && options.dataPeriod != "ALL_DATA" && options.dataPeriod != "OVER_ALL_DATA" && options.dataPeriod != "UNI_DATA" {
		return options, errors.New("--data-period must be ALL_DATA, OVER_ALL_DATA, or UNI_DATA")
	}
	var err error
	options.filters, err = parseQianchuanFilters(options.filterValues)
	return options, err
}

func parseQianchuanFilters(values []string) ([]applicationreports.QianchuanFilter, error) {
	result := make([]applicationreports.QianchuanFilter, 0, len(values))
	for _, value := range values {
		filter := applicationreports.QianchuanFilter{Operator: 7}
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "{") {
			if err := json.Unmarshal([]byte(value), &filter); err != nil {
				return nil, fmt.Errorf("invalid --filter JSON: %w", err)
			}
		} else {
			parts := strings.SplitN(value, "=", 2)
			if len(parts) != 2 {
				return nil, errors.New("--filter must be JSON or field=value1,value2")
			}
			filter.Field, filter.Values = strings.TrimSpace(parts[0]), []string(splitRepeatedCSV(repeatedValues{parts[1]}))
		}
		if filter.Field == "" || filter.Operator != 7 || len(filter.Values) == 0 {
			return nil, errors.New("--filter requires a field, operator 7, and values")
		}
		result = append(result, filter)
	}
	return result, nil
}

func RunQianchuanReport(
	ctx context.Context,
	action string,
	args []string,
	service QianchuanReportService,
	stdout io.Writer,
	now func() time.Time,
) int {
	options, err := parseQianchuanReportOptions(action, args, now)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("invalid_arguments", err.Error(), 2, nil))
		return 2
	}
	if service == nil {
		WriteDomainError(stdout, domain.NewError("unexpected_error", "Qianchuan report service is unavailable", 1, nil))
		return 1
	}
	scope := applicationreports.CredentialScope{
		AdvertiserID: options.advertiserID, AuthAccountID: options.authAccountID,
	}
	var result any
	switch action {
	case "plans":
		result, err = service.PlanReport(ctx, applicationreports.PlanQuery{
			CredentialScope: scope, StartDate: options.startDate, EndDate: options.endDate,
			Top: options.top, Status: options.status,
		})
	case "materials":
		result, err = service.MaterialReport(ctx, applicationreports.MaterialQuery{
			CredentialScope: scope, StartDate: options.startDate, EndDate: options.endDate,
			Fields: []string(options.fields),
			Filters: applicationreports.MaterialFilters{
				MaterialIDs: []string(options.materialIDs), MaterialType: options.materialType,
				MaterialMode: []string(options.materialModes), VideoSource: []string(options.videoSources),
			},
			OrderField: options.orderField, OrderType: options.orderType,
			PageSize: options.pageSize, MaxPages: options.maxPages, Top: options.top,
		})
	default:
		unified, ok := service.(QianchuanUnifiedReportService)
		if !ok {
			err = errors.New("Qianchuan unified report service is unavailable")
			break
		}
		result, err = runQianchuanUnifiedReport(ctx, action, options, scope, unified)
	}
	if err != nil {
		mapped := qianchuanReadError(err)
		WriteDomainError(stdout, mapped)
		return mapped.ExitCode
	}
	if err := WriteJSONDestination(stdout, result, options.out); err != nil {
		WriteDomainError(stdout, domain.WrapError("configuration_error", "failed to write Qianchuan report", 2, err))
		return 2
	}
	return 0
}

func runQianchuanUnifiedReport(
	ctx context.Context,
	action string,
	options qianchuanReportOptions,
	scope applicationreports.CredentialScope,
	service QianchuanUnifiedReportService,
) (any, error) {
	switch action {
	case "account", "uni-account":
		fields := []string(options.metrics)
		if len(fields) == 0 {
			fields = applicationreports.DefaultQianchuanAllPromotionFields
			if action == "uni-account" {
				fields = applicationreports.DefaultQianchuanUniPromotionFields
			}
		}
		query := applicationreports.QianchuanAggregateQuery{
			CredentialScope: scope, StartDate: options.startDate, EndDate: options.endDate,
			Fields: fields, MarketingGoal: options.marketingGoal, OrderPlatform: options.orderPlatform,
			AdlabScene: options.adlabScene, DataPeriod: options.dataPeriod,
		}
		if action == "account" {
			return service.QianchuanAllPromotion(ctx, query)
		}
		return service.QianchuanUniPromotion(ctx, query)
	case "schema":
		return service.QianchuanSchema(ctx, applicationreports.QianchuanSchemaQuery{
			CredentialScope: scope, Topics: []string(options.dataTopics), DataPeriod: options.dataPeriod,
		})
	case "custom", "products":
		topic := ""
		dimensions := []string(options.dimensions)
		metrics := []string(options.metrics)
		if action == "custom" {
			topic = options.dataTopics[0]
		} else {
			topic = applicationreports.QianchuanProductTopic
			if options.reportMode == "overall" {
				topic = applicationreports.QianchuanOverallProductTopic
			}
			if len(dimensions) == 0 {
				dimensions = []string{"product_id"}
			}
			if len(metrics) == 0 {
				metrics = applicationreports.DefaultQianchuanProductMetrics
			}
		}
		result, err := service.QianchuanCustom(ctx, applicationreports.QianchuanCustomQuery{
			CredentialScope: scope, StartDate: options.startDate, EndDate: options.endDate,
			DataTopic: topic, Dimensions: dimensions, Metrics: metrics, Filters: options.filters,
			DataPeriod: options.dataPeriod, OrderField: options.orderField, OrderType: options.orderType,
			PageSize: options.pageSize, MaxPages: options.maxPages, Top: options.top,
		})
		if err == nil && action == "products" {
			result.Mode = "qianchuan_product_dimension_report"
		}
		return result, err
	case "rooms", "authors":
		metrics := []string(options.metrics)
		if len(metrics) == 0 {
			metrics = applicationreports.DefaultQianchuanDimensionMetrics
		}
		query := applicationreports.QianchuanDimensionQuery{
			CredentialScope: scope, DimensionID: options.dimensionID,
			StartDate: options.startDate, EndDate: options.endDate,
			Dimension: options.dimension, Metrics: metrics, MarketingGoal: options.marketingGoal,
			OrderPlatform: options.orderPlatform, SmartBidType: options.smartBidType,
			OrderField: options.orderField, OrderType: options.orderType,
			PageSize: options.pageSize, MaxPages: options.maxPages, Top: options.top,
		}
		if action == "rooms" {
			return service.QianchuanRoom(ctx, query)
		}
		return service.QianchuanAuthor(ctx, query)
	default:
		return nil, errors.New("unsupported Qianchuan unified report action")
	}
}

func (runner Runner) runQianchuanReport(
	ctx context.Context,
	action string,
	args []string,
	stateRoot string,
	credentialsStore authapplication.CredentialStore,
	stdout io.Writer,
) int {
	runtime := runner.QianchuanReports
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
			reader = oceanengine.QianchuanReportAdapter{Factory: factory, Retry: runtime.Retry}
		}
		tokens := &authapplication.TokenManager{
			Credentials: credentialsStore, Authorizations: authorizations,
			Locks: refreshLocker, OAuth: oauth, Now: runtime.Now,
		}
		unified, _ := reader.(portreports.QianchuanUnifiedReader)
		service = applicationreports.Service{
			Tokens: tokens, Reader: reader, UnifiedReader: unified, Now: runtime.Now,
		}
	}
	return RunQianchuanReport(ctx, action, args, service, stdout, runtime.Now)
}

func reportToday(now func() time.Time) string {
	current := time.Now()
	if now != nil {
		current = now()
	}
	return current.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
}
