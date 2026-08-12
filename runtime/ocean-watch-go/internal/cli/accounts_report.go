package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"strings"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/oceanengine"
	applicationaccounts "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/accounts"
	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type AccountReportService interface {
	Report(context.Context, applicationaccounts.Query) (domain.AccountReportResult, *domain.Error)
}

type AccountReportRuntime struct {
	Service        AccountReportService
	Authorizations authapplication.AuthorizationStore
	RefreshLocker  authapplication.RefreshLocker
	OAuth          authapplication.OAuthAdapter
	ClientFactory  *oceanengine.ClientFactory
	Now            func() time.Time
}

type accountReportOptions struct {
	configPath      string
	channels        channelList
	startDate       string
	endDate         string
	includeDisabled bool
	concurrency     int
	out             string
}

type channelList []string

func (channels *channelList) String() string {
	return strings.Join(*channels, ",")
}

func (channels *channelList) Set(value string) error {
	*channels = append(*channels, value)
	return nil
}

func parseAccountReportOptions(args []string, now func() time.Time) (accountReportOptions, error) {
	today := accountReportToday(now)
	options := accountReportOptions{startDate: today, endDate: today, concurrency: applicationaccounts.DefaultConcurrency}
	flags := flag.NewFlagSet("accounts report", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.Var(&options.channels, "channel", "")
	flags.StringVar(&options.startDate, "start-date", today, "")
	flags.StringVar(&options.endDate, "end-date", today, "")
	flags.BoolVar(&options.includeDisabled, "include-disabled", false, "")
	flags.IntVar(&options.concurrency, "concurrency", applicationaccounts.DefaultConcurrency, "")
	flags.StringVar(&options.out, "out", "", "")
	if err := flags.Parse(args); err != nil {
		return accountReportOptions{}, err
	}
	if len(flags.Args()) != 0 {
		return accountReportOptions{}, errors.New("unexpected positional account report arguments")
	}
	if options.concurrency < 1 || options.concurrency > applicationaccounts.MaxConcurrency {
		return accountReportOptions{}, errors.New("concurrency must be between 1 and 8")
	}
	if err := validateAccountReportDates(options.startDate, options.endDate); err != nil {
		return accountReportOptions{}, err
	}
	return options, nil
}

func validateAccountReportDates(startDate, endDate string) error {
	start, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		return errors.New("start-date and end-date must use YYYY-MM-DD")
	}
	end, err := time.Parse("2006-01-02", endDate)
	if err != nil {
		return errors.New("start-date and end-date must use YYYY-MM-DD")
	}
	if start.After(end) {
		return errors.New("start-date cannot be after end-date")
	}
	return nil
}

func accountReportToday(now func() time.Time) string {
	current := time.Now()
	if now != nil {
		current = now()
	}
	return current.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
}

func RunAccountReport(
	ctx context.Context,
	args []string,
	service AccountReportService,
	stdout io.Writer,
	now func() time.Time,
) int {
	options, err := parseAccountReportOptions(args, now)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("invalid_arguments", err.Error(), 2, nil))
		return 2
	}
	if service == nil {
		WriteDomainError(stdout, domain.NewError("unexpected_error", "account report service is unavailable", 1, nil))
		return 1
	}
	channels := make([]domain.Channel, 0, len(options.channels))
	for _, value := range options.channels {
		channel, parseErr := domain.ParseChannel(value)
		if parseErr != nil {
			WriteDomainError(stdout, domain.NewError("invalid_channel", parseErr.Error(), 2, nil))
			return 2
		}
		channels = append(channels, channel)
	}
	result, reportErr := service.Report(ctx, applicationaccounts.Query{
		Channels: channels, StartDate: options.startDate, EndDate: options.endDate,
		IncludeDisabled: options.includeDisabled, Concurrency: options.concurrency,
	})
	if reportErr != nil {
		WriteDomainError(stdout, reportErr)
		return reportErr.ExitCode
	}
	if err := WriteJSONDestination(stdout, result, options.out); err != nil {
		WriteDomainError(stdout, domain.WrapError("configuration_error", "failed to write output", 2, err))
		return 2
	}
	if !result.OK {
		return 1
	}
	return 0
}

func (runner Runner) runAccountReport(
	ctx context.Context,
	args []string,
	store filesystem.AccountStore,
	stateRoot string,
	credentialsStore authapplication.CredentialStore,
	stdout io.Writer,
) int {
	runtime := runner.AccountReports
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
		tokens := &authapplication.TokenManager{
			Credentials: credentialsStore, Authorizations: authorizations,
			Locks: refreshLocker, OAuth: oauth, Now: runtime.Now,
		}
		service = applicationaccounts.Reporter{
			Store: store, Tokens: tokens,
			Marketing: oceanengine.MarketingAccountReportAdapter{Factory: factory},
			Qianchuan: oceanengine.QianchuanAccountReportAdapter{Factory: factory},
		}
	}
	return RunAccountReport(ctx, args, service, stdout, runtime.Now)
}
