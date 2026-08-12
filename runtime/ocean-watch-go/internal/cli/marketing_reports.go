package cli

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/oceanengine"
	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	applicationreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/reports"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/configuration"
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
	portreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/reports"
)

type MarketingReportService interface {
	Schema(context.Context, applicationreports.MarketingSchemaQuery) (applicationreports.MarketingSchemaResult, error)
	Custom(context.Context, applicationreports.MarketingCustomQuery) (applicationreports.MarketingCustomResult, error)
	Plans(context.Context, applicationreports.MarketingPlanQuery) (applicationreports.MarketingPlanResult, error)
	Materials(context.Context, applicationreports.MarketingMaterialQuery) (applicationreports.MarketingMaterialResult, error)
}

type MarketingReportConfigReader interface {
	Read(context.Context) (map[string]any, error)
}

type MarketingReportRuntime struct {
	Service        MarketingReportService
	Reader         portreports.MarketingReader
	Authorizations authapplication.AuthorizationStore
	RefreshLocker  authapplication.RefreshLocker
	OAuth          authapplication.OAuthAdapter
	ClientFactory  *oceanengine.ClientFactory
	Retry          platformretry.Policy
	Now            func() time.Time
	ConfigFactory  func(string) MarketingReportConfigReader
}

type marketingReportOptions struct {
	configPath                  string
	channel                     string
	authAccountID               string
	advertiserID                string
	out                         string
	csvOut                      string
	dataTopic                   string
	dataTopics                  repeatedValues
	dimensions                  repeatedValues
	metrics                     repeatedValues
	filters                     []applicationreports.MarketingFilter
	filterValues                repeatedValues
	startTime                   string
	endTime                     string
	startDate                   string
	endDate                     string
	orderField                  string
	orderType                   string
	page                        int
	pageSize                    int
	maxPages                    int
	top                         int
	full                        bool
	includeExtraReportMaterials bool
	filterMaterialIDs           bool
	projectID                   string
	promotionIDs                repeatedValues
	activeOnly                  bool
	includeNonActive            bool
	promotionPage               int
	promotionPageSize           int
	reportPage                  int
	reportPageSize              int
	singlePage                  bool
}

