package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/oceanengine"
	authapplication "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/discovery"
	applicationmaterials "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/materials"
	sharedplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/plans"
	applicationmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/plans/marketing"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
)

type optionalJSONNumber struct {
	value json.Number
	set   bool
}

func (number *optionalJSONNumber) String() string {
	if number == nil || !number.set {
		return ""
	}
	return number.value.String()
}

func (number *optionalJSONNumber) Set(value string) error {
	value = strings.TrimSpace(value)
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return errors.New("value must be a finite number")
	}
	number.value = json.Number(value)
	number.set = true
	return nil
}

func (number optionalJSONNumber) Value() any {
	if !number.set {
		return nil
	}
	return number.value
}

type marketingCreateOptions struct {
	configPath    string
	channel       string
	authAccountID string
	advertiserID  string
	planTemplate  string
	videoIDs      repeatedValues
	itemIDs       repeatedValues
	budget        optionalJSONNumber
	bid           optionalJSONNumber
	roiGoal       optionalJSONNumber
	materialDate  string
	productName   string
	productID     string
	projectName   string
	promotionName string
	projectID     string
	promotionOnly bool
	submit        bool
	out           string
}

type marketingCreateComponents struct {
	Preparer applicationmarketing.Preparer
	Executor applicationmarketing.TransactionExecutor
}

func parseMarketingCreateOptions(
	action string,
	args []string,
) (marketingCreateOptions, applicationmarketing.CreateRequest, error) {
	options := marketingCreateOptions{channel: "marketing"}
	flags := flag.NewFlagSet("plans "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.StringVar(&options.advertiserID, "advertiser-id", "", "")
	flags.StringVar(&options.planTemplate, "plan-template", "", "")
	flags.Var(&options.budget, "budget", "")
	flags.Var(&options.bid, "bid", "")
	flags.Var(&options.roiGoal, "roi-goal", "")
	flags.StringVar(&options.materialDate, "material-date", "", "")
	flags.StringVar(&options.productName, "product-name", "", "")
	flags.StringVar(&options.productID, "product-id", "", "")
	flags.StringVar(&options.projectName, "project-name", "", "")
	flags.StringVar(&options.promotionName, "promotion-name", "", "")
	flags.StringVar(&options.projectID, "project-id", "", "")
	flags.BoolVar(&options.promotionOnly, "promotion-only", false, "")
	flags.BoolVar(&options.submit, "submit", false, "")
	flags.StringVar(&options.out, "out", "", "")
	flags.StringVar(&options.channel, "channel", "marketing", "")
	flags.StringVar(&options.authAccountID, "auth-account-id", "", "")
	switch action {
	case "create":
		flags.Var(&options.videoIDs, "video-id", "")
	case "create-creator":
		flags.Var(&options.itemIDs, "item-id", "")
	default:
		return marketingCreateOptions{}, applicationmarketing.CreateRequest{},
			errors.New("unsupported Marketing create action")
	}
	if err := flags.Parse(args); err != nil {
		return marketingCreateOptions{}, applicationmarketing.CreateRequest{}, err
	}
	if len(flags.Args()) != 0 {
		return marketingCreateOptions{}, applicationmarketing.CreateRequest{},
			errors.New("unexpected positional Marketing create arguments")
	}
	options.configPath = strings.TrimSpace(options.configPath)
	options.channel = strings.TrimSpace(options.channel)
	options.authAccountID = strings.TrimSpace(options.authAccountID)
	options.advertiserID = strings.TrimSpace(options.advertiserID)
	options.planTemplate = strings.TrimSpace(options.planTemplate)
	options.videoIDs = splitRepeatedCSV(options.videoIDs)
	options.itemIDs = splitRepeatedCSV(options.itemIDs)
	options.materialDate = strings.TrimSpace(options.materialDate)
	options.productName = strings.TrimSpace(options.productName)
	options.productID = strings.TrimSpace(options.productID)
	options.projectName = strings.TrimSpace(options.projectName)
	options.promotionName = strings.TrimSpace(options.promotionName)
	options.projectID = strings.TrimSpace(options.projectID)
	options.out = strings.TrimSpace(options.out)
	if options.channel != "marketing" {
		return marketingCreateOptions{}, applicationmarketing.CreateRequest{},
			errors.New("Marketing create commands only support --channel marketing")
	}
	kind := applicationmarketing.PrepareUpload
	if action == "create-creator" {
		kind = applicationmarketing.PrepareCreator
	}
	request := applicationmarketing.CreateRequest{
		PrepareRequest: applicationmarketing.PrepareRequest{
			Kind: kind, AdvertiserID: options.advertiserID,
			AuthAccountID: options.authAccountID, PlanTemplate: options.planTemplate,
			Submit: options.submit, OnlinePreflight: options.submit || kind == applicationmarketing.PrepareCreator,
			VideoIDs: []string(options.videoIDs), ItemIDs: []string(options.itemIDs),
			Budget: options.budget.Value(), CPABid: options.bid.Value(), ROIGoal: options.roiGoal.Value(),
			MaterialDate: options.materialDate, ProductName: options.productName,
			ProductID: options.productID, ProjectName: options.projectName,
			PromotionName: options.promotionName, ProjectID: options.projectID,
		},
		PromotionOnly: options.promotionOnly,
	}
	return options, request, nil
}

func (runner Runner) runMarketingCreatePlan(
	ctx context.Context,
	action string,
	args []string,
	stateRoot string,
	credentialsStore authapplication.CredentialStore,
	stdout io.Writer,
) int {
	options, request, err := parseMarketingCreateOptions(action, args)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("invalid_arguments", err.Error(), 2, nil))
		return 2
	}
	runtime := runner.MarketingPlans
	service := runtime.CreateService
	if service == nil {
		configPath := runner.resolveMarketingPlanConfigPath(options.configPath)
		configReader := applicationmarketing.ConfigReader(filesystem.ConfigStore{Path: configPath})
		if runtime.ConfigFactory != nil {
			configReader = runtime.ConfigFactory(configPath)
		}
		service, err = runner.buildMarketingCreateService(
			configReader, request.Submit || request.OnlinePreflight, stateRoot, credentialsStore,
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
				"configuration_error", "failed to write Marketing plan result", 2, err,
			))
			return 2
		}
		if executionErr != nil || result.SubmitBlocked || request.Submit && result.Status != "completed" {
			return 1
		}
		return 0
	}
	if executionErr != nil {
		mapped := marketingPlanError(executionErr)
		WriteDomainError(stdout, mapped)
		return mapped.ExitCode
	}
	WriteDomainError(stdout, domain.NewError(
		"marketing_plan_write_failed", "Marketing create service returned an empty result", 1, nil,
	))
	return 1
}

