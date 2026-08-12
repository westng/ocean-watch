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
	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	sharedplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans"
	applicationmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/marketing"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
	portmarketing "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/marketing"
)

type MarketingMutationService interface {
	Execute(context.Context, applicationmarketing.MutationCommand) (applicationmarketing.MutationResult, error)
}

type MarketingCreateService interface {
	Execute(context.Context, applicationmarketing.CreateRequest) (applicationmarketing.CreateResult, error)
}

type MarketingCreatorBatchService interface {
	Execute(context.Context, applicationmarketing.CreatorBatchRequest) (applicationmarketing.CreatorBatchResult, error)
}

type MarketingUploadBatchService interface {
	Execute(context.Context, applicationmarketing.UploadBatchRequest) (applicationmarketing.UploadBatchResult, error)
}

type MarketingPlanRuntime struct {
	CreateService       MarketingCreateService
	UploadBatchService  MarketingUploadBatchService
	CreatorBatchService MarketingCreatorBatchService
	MutationService     MarketingMutationService
	Credentials         sharedplans.CredentialProvider
	Locker              sharedplans.AdvertiserLocker
	ConfigFactory       func(string) applicationmarketing.ConfigReader
	MaterialService     applicationmarketing.MaterialService
	Discovery           applicationmarketing.RuntimeDiscovery
	RuntimeAssets       applicationmarketing.RuntimeAssetResolver
	PlanWriter          portmarketing.PlanWriter
	PlanReconciler      portmarketing.PlanReconciler
	Writer              portmarketing.PlanMutationWriter
	Reader              portmarketing.PlanMutationReader
	Authorizations      authapplication.AuthorizationStore
	RefreshLocker       authapplication.RefreshLocker
	OAuth               authapplication.OAuthAdapter
	ClientFactory       *oceanengine.ClientFactory
	Retry               platformretry.Policy
	Now                 func() time.Time
}

type marketingMutationOptions struct {
	configPath         string
	advertiserID       string
	authAccountID      string
	projectIDs         repeatedValues
	promotionIDs       repeatedValues
	adIDs              repeatedValues
	status             string
	value              string
	deepExternalAction string
	out                string
	confirmDelete      bool
	submit             bool
}