func parseMarketingReportOptions(
	action string,
	args []string,
	now func() time.Time,
) (marketingReportOptions, error) {
	today := reportToday(now)
	options := marketingReportOptions{
		channel: "marketing", dataTopic: applicationreports.MarketingMaterialTopic,
		startDate: today, endDate: today, orderField: "stat_cost", orderType: "DESC",
		page: 1, pageSize: applicationreports.MarketingPageSize,
		maxPages: 100, top: 10, promotionPage: 1, promotionPageSize: 20,
		reportPage: 1, reportPageSize: applicationreports.MarketingPageSize,
	}
	flags := flag.NewFlagSet("reports "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.StringVar(&options.out, "out", "", "")
	switch action {
	case "schema":
		flags.Var(&options.dataTopics, "data-topic", "")
		flags.BoolVar(&options.full, "full", false, "")
		addMarketingReportAuthorizationFlags(flags, &options, false)
	case "custom":
		flags.StringVar(&options.csvOut, "csv-out", "", "")
		flags.StringVar(&options.dataTopic, "data-topic", applicationreports.MarketingMaterialTopic, "")
		flags.Var(&options.dimensions, "dimension", "")
		flags.Var(&options.metrics, "metric", "")
		flags.Var(&options.filterValues, "filter", "")
		flags.StringVar(&options.startTime, "start-time", "", "")
		flags.StringVar(&options.endTime, "end-time", "", "")
		flags.StringVar(&options.startDate, "start-date", "", "")
		flags.StringVar(&options.endDate, "end-date", "", "")
		flags.StringVar(&options.orderField, "order-field", "stat_cost", "")
		flags.StringVar(&options.orderType, "order-type", "DESC", "")
		flags.IntVar(&options.page, "page", 1, "")
		flags.IntVar(&options.pageSize, "page-size", applicationreports.MarketingPageSize, "")
		addMarketingReportAuthorizationFlags(flags, &options, false)
	case "plans":
		flags.StringVar(&options.advertiserID, "advertiser-id", "", "")
		flags.StringVar(&options.authAccountID, "auth-account-id", "", "")
		flags.StringVar(&options.startDate, "start-date", today, "")
		flags.StringVar(&options.endDate, "end-date", today, "")
		flags.Var(&options.metrics, "metric", "")
		flags.IntVar(&options.pageSize, "page-size", applicationreports.MarketingPageSize, "")
		flags.IntVar(&options.maxPages, "max-pages", 100, "")
		flags.IntVar(&options.top, "top", 10, "")
	case "materials":
		flags.StringVar(&options.advertiserID, "advertiser-id", "", "")
		flags.StringVar(&options.csvOut, "csv-out", "", "")
		flags.StringVar(&options.startDate, "start-date", today, "")
		flags.StringVar(&options.endDate, "end-date", today, "")
		flags.StringVar(&options.dataTopic, "data-topic", applicationreports.MarketingMaterialTopic, "")
		flags.Var(&options.dimensions, "dimension", "")
		flags.Var(&options.metrics, "metric", "")
		flags.BoolVar(&options.filterMaterialIDs, "filter-material-ids", false, "")
		flags.BoolVar(&options.includeExtraReportMaterials, "include-extra-report-materials", false, "")
		flags.StringVar(&options.projectID, "project-id", "", "")
		flags.Var(&options.promotionIDs, "promotion-id", "")
		flags.BoolVar(&options.activeOnly, "active-only", false, "")
		flags.BoolVar(&options.includeNonActive, "include-non-active", false, "")
		flags.IntVar(&options.promotionPage, "promotion-page", 1, "")
		flags.IntVar(&options.promotionPageSize, "promotion-page-size", 20, "")
		flags.IntVar(&options.reportPage, "report-page", 1, "")
		flags.IntVar(&options.reportPageSize, "report-page-size", applicationreports.MarketingPageSize, "")
		flags.BoolVar(&options.singlePage, "single-page", false, "")
		flags.StringVar(&options.orderField, "order-field", "stat_cost", "")
		flags.StringVar(&options.orderType, "order-type", "DESC", "")
		addMarketingReportAuthorizationFlags(flags, &options, false)
	default:
		return marketingReportOptions{}, errors.New("unsupported Marketing report action")
	}
	if err := flags.Parse(args); err != nil {
		return marketingReportOptions{}, err
	}
	if len(flags.Args()) != 0 {
		return marketingReportOptions{}, errors.New("unexpected positional Marketing report arguments")
	}
	options.configPath = strings.TrimSpace(options.configPath)
	options.channel = strings.ToLower(strings.TrimSpace(options.channel))
	options.authAccountID = strings.TrimSpace(options.authAccountID)
	options.advertiserID = strings.TrimSpace(options.advertiserID)
	options.out = strings.TrimSpace(options.out)
	options.csvOut = strings.TrimSpace(options.csvOut)
	options.dataTopic = strings.TrimSpace(options.dataTopic)
	options.dataTopics = splitRepeatedCSV(options.dataTopics)
	options.dimensions = splitRepeatedCSV(options.dimensions)
	options.metrics = splitRepeatedCSV(options.metrics)
	options.promotionIDs = splitRepeatedCSV(options.promotionIDs)
	options.startTime = strings.TrimSpace(options.startTime)
	options.endTime = strings.TrimSpace(options.endTime)
	options.startDate = strings.TrimSpace(options.startDate)
	options.endDate = strings.TrimSpace(options.endDate)
	options.orderField = strings.TrimSpace(options.orderField)
	options.orderType = strings.ToUpper(strings.TrimSpace(options.orderType))
	options.projectID = strings.TrimSpace(options.projectID)
	if action == "custom" {
		var err error
		options.filters, err = parseMarketingFilters(options.filterValues)
		if err != nil {
			return marketingReportOptions{}, err
		}
		if options.startTime == "" {
			options.startTime = options.startDate
		}
		if options.endTime == "" {
			options.endTime = options.endDate
		}
	}
	if err := validateMarketingReportOptions(action, options); err != nil {
		return marketingReportOptions{}, err
	}
	return options, nil
}

func addMarketingReportAuthorizationFlags(
	flags *flag.FlagSet,
	options *marketingReportOptions,
	advertiser bool,
) {
	if advertiser {
		flags.StringVar(&options.advertiserID, "advertiser-id", "", "")
	}
	flags.StringVar(&options.channel, "channel", "marketing", "")
	flags.StringVar(&options.authAccountID, "auth-account-id", "", "")
}

func validateMarketingReportOptions(action string, options marketingReportOptions) error {
	if action != "plans" && options.channel != "marketing" {
		return errors.New("reports commands only support --channel marketing")
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
	case "schema":
		if len(options.dataTopics) > 10 {
			return errors.New("--data-topic accepts at most 10 values")
		}
		for _, topic := range options.dataTopics {
			if !applicationreports.ValidMarketingDataTopic(topic) {
				return fmt.Errorf("unsupported Marketing report data topic %q", topic)
			}
		}
	case "custom":
		if !applicationreports.ValidMarketingDataTopic(options.dataTopic) {
			return fmt.Errorf("unsupported Marketing report data topic %q", options.dataTopic)
		}
		if err := validateMarketingReportTimes(options.startTime, options.endTime); err != nil {
			return err
		}
		if err := validateMarketingReportOrder(options.orderField, options.orderType); err != nil {
			return err
		}
		if options.page < 1 {
			return errors.New("--page must be positive")
		}
		if err := validateMarketingReportPageSize(options.pageSize, "--page-size"); err != nil {
			return err
		}
	case "plans":
		if err := validateAccountReportDates(options.startDate, options.endDate); err != nil {
			return err
		}
		if err := validateMarketingReportPageSize(options.pageSize, "--page-size"); err != nil {
			return err
		}
		if options.maxPages < 1 || options.maxPages > applicationreports.MarketingMaxPages {
			return errors.New("--max-pages must be between 1 and 500")
		}
		if options.top < 0 {
			return errors.New("--top must be zero or a positive integer")
		}
	case "materials":
		if err := validateAccountReportDates(options.startDate, options.endDate); err != nil {
			return err
		}
		if !applicationreports.ValidMarketingDataTopic(options.dataTopic) {
			return fmt.Errorf("unsupported Marketing report data topic %q", options.dataTopic)
		}
		if options.projectID != "" {
			if err := domain.ValidateDecimalID(options.projectID, "project_id"); err != nil {
				return err
			}
		}
		for index, value := range options.promotionIDs {
			if err := domain.ValidateDecimalID(value, "promotion_id["+strconv.Itoa(index)+"]"); err != nil {
				return err
			}
		}
		if options.promotionPage < 1 || options.reportPage < 1 {
			return errors.New("--promotion-page and --report-page must be positive")
		}
		if err := validateMarketingReportPageSize(options.promotionPageSize, "--promotion-page-size"); err != nil {
			return err
		}
		if err := validateMarketingReportPageSize(options.reportPageSize, "--report-page-size"); err != nil {
			return err
		}
		if err := validateMarketingReportOrder(options.orderField, options.orderType); err != nil {
			return err
		}
	}
	return nil
}

func validateMarketingReportPageSize(value int, name string) error {
	if value < 1 || value > applicationreports.MarketingPageSize {
		return fmt.Errorf("%s must be between 1 and 100", name)
	}
	return nil
}

func validateMarketingReportOrder(field, orderType string) error {
	if field == "" {
		return errors.New("--order-field must not be empty")
	}
	if orderType != "ASC" && orderType != "DESC" {
		return errors.New("--order-type must be ASC or DESC")
	}
	return nil
}

func validateMarketingReportTimes(start, end string) error {
	parse := func(value string) (time.Time, error) {
		if value == "" {
			return time.Time{}, nil
		}
		layout := "2006-01-02 15:04:05"
		if len(value) == 10 {
			layout = "2006-01-02"
		}
		return time.Parse(layout, value)
	}
	startTime, err := parse(start)
	if err != nil {
		return errors.New("--start-time and --start-date must use YYYY-MM-DD or YYYY-MM-DD HH:MM:SS")
	}
	endTime, err := parse(end)
	if err != nil {
		return errors.New("--end-time and --end-date must use YYYY-MM-DD or YYYY-MM-DD HH:MM:SS")
	}
	if !startTime.IsZero() && !endTime.IsZero() && startTime.After(endTime) {
		return errors.New("report start time must not be after end time")
	}
	return nil
}

func parseMarketingFilters(values []string) ([]applicationreports.MarketingFilter, error) {
	result := make([]applicationreports.MarketingFilter, 0, len(values))
	for _, value := range values {
		filter, err := parseMarketingFilter(value)
		if err != nil {
			return nil, err
		}
		result = append(result, filter)
	}
	return result, nil
}

func parseMarketingFilter(value string) (applicationreports.MarketingFilter, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "{") {
		decoder := json.NewDecoder(strings.NewReader(value))
		decoder.UseNumber()
		var raw struct {
			Field    string          `json:"field"`
			Type     json.Number     `json:"type"`
			Operator json.Number     `json:"operator"`
			Values   json.RawMessage `json:"values"`
		}
		if err := decoder.Decode(&raw); err != nil {
			return applicationreports.MarketingFilter{}, fmt.Errorf("invalid --filter JSON: %w", err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return applicationreports.MarketingFilter{}, errors.New("invalid --filter JSON")
		}
		filterType, err := raw.Type.Int64()
		if err != nil {
			return applicationreports.MarketingFilter{}, errors.New("--filter type must be an integer")
		}
		operator, err := raw.Operator.Int64()
		if err != nil {
			return applicationreports.MarketingFilter{}, errors.New("--filter operator must be an integer")
		}
		filterValues, err := marketingFilterJSONValues(raw.Values)
		if err != nil {
			return applicationreports.MarketingFilter{}, err
		}
		return normalizedMarketingFilter(raw.Field, filterType, operator, filterValues)
	}
	parts := strings.SplitN(value, ":", 4)
	if len(parts) != 4 {
		return applicationreports.MarketingFilter{}, errors.New("--filter must be JSON or field:type:operator:value1,value2")
	}
	filterType, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil {
		return applicationreports.MarketingFilter{}, errors.New("--filter type must be an integer")
	}
	operator, err := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
	if err != nil {
		return applicationreports.MarketingFilter{}, errors.New("--filter operator must be an integer")
	}
	return normalizedMarketingFilter(parts[0], filterType, operator, strings.Split(parts[3], ","))
}

func marketingFilterJSONValues(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, errors.New("--filter values must be a non-empty array")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var values []any
	if err := decoder.Decode(&values); err != nil {
		return nil, errors.New("--filter values must be an array")
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			result = append(result, typed)
		case json.Number:
			result = append(result, typed.String())
		default:
			return nil, errors.New("--filter values must contain only strings or numbers")
		}
	}
	return result, nil
}