func (runner Runner) buildMarketingCreateService(
	config applicationmarketing.ConfigReader,
	online bool,
	stateRoot string,
	credentialsStore authapplication.CredentialStore,
) (MarketingCreateService, error) {
	components, err := runner.buildMarketingCreateComponents(
		config, online, stateRoot, credentialsStore,
	)
	if err != nil {
		return nil, err
	}
	return applicationmarketing.CreateService{
		Preparer: components.Preparer, Executor: components.Executor,
	}, nil
}

func (runner Runner) buildMarketingCreateComponents(
	config applicationmarketing.ConfigReader,
	online bool,
	stateRoot string,
	credentialsStore authapplication.CredentialStore,
) (marketingCreateComponents, error) {
	runtime := runner.MarketingPlans
	preparer := applicationmarketing.Preparer{Config: config, Now: runtime.Now}
	if !online {
		return marketingCreateComponents{Preparer: preparer}, nil
	}
	factory := runtime.ClientFactory
	var err error
	if factory == nil {
		factory, err = oceanengine.NewClientFactory(oceanengine.FactoryOptions{})
		if err != nil {
			return marketingCreateComponents{}, err
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
	tokenManager := &authapplication.TokenManager{
		Credentials: credentialsStore, Authorizations: authorizations,
		Locks: refreshLocker, OAuth: oauth, Now: runtime.Now,
	}
	materials := runtime.MaterialService
	if materials == nil {
		materials = applicationmaterials.Service{
			Tokens: tokenManager,
			Reader: oceanengine.MarketingMaterialsAdapter{Factory: factory, Retry: runtime.Retry},
			Now:    runtime.Now,
		}
	}
	discoveryService := runtime.Discovery
	if discoveryService == nil {
		discoveryService = discovery.Service{
			Tokens: tokenManager,
			Reader: oceanengine.MarketingDiscoveryAdapter{Factory: factory, Retry: runtime.Retry},
		}
	}
	runtimeAssets := runtime.RuntimeAssets
	if runtimeAssets == nil {
		runtimeAssets = applicationmarketing.AssetResolver{Discovery: discoveryService}
	}
	preparer.Materials = materials
	preparer.CreatorCovers = applicationmarketing.HistoricalCreatorCoverResolver{Discovery: discoveryService}
	preparer.RuntimeAssets = runtimeAssets
	credentialProvider := runtime.Credentials
	if credentialProvider == nil {
		credentialProvider = sharedplans.TokenCredentialProvider{Tokens: tokenManager}
	}
	locker := runtime.Locker
	if locker == nil {
		locker = filesystem.AdvertiserLockStore{Root: stateRoot}
	}
	planAdapter := oceanengine.MarketingPlanAdapter{Factory: factory, Retry: runtime.Retry}
	writer := runtime.PlanWriter
	if writer == nil {
		writer = planAdapter
	}
	reconciler := runtime.PlanReconciler
	if reconciler == nil {
		reconciler = planAdapter
	}
	executor := applicationmarketing.Executor{
		Guard: sharedplans.GuardedExecutor{
			Credentials: credentialProvider, Locks: locker, Now: runtime.Now,
		},
		Writer: writer, Reconciler: reconciler,
	}
	return marketingCreateComponents{Preparer: preparer, Executor: executor}, nil
}

func (runner Runner) resolveMarketingPlanConfigPath(explicit string) string {
	cwd := runner.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	userHome := runner.UserHome
	if userHome == "" {
		userHome, _ = os.UserHomeDir()
	}
	getenv := runner.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	return filepath.Clean(filesystem.ResolveConfigPath(explicit, cwd, getenv, userHome))
}
