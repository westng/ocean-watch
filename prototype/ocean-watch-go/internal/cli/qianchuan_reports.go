package cli

import (
	"context"
	"errors"
	"flag"
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
			reader = oceanengine.QianchuanReportAdapter{Factory: factory, Retry: runtime.Retry}
		}
		tokens := &authapplication.TokenManager{
			Credentials: credentialsStore, Authorizations: authorizations,
			Locks: refreshLocker, OAuth: oauth, Now: runtime.Now,
		}
		service = applicationreports.Service{Tokens: tokens, Reader: reader, Now: runtime.Now}
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