func normalizedMarketingFilter(
	field string,
	filterType int64,
	operator int64,
	values []string,
) (applicationreports.MarketingFilter, error) {
	field = strings.TrimSpace(field)
	values = []string(splitRepeatedCSV(repeatedValues(values)))
	if field == "" || len(values) == 0 {
		return applicationreports.MarketingFilter{}, errors.New("--filter requires a field and at least one value")
	}
	return applicationreports.MarketingFilter{
		Field: field, Type: filterType, Operator: operator, Values: values,
	}, nil
}

func (runner Runner) runMarketingReport(
	ctx context.Context,
	action string,
	args []string,
	cwd string,
	userHome string,
	stateRoot string,
	getenv func(string) string,
	credentialsStore authapplication.CredentialStore,
	stdout io.Writer,
) int {
	runtime := runner.MarketingReports
	options, err := parseMarketingReportOptions(action, args, runtime.Now)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("invalid_arguments", err.Error(), 2, nil))
		return 2
	}
	configPath := filepath.Clean(filesystem.ResolveConfigPath(options.configPath, cwd, getenv, userHome))
	configReader := MarketingReportConfigReader(filesystem.ConfigStore{Path: configPath})
	if runtime.ConfigFactory != nil {
		configReader = runtime.ConfigFactory(configPath)
	}
	if configReader == nil {
		WriteDomainError(stdout, domain.NewError("configuration_error", "Marketing report config reader is unavailable", 2, nil))
		return 2
	}
	rawConfig, err := configReader.Read(ctx)
	if err != nil {
		WriteDomainError(stdout, domain.WrapError("configuration_error", "failed to read Marketing report config", 2, err))
		return 2
	}
	options, err = resolveMarketingReportConfig(options, rawConfig)
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
			reader = oceanengine.MarketingReportsAdapter{Factory: factory, Retry: runtime.Retry}
		}
		tokens := &authapplication.TokenManager{
			Credentials: credentialsStore, Authorizations: authorizations,
			Locks: refreshLocker, OAuth: oauth, Now: runtime.Now,
		}
		service = applicationreports.MarketingService{Tokens: tokens, Reader: reader, Now: runtime.Now}
	}
	return runMarketingReportQuery(ctx, action, options, service, stdout)
}