func parseMarketingMutationOptions(
	action string,
	args []string,
) (marketingMutationOptions, applicationmarketing.MutationCommand, error) {
	options := marketingMutationOptions{}
	flags := flag.NewFlagSet("plans "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.StringVar(&options.advertiserID, "advertiser-id", "", "")
	flags.StringVar(&options.authAccountID, "auth-account-id", "", "")
	flags.Var(&options.projectIDs, "project-id", "")
	flags.Var(&options.promotionIDs, "promotion-id", "")
	flags.Var(&options.adIDs, "ad-id", "")
	flags.StringVar(&options.status, "status", "", "")
	flags.StringVar(&options.value, "value", "", "")
	flags.StringVar(&options.deepExternalAction, "deep-external-action", "", "")
	flags.BoolVar(&options.confirmDelete, "confirm-delete", false, "")
	flags.BoolVar(&options.submit, "submit", false, "")
	flags.StringVar(&options.out, "out", "", "")
	if err := flags.Parse(args); err != nil {
		return marketingMutationOptions{}, applicationmarketing.MutationCommand{}, err
	}
	if len(flags.Args()) != 0 {
		return marketingMutationOptions{}, applicationmarketing.MutationCommand{},
			errors.New("unexpected positional Marketing plan arguments")
	}
	options.configPath = strings.TrimSpace(options.configPath)
	options.advertiserID = strings.TrimSpace(options.advertiserID)
	options.authAccountID = strings.TrimSpace(options.authAccountID)
	options.projectIDs = splitRepeatedCSV(options.projectIDs)
	options.promotionIDs = splitRepeatedCSV(options.promotionIDs)
	options.adIDs = splitRepeatedCSV(options.adIDs)
	options.status = strings.TrimSpace(options.status)
	options.value = strings.TrimSpace(options.value)
	options.deepExternalAction = strings.TrimSpace(options.deepExternalAction)
	options.out = strings.TrimSpace(options.out)
	if options.deepExternalAction != "" || options.confirmDelete || len(options.adIDs) != 0 {
		return marketingMutationOptions{}, applicationmarketing.MutationCommand{},
			errors.New("Qianchuan-only options cannot be used for Marketing")
	}

	command := applicationmarketing.MutationCommand{
		AdvertiserID: options.advertiserID, AuthAccountID: options.authAccountID,
		Submit: options.submit, Status: options.status, Value: options.value,
	}
	switch action {
	case "update-project-status":
		command.Kind = portmarketing.MutationProjectStatus
		command.ObjectIDs = []string(options.projectIDs)
		if len(options.promotionIDs) != 0 {
			return marketingMutationOptions{}, applicationmarketing.MutationCommand{},
				errors.New("this operation accepts only --project-id")
		}
	case "update-promotion-status":
		command.Kind = portmarketing.MutationPromotionStatus
		command.ObjectIDs = []string(options.promotionIDs)
		if len(options.projectIDs) != 0 {
			return marketingMutationOptions{}, applicationmarketing.MutationCommand{},
				errors.New("this operation accepts only --promotion-id")
		}
	case "update-budget":
		command.Kind = portmarketing.MutationPromotionBudget
		command.ObjectIDs = []string(options.promotionIDs)
		if len(options.projectIDs) != 0 {
			return marketingMutationOptions{}, applicationmarketing.MutationCommand{},
				errors.New("this operation accepts only --promotion-id")
		}
	case "update-bid":
		command.Kind = portmarketing.MutationPromotionBid
		command.ObjectIDs = []string(options.promotionIDs)
		if len(options.projectIDs) != 0 {
			return marketingMutationOptions{}, applicationmarketing.MutationCommand{},
				errors.New("this operation accepts only --promotion-id")
		}
	case "update-roi":
		command.Kind = portmarketing.MutationProjectROI
		command.ObjectIDs = []string(options.projectIDs)
		if len(options.promotionIDs) != 0 {
			return marketingMutationOptions{}, applicationmarketing.MutationCommand{},
				errors.New("this operation accepts only --project-id")
		}
	default:
		return marketingMutationOptions{}, applicationmarketing.MutationCommand{},
			errors.New("unsupported Marketing plan action")
	}
	return options, command, nil
}

func (runner Runner) runMarketingPlan(
	ctx context.Context,
	action string,
	args []string,
	stateRoot string,
	credentialsStore authapplication.CredentialStore,
	stdout io.Writer,
) int {
	switch action {
	case "create", "create-creator":
		return runner.runMarketingCreatePlan(ctx, action, args, stateRoot, credentialsStore, stdout)
	case "batch-upload":
		return runner.runMarketingUploadBatch(ctx, args, stateRoot, credentialsStore, stdout)
	case "batch-creator":
		return runner.runMarketingCreatorBatch(ctx, args, stateRoot, credentialsStore, stdout)
	}
	options, command, err := parseMarketingMutationOptions(action, args)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("invalid_arguments", err.Error(), 2, nil))
		return 2
	}
	runtime := runner.MarketingPlans
	service := runtime.MutationService
	if service == nil && !command.Submit {
		service = applicationmarketing.MutationExecutor{}
	}
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
		credentialProvider := runtime.Credentials
		if credentialProvider == nil {
			credentialProvider = sharedplans.TokenCredentialProvider{Tokens: &authapplication.TokenManager{
				Credentials: credentialsStore, Authorizations: authorizations,
				Locks: refreshLocker, OAuth: oauth, Now: runtime.Now,
			}}
		}
		locker := runtime.Locker
		if locker == nil {
			locker = filesystem.AdvertiserLockStore{Root: stateRoot}
		}
		adapter := oceanengine.MarketingPlanAdapter{Factory: factory, Retry: runtime.Retry}
		writer := runtime.Writer
		if writer == nil {
			writer = adapter
		}
		reader := runtime.Reader
		if reader == nil {
			reader = adapter
		}
		service = applicationmarketing.MutationExecutor{
			Guard: sharedplans.GuardedExecutor{
				Credentials: credentialProvider, Locks: locker, Now: runtime.Now,
			},
			Writer: writer, Reader: reader,
		}
	}
	result, err := service.Execute(ctx, command)
	if err != nil {
		mapped := marketingPlanError(err)
		WriteDomainError(stdout, mapped)
		return mapped.ExitCode
	}
	if err := WriteJSONDestination(stdout, result, options.out); err != nil {
		WriteDomainError(stdout, domain.WrapError(
			"configuration_error", "failed to write Marketing plan result", 2, err,
		))
		return 2
	}
	return result.ExitCode
}

func marketingPlanError(err error) *domain.Error {
	var domainErr *domain.Error
	if errors.As(err, &domainErr) {
		return domainErr
	}
	var batchErr *applicationmarketing.BatchInputError
	if errors.As(err, &batchErr) {
		return domain.NewError(batchErr.Code, batchErr.Message, 2, nil)
	}
	if errors.Is(err, context.Canceled) {
		return domain.NewError("interrupted", "operation interrupted", 130, nil)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return domain.NewError("timeout", "operation timed out", 1, nil)
	}
	return domain.NewError("marketing_plan_write_failed", err.Error(), 1, nil)
}
