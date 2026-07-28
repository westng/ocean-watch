package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/filesystem"
	authapplication "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/auth"
	applicationmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/plans/marketing"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
)

type marketingUploadBatchOptions struct {
	configPath         string
	accounts           repeatedValues
	planTemplate       string
	accountTemplates   repeatedValues
	date               string
	filename           string
	materialDate       string
	productName        string
	productID          string
	budget             optionalJSONNumber
	bid                optionalJSONNumber
	roiGoal            optionalJSONNumber
	videosPerUnit      int
	maxVideos          int
	startIndex         int
	accountConcurrency int
	groupConcurrency   int
	coverConcurrency   int
	coverAttempts      int
	coverWaitSeconds   float64
	pageSize           int
	adGetBatchSize     int
	validateAdGet      bool
	skipMissingCover   bool
	includePayloads    bool
	submit             bool
	out                string
	channel            string
	authAccountID      string
}

func parseMarketingUploadBatchOptions(
	args []string,
) (marketingUploadBatchOptions, applicationmarketing.UploadBatchRequest, error) {
	options := marketingUploadBatchOptions{
		date: "today", startIndex: 1,
		accountConcurrency: 2, groupConcurrency: 2, coverConcurrency: 4,
		coverAttempts: 8, coverWaitSeconds: 2, pageSize: 100, adGetBatchSize: 50,
		validateAdGet: true, skipMissingCover: true, channel: "marketing",
	}
	flags := flag.NewFlagSet("plans batch-upload", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.Var(&options.accounts, "accounts", "")
	flags.StringVar(&options.planTemplate, "plan-template", "", "")
	flags.Var(&options.accountTemplates, "account-template", "")
	flags.StringVar(&options.date, "date", "today", "")
	flags.StringVar(&options.filename, "filename", "", "")
	flags.StringVar(&options.materialDate, "material-date", "", "")
	flags.StringVar(&options.productName, "product-name", "", "")
	flags.StringVar(&options.productID, "product-id", "", "")
	flags.Var(&options.budget, "budget", "")
	flags.Var(&options.bid, "bid", "")
	flags.Var(&options.roiGoal, "roi-goal", "")
	flags.IntVar(&options.videosPerUnit, "videos-per-unit", 0, "")
	flags.IntVar(&options.maxVideos, "max-videos", 0, "")
	flags.IntVar(&options.startIndex, "start-index", 1, "")
	flags.IntVar(&options.accountConcurrency, "account-concurrency", 2, "")
	flags.IntVar(&options.groupConcurrency, "group-concurrency", 2, "")
	flags.IntVar(&options.coverConcurrency, "cover-concurrency", 4, "")
	flags.IntVar(&options.coverAttempts, "cover-attempts", 8, "")
	flags.Float64Var(&options.coverWaitSeconds, "cover-wait-sec", 2, "")
	flags.IntVar(&options.pageSize, "page-size", 100, "")
	flags.IntVar(&options.adGetBatchSize, "ad-get-batch-size", 50, "")
	registerBooleanOptionalAction(flags, "validate-ad-get", &options.validateAdGet)
	registerBooleanOptionalAction(flags, "skip-missing-cover", &options.skipMissingCover)
	flags.BoolVar(&options.includePayloads, "include-payloads", false, "")
	flags.BoolVar(&options.submit, "submit", false, "")
	flags.StringVar(&options.out, "out", "", "")
	flags.StringVar(&options.channel, "channel", "marketing", "")
	flags.StringVar(&options.authAccountID, "auth-account-id", "", "")
	if err := flags.Parse(args); err != nil {
		return marketingUploadBatchOptions{}, applicationmarketing.UploadBatchRequest{}, err
	}
	if len(flags.Args()) != 0 {
		return marketingUploadBatchOptions{}, applicationmarketing.UploadBatchRequest{},
			errors.New("unexpected positional Marketing upload batch arguments")
	}
	options.configPath = strings.TrimSpace(options.configPath)
	options.planTemplate = strings.TrimSpace(options.planTemplate)
	options.date = strings.TrimSpace(options.date)
	options.filename = strings.TrimSpace(options.filename)
	options.materialDate = strings.TrimSpace(options.materialDate)
	options.productName = strings.TrimSpace(options.productName)
	options.productID = strings.TrimSpace(options.productID)
	options.out = strings.TrimSpace(options.out)
	options.channel = strings.TrimSpace(options.channel)
	options.authAccountID = strings.TrimSpace(options.authAccountID)
	if options.channel != "marketing" {
		return marketingUploadBatchOptions{}, applicationmarketing.UploadBatchRequest{},
			errors.New("Marketing upload batches only support --channel marketing")
	}
	if math.IsNaN(options.coverWaitSeconds) || math.IsInf(options.coverWaitSeconds, 0) ||
		options.coverWaitSeconds < 0 || options.coverWaitSeconds > 300 {
		return marketingUploadBatchOptions{}, applicationmarketing.UploadBatchRequest{},
			errors.New("cover-wait-sec must be between 0 and 300")
	}
	visited := map[string]bool{}
	flags.Visit(func(current *flag.Flag) { visited[current.Name] = true })
	for name, value := range map[string]int{
		"videos-per-unit": options.videosPerUnit, "max-videos": options.maxVideos,
		"start-index": options.startIndex, "account-concurrency": options.accountConcurrency,
		"group-concurrency": options.groupConcurrency, "cover-concurrency": options.coverConcurrency,
		"cover-attempts": options.coverAttempts, "page-size": options.pageSize,
		"ad-get-batch-size": options.adGetBatchSize,
	} {
		if (visited[name] || name != "videos-per-unit" && name != "max-videos") && value < 1 {
			return marketingUploadBatchOptions{}, applicationmarketing.UploadBatchRequest{},
				fmt.Errorf("%s must be >= 1", name)
		}
	}
	request := applicationmarketing.UploadBatchRequest{
		ConfigPath: options.configPath, Accounts: []string(options.accounts),
		PlanTemplate: options.planTemplate, AccountTemplates: []string(options.accountTemplates),
		Date: options.date, Filename: options.filename, MaterialDate: options.materialDate,
		ProductName: options.productName, ProductID: options.productID,
		Budget: options.budget.Value(), CPABid: options.bid.Value(), ROIGoal: options.roiGoal.Value(),
		VideosPerUnit: options.videosPerUnit, MaxVideos: options.maxVideos, StartIndex: options.startIndex,
		AccountConcurrency: options.accountConcurrency, GroupConcurrency: options.groupConcurrency,
		CoverConcurrency: options.coverConcurrency, CoverAttempts: options.coverAttempts,
		CoverWait: time.Duration(options.coverWaitSeconds * float64(time.Second)), CoverWaitSet: true,
		PageSize: options.pageSize, AdGetBatchSize: options.adGetBatchSize,
		ValidateAdGet: options.validateAdGet, SkipMissingCover: options.skipMissingCover,
		IncludePayloads: options.includePayloads, Submit: options.submit,
		Channel: options.channel, AuthAccountID: options.authAccountID,
	}
	return options, request, nil
}

func registerBooleanOptionalAction(flags *flag.FlagSet, name string, target *bool) {
	flags.BoolFunc(name, "", func(value string) error {
		if value != "true" {
			return fmt.Errorf("--%s does not accept a value", name)
		}
		*target = true
		return nil
	})
	flags.BoolFunc("no-"+name, "", func(value string) error {
		if value != "true" {
			return fmt.Errorf("--no-%s does not accept a value", name)
		}
		*target = false
		return nil
	})
}

func (runner Runner) runMarketingUploadBatch(
	ctx context.Context,
	args []string,
	stateRoot string,
	credentialsStore authapplication.CredentialStore,
	stdout io.Writer,
) int {
	options, request, err := parseMarketingUploadBatchOptions(args)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("invalid_arguments", err.Error(), 2, nil))
		return 2
	}
	configPath := runner.resolveMarketingPlanConfigPath(options.configPath)
	request.ConfigPath = configPath
	service := runner.MarketingPlans.UploadBatchService
	if service == nil {
		configReader := applicationmarketing.ConfigReader(filesystem.ConfigStore{Path: configPath})
		if runner.MarketingPlans.ConfigFactory != nil {
			configReader = runner.MarketingPlans.ConfigFactory(configPath)
		}
		service, err = runner.buildMarketingUploadBatchService(
			configReader, stateRoot, credentialsStore,
		)
		if err != nil {
			mapped := marketingPlanError(err)
			WriteDomainError(stdout, mapped)
			return mapped.ExitCode
		}
	}
	result, executionErr := service.Execute(ctx, request)
	if result.Mode != "" {
		if err := WriteJSONDestination(stdout, result, options.out); err != nil {
			WriteDomainError(stdout, domain.WrapError(
				"configuration_error", "failed to write Marketing upload batch result", 2, err,
			))
			return 2
		}
		if executionErr != nil {
			return marketingPlanError(executionErr).ExitCode
		}
		return result.ExitCode
	}
	if executionErr != nil {
		mapped := marketingPlanError(executionErr)
		WriteDomainError(stdout, mapped)
		return mapped.ExitCode
	}
	WriteDomainError(stdout, domain.NewError(
		"marketing_plan_write_failed", "Marketing upload batch service returned an empty result", 1, nil,
	))
	return 1
}

func (runner Runner) buildMarketingUploadBatchService(
	config applicationmarketing.ConfigReader,
	stateRoot string,
	credentialsStore authapplication.CredentialStore,
) (MarketingUploadBatchService, error) {
	components, err := runner.buildMarketingCreateComponents(
		config, true, stateRoot, credentialsStore,
	)
	if err != nil {
		return nil, err
	}
	journals := filesystem.OperationJournalStore{Root: stateRoot}
	return applicationmarketing.UploadBatchService{
		Config: config, Materials: components.Preparer.Materials,
		RuntimeAssets: components.Preparer.RuntimeAssets, Executor: components.Executor,
		Journals: journals, Catalog: journals, ScopeLocker: journals,
		Now: runner.MarketingPlans.Now,
	}, nil
}