func resolveMarketingReportConfig(
	options marketingReportOptions,
	raw map[string]any,
) (marketingReportOptions, error) {
	config, selected, err := configuration.Runtime(raw, "marketing", "query")
	if err != nil {
		return marketingReportOptions{}, err
	}
	if selected.ID != "marketing" {
		return marketingReportOptions{}, errors.New("Marketing report config resolved another channel")
	}
	if options.advertiserID == "" {
		options.advertiserID = marketingConfigString(config, "account.advertiser_id")
	}
	if err := domain.ValidateDecimalID(options.advertiserID, "account.advertiser_id"); err != nil {
		return marketingReportOptions{}, err
	}
	return options, nil
}

func runMarketingReportQuery(
	ctx context.Context,
	action string,
	options marketingReportOptions,
	service MarketingReportService,
	stdout io.Writer,
) int {
	if service == nil {
		WriteDomainError(stdout, domain.NewError("unexpected_error", "Marketing report service is unavailable", 1, nil))
		return 1
	}
	scope := applicationreports.CredentialScope{
		AdvertiserID: options.advertiserID, AuthAccountID: options.authAccountID,
	}
	var result any
	var csvRows []map[string]any
	var csvFields []string
	var err error
	switch action {
	case "schema":
		result, err = service.Schema(ctx, applicationreports.MarketingSchemaQuery{
			CredentialScope: scope, DataTopics: []string(options.dataTopics), Full: options.full,
		})
	case "custom":
		var response applicationreports.MarketingCustomResult
		response, err = service.Custom(ctx, applicationreports.MarketingCustomQuery{
			CredentialScope: scope, DataTopic: options.dataTopic,
			Dimensions: []string(options.dimensions), Metrics: []string(options.metrics),
			Filters: options.filters, StartTime: options.startTime, EndTime: options.endTime,
			OrderField: options.orderField, OrderType: options.orderType,
			Page: options.page, PageSize: options.pageSize,
		})
		result, csvRows = response, response.FlatRows
		csvFields = marketingReportCSVFields(
			options.dimensions, applicationreports.DefaultMarketingDimensions,
			options.metrics, applicationreports.DefaultMarketingMetrics,
		)
	case "plans":
		result, err = service.Plans(ctx, applicationreports.MarketingPlanQuery{
			CredentialScope: scope, StartDate: options.startDate, EndDate: options.endDate,
			Metrics: []string(options.metrics), PageSize: options.pageSize,
			MaxPages: options.maxPages, Top: options.top,
		})
	case "materials":
		var response applicationreports.MarketingMaterialResult
		response, err = service.Materials(ctx, applicationreports.MarketingMaterialQuery{
			CredentialScope: scope, StartDate: options.startDate, EndDate: options.endDate,
			DataTopic: options.dataTopic, Dimensions: []string(options.dimensions), Metrics: []string(options.metrics),
			IncludeExtraReportMaterials: options.includeExtraReportMaterials,
			ProjectID:                   options.projectID, PromotionIDs: []string(options.promotionIDs),
			ActiveOnly: options.activeOnly, PromotionPage: options.promotionPage,
			PromotionPageSize: options.promotionPageSize, ReportPage: options.reportPage,
			ReportPageSize: options.reportPageSize, SinglePage: options.singlePage,
			OrderField: options.orderField, OrderType: options.orderType,
		})
		result, csvRows = response, response.Rows
		csvFields = append([]string{
			"project_id", "promotion_id", "promotion_name", "promotion_status",
			"promotion_status_first", "promotion_status_second", "promotion_opt_status",
			"material_id", "video_id", "video_cover_id", "material_status",
			"material_opt_status", "image_mode", "material_create_time", "has_report_data",
		}, marketingReportCSVFields(
			options.dimensions, applicationreports.DefaultMarketingDimensions,
			options.metrics, applicationreports.DefaultMarketingMetrics,
		)...)
	}
	if err != nil {
		mapped := marketingReportError(err)
		WriteDomainError(stdout, mapped)
		return mapped.ExitCode
	}
	if options.csvOut != "" {
		if err := writeMarketingReportCSV(options.csvOut, csvRows, csvFields); err != nil {
			WriteDomainError(stdout, domain.WrapError("configuration_error", "failed to write Marketing report CSV", 2, err))
			return 2
		}
	}
	if err := WriteJSONDestination(stdout, result, options.out); err != nil {
		WriteDomainError(stdout, domain.WrapError("configuration_error", "failed to write Marketing report", 2, err))
		return 2
	}
	return 0
}

func writeMarketingReportCSV(path string, rows []map[string]any, preferred []string) error {
	if len(rows) == 0 {
		preferred = nil
	}
	fields := uniqueCSVFields(preferred, rows)
	buffer := bytes.NewBuffer([]byte{0xef, 0xbb, 0xbf})
	writer := csv.NewWriter(buffer)
	if err := writer.Write(fields); err != nil {
		return err
	}
	for _, row := range rows {
		values := make([]string, len(fields))
		for index, field := range fields {
			values[index] = marketingCSVValue(row[field])
		}
		if err := writer.Write(values); err != nil {
			return err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	return filesystem.AtomicWritePrivateFile(filepath.Clean(path), buffer.Bytes())
}

func marketingReportCSVFields(
	dimensions []string,
	defaultDimensions []string,
	metrics []string,
	defaultMetrics []string,
) []string {
	if len(dimensions) == 0 {
		dimensions = defaultDimensions
	}
	if len(metrics) == 0 {
		metrics = defaultMetrics
	}
	result := append([]string(nil), dimensions...)
	return append(result, metrics...)
}

func uniqueCSVFields(preferred []string, rows []map[string]any) []string {
	result := []string{}
	seen := map[string]bool{}
	appendField := func(field string) {
		field = strings.TrimSpace(field)
		if field != "" && !seen[field] {
			seen[field] = true
			result = append(result, field)
		}
	}
	for _, field := range preferred {
		appendField(field)
	}
	extra := []string{}
	for _, row := range rows {
		for field := range row {
			if !seen[field] {
				seen[field] = true
				extra = append(extra, field)
			}
		}
	}
	sortStrings(extra)
	return append(result, extra...)
}

func sortStrings(values []string) {
	for index := 1; index < len(values); index++ {
		for current := index; current > 0 && values[current] < values[current-1]; current-- {
			values[current], values[current-1] = values[current-1], values[current]
		}
	}
}

func marketingCSVValue(value any) string {
	if value == nil {
		return ""
	}
	switch value.(type) {
	case map[string]any, []any, []string:
		payload, err := json.Marshal(value)
		if err == nil {
			return string(payload)
		}
	}
	return fmt.Sprint(value)
}

func marketingReportError(err error) *domain.Error {
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
	return domain.NewError("marketing_report_query_failed", err.Error(), 1, nil)
}
