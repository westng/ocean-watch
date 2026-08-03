package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/oceanengine"
	adapterworkmetadata "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/workmetadata"
	authapplication "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/auth"
	sharedplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/plans"
	applicationqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/plans/qianchuan"
	applicationworkmetadata "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/workmetadata"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	platformretry "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/retry"
	portqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/qianchuan"
)

const qianchuanPayloadLimit = int64(8 << 20)

type QianchuanPlanCommandService interface {
	CreatePlan(context.Context, applicationqianchuan.CreatePlanCommand) (applicationqianchuan.CreateCommandResult, error)
	BatchWorks(context.Context, applicationqianchuan.BatchWorksCommand) (applicationqianchuan.BatchCommandResult, error)
	RemoveWorks(context.Context, applicationqianchuan.RemoveWorksCommand) (applicationqianchuan.RemoveResult, error)
}

type QianchuanPlanMutationService interface {
	Execute(context.Context, applicationqianchuan.MutationCommand) (applicationqianchuan.MutationResult, error)
}

type QianchuanPlanRuntime struct {
	Commands       QianchuanPlanCommandService
	Mutations      QianchuanPlanMutationService
	Reader         portqianchuan.Reader
	Writer         portqianchuan.Writer
	Credentials    sharedplans.CredentialProvider
	Locker         sharedplans.AdvertiserLocker
	Tokens         authapplication.TokenProvider
	Links          applicationqianchuan.WorkLinkResolver
	OwnerHints     applicationqianchuan.OwnerHintCache
	Authorizations authapplication.AuthorizationStore
	RefreshLocker  authapplication.RefreshLocker
	OAuth          authapplication.OAuthAdapter
	ClientFactory  *oceanengine.ClientFactory
	HTTPClient     *http.Client
	Retry          platformretry.Policy
	Now            func() time.Time
}

type qianchuanCreateOptions struct {
	configPath    string
	payloadFile   string
	payloadJSON   string
	planTemplate  string
	liveTemplate  string
	name          string
	advertiserID  string
	authAccountID string
	submit        bool
	out           string
}

type qianchuanBatchOptions struct {
	configPath        string
	planTemplate      string
	workURLs          repeatedValues
	concurrency       int
	authAccountID     string
	noLinkMetadataAPI bool
	includePayloads   bool
	submit            bool
	out               string
}

type qianchuanRemoveOptions struct {
	configPath    string
	advertiserID  string
	authAccountID string
	adID          string
	workURLs      repeatedValues
	concurrency   int
	confirmDelete bool
	submit        bool
	out           string
}

type qianchuanMutationOptions struct {
	configPath         string
	advertiserID       string
	authAccountID      string
	adIDs              repeatedValues
	status             string
	value              string
	deepExternalAction string
	confirmDelete      bool
	submit             bool
	out                string
}

func parseQianchuanCreateOptions(args []string, stdin io.Reader) (qianchuanCreateOptions, applicationqianchuan.CreatePlanCommand, error) {
	options := qianchuanCreateOptions{}
	flags := flag.NewFlagSet("plans create-qianchuan", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.StringVar(&options.payloadFile, "payload-file", "", "")
	flags.StringVar(&options.payloadJSON, "payload-json", "", "")
	flags.StringVar(&options.planTemplate, "plan-template", "", "")
	flags.StringVar(&options.liveTemplate, "live-template", "", "")
	flags.StringVar(&options.name, "name", "", "")
	flags.StringVar(&options.advertiserID, "advertiser-id", "", "")
	flags.StringVar(&options.authAccountID, "auth-account-id", "", "")
	flags.BoolVar(&options.submit, "submit", false, "")
	flags.StringVar(&options.out, "out", "", "")
	if err := flags.Parse(args); err != nil {
		return qianchuanCreateOptions{}, applicationqianchuan.CreatePlanCommand{}, err
	}
	if len(flags.Args()) != 0 {
		return qianchuanCreateOptions{}, applicationqianchuan.CreatePlanCommand{}, errors.New("unexpected positional Qianchuan create arguments")
	}
	options.trim()
	sources := 0
	if options.payloadFile != "" {
		sources++
	}
	if options.payloadJSON != "" {
		sources++
	}
	if options.planTemplate != "" {
		sources++
	}
	if options.liveTemplate != "" {
		sources++
	}
	if sources != 1 {
		return qianchuanCreateOptions{}, applicationqianchuan.CreatePlanCommand{}, errors.New("exactly one --payload-file, --payload-json, --plan-template, or --live-template is required")
	}
	payload := json.RawMessage(nil)
	var err error
	if options.payloadFile != "" {
		payload, err = readQianchuanPayload(options.payloadFile, stdin)
		if err != nil {
			return qianchuanCreateOptions{}, applicationqianchuan.CreatePlanCommand{}, err
		}
	} else if options.payloadJSON != "" {
		payload, err = validateQianchuanPayload([]byte(options.payloadJSON))
		if err != nil {
			return qianchuanCreateOptions{}, applicationqianchuan.CreatePlanCommand{}, err
		}
	}
	return options, applicationqianchuan.CreatePlanCommand{
		Payload: payload, PlanTemplate: options.planTemplate, LiveTemplate: options.liveTemplate,
		Name: options.name, AdvertiserID: options.advertiserID,
		AuthAccountID: options.authAccountID, Submit: options.submit,
	}, nil
}

func parseQianchuanBatchOptions(args []string) (qianchuanBatchOptions, applicationqianchuan.BatchWorksCommand, error) {
	options := qianchuanBatchOptions{concurrency: applicationqianchuan.DefaultBatchConcurrency}
	flags := flag.NewFlagSet("plans batch-qianchuan-works", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.StringVar(&options.planTemplate, "plan-template", "", "")
	flags.Var(&options.workURLs, "work-url", "")
	flags.IntVar(&options.concurrency, "concurrency", applicationqianchuan.DefaultBatchConcurrency, "")
	flags.StringVar(&options.authAccountID, "auth-account-id", "", "")
	flags.BoolVar(&options.noLinkMetadataAPI, "no-link-metadata-api", false, "")
	flags.BoolVar(&options.submit, "submit", false, "")
	flags.BoolVar(&options.includePayloads, "include-payloads", false, "")
	flags.StringVar(&options.out, "out", "", "")
	if err := flags.Parse(args); err != nil {
		return qianchuanBatchOptions{}, applicationqianchuan.BatchWorksCommand{}, err
	}
	if len(flags.Args()) != 0 {
		return qianchuanBatchOptions{}, applicationqianchuan.BatchWorksCommand{}, errors.New("unexpected positional Qianchuan batch arguments")
	}
	options.trim()
	if options.planTemplate == "" {
		return qianchuanBatchOptions{}, applicationqianchuan.BatchWorksCommand{}, errors.New("plan_template is required")
	}
	if len(options.workURLs) == 0 {
		return qianchuanBatchOptions{}, applicationqianchuan.BatchWorksCommand{}, errors.New("at least one work URL is required")
	}
	if options.concurrency < 1 || options.concurrency > applicationworkmetadata.MaxConcurrency {
		return qianchuanBatchOptions{}, applicationqianchuan.BatchWorksCommand{}, errors.New("concurrency must be between 1 and 10")
	}
	return options, applicationqianchuan.BatchWorksCommand{
		PlanTemplate: options.planTemplate, WorkURLs: append([]string(nil), options.workURLs...),
		Concurrency: options.concurrency, AuthAccountID: options.authAccountID,
		NoLinkMetadataAPI: options.noLinkMetadataAPI, IncludePayloads: options.includePayloads,
		Submit: options.submit,
	}, nil
}

func parseQianchuanRemoveOptions(args []string) (qianchuanRemoveOptions, applicationqianchuan.RemoveWorksCommand, error) {
	options := qianchuanRemoveOptions{concurrency: applicationworkmetadata.DefaultConcurrency}
	flags := flag.NewFlagSet("plans remove-qianchuan-work", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.StringVar(&options.advertiserID, "advertiser-id", "", "")
	flags.StringVar(&options.adID, "ad-id", "", "")
	flags.Var(&options.workURLs, "work-url", "")
	flags.IntVar(&options.concurrency, "concurrency", applicationworkmetadata.DefaultConcurrency, "")
	flags.StringVar(&options.authAccountID, "auth-account-id", "", "")
	flags.BoolVar(&options.submit, "submit", false, "")
	flags.BoolVar(&options.confirmDelete, "confirm-delete", false, "")
	flags.StringVar(&options.out, "out", "", "")
	if err := flags.Parse(args); err != nil {
		return qianchuanRemoveOptions{}, applicationqianchuan.RemoveWorksCommand{}, err
	}
	if len(flags.Args()) != 0 {
		return qianchuanRemoveOptions{}, applicationqianchuan.RemoveWorksCommand{}, errors.New("unexpected positional Qianchuan material removal arguments")
	}
	options.trim()
	if err := validateCLIPositiveID(options.advertiserID, "advertiser_id", true); err != nil {
		return qianchuanRemoveOptions{}, applicationqianchuan.RemoveWorksCommand{}, err
	}
	if err := validateCLIPositiveID(options.adID, "ad_id", true); err != nil {
		return qianchuanRemoveOptions{}, applicationqianchuan.RemoveWorksCommand{}, err
	}
	if len(options.workURLs) == 0 {
		return qianchuanRemoveOptions{}, applicationqianchuan.RemoveWorksCommand{}, errors.New("at least one work URL is required")
	}
	if options.concurrency < 1 || options.concurrency > applicationworkmetadata.MaxConcurrency {
		return qianchuanRemoveOptions{}, applicationqianchuan.RemoveWorksCommand{}, errors.New("concurrency must be between 1 and 10")
	}
	return options, applicationqianchuan.RemoveWorksCommand{
		AdvertiserID: options.advertiserID, AuthAccountID: options.authAccountID,
		AdID: options.adID, WorkURLs: append([]string(nil), options.workURLs...),
		Concurrency: options.concurrency, Submit: options.submit, ConfirmDelete: options.confirmDelete,
	}, nil
}

func parseQianchuanMutationOptions(action string, args []string) (qianchuanMutationOptions, applicationqianchuan.MutationCommand, error) {
	options := qianchuanMutationOptions{}
	flags := flag.NewFlagSet("qc-plans "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.StringVar(&options.advertiserID, "advertiser-id", "", "")
	flags.StringVar(&options.authAccountID, "auth-account-id", "", "")
	flags.Var(&options.adIDs, "ad-id", "")
	flags.StringVar(&options.status, "status", "", "")
	flags.StringVar(&options.value, "value", "", "")
	flags.StringVar(&options.deepExternalAction, "deep-external-action", "", "")
	flags.BoolVar(&options.confirmDelete, "confirm-delete", false, "")
	flags.BoolVar(&options.submit, "submit", false, "")
	flags.StringVar(&options.out, "out", "", "")
	if err := flags.Parse(args); err != nil {
		return qianchuanMutationOptions{}, applicationqianchuan.MutationCommand{}, err
	}
	if len(flags.Args()) != 0 {
		return qianchuanMutationOptions{}, applicationqianchuan.MutationCommand{}, errors.New("unexpected positional Qianchuan mutation arguments")
	}
	options.trim()
	if err := validateCLIPositiveID(options.advertiserID, "advertiser_id", true); err != nil {
		return qianchuanMutationOptions{}, applicationqianchuan.MutationCommand{}, err
	}
	options.adIDs = splitRepeatedCSV(options.adIDs)
	command := applicationqianchuan.MutationCommand{
		AdvertiserID: options.advertiserID, AuthAccountID: options.authAccountID,
		AdIDs: append([]string(nil), options.adIDs...), Status: options.status, Value: options.value,
		DeepExternalAction: options.deepExternalAction, Submit: options.submit,
		ConfirmDelete: options.confirmDelete,
	}
	switch action {
	case "update-status":
		command.Kind = portqianchuan.MutationStatus
	case "update-budget":
		command.Kind = portqianchuan.MutationBudget
	case "update-roi":
		command.Kind = portqianchuan.MutationROI
	default:
		return qianchuanMutationOptions{}, applicationqianchuan.MutationCommand{}, errors.New("unsupported Qianchuan mutation action")
	}
	return options, command, nil
}

func (runner Runner) runQianchuanPlan(
	ctx context.Context,
	domainName string,
	action string,
	args []string,
	cwd string,
	userHome string,
	stateRoot string,
	getenv func(string) string,
	credentialsStore authapplication.CredentialStore,
	stdin io.Reader,
	stdout io.Writer,
) int {
	switch domainName {
	case "plans":
		switch action {
		case "create-qianchuan":
			return runner.runQianchuanCreatePlan(ctx, args, cwd, userHome, stateRoot, getenv, credentialsStore, stdin, stdout)
		case "batch-qianchuan-works":
			return runner.runQianchuanBatchWorks(ctx, args, cwd, userHome, stateRoot, getenv, credentialsStore, stdout)
		case "remove-qianchuan-work":
			return runner.runQianchuanRemoveWorks(ctx, args, cwd, userHome, stateRoot, getenv, credentialsStore, stdout)
		}
	case "qc-plans":
		return runner.runQianchuanMutation(ctx, action, args, stateRoot, credentialsStore, stdout)
	}
	WriteDomainError(stdout, domain.NewError("invalid_arguments", "unsupported Qianchuan plan command", 2, nil))
	return 2
}

func (runner Runner) runQianchuanCreatePlan(
	ctx context.Context,
	args []string,
	cwd string,
	userHome string,
	stateRoot string,
	getenv func(string) string,
	credentialsStore authapplication.CredentialStore,
	stdin io.Reader,
	stdout io.Writer,
) int {
	options, command, err := parseQianchuanCreateOptions(args, stdin)
	if err != nil {
		return writeQianchuanInvalidArguments(stdout, err)
	}
	configPath, err := resolveQianchuanConfigPath(options.configPath, cwd, userHome, getenv)
	if err != nil {
		return writeQianchuanPlanError(stdout, err)
	}
	command.ConfigPath = configPath
	service, err := runner.qianchuanCommandService(configPath, stateRoot, credentialsStore, command.Submit, false)
	if err != nil {
		return writeQianchuanPlanError(stdout, err)
	}
	result, executionErr := service.CreatePlan(ctx, command)
	if result.Mode != "" {
		if err := WriteJSONDestination(stdout, result, options.out); err != nil {
			return writeQianchuanOutputError(stdout, err)
		}
		if executionErr != nil || result.ExitCode != 0 {
			return 1
		}
		return 0
	}
	if executionErr != nil {
		return writeQianchuanPlanError(stdout, executionErr)
	}
	return writeQianchuanPlanError(stdout, errors.New("Qianchuan create service returned an empty result"))
}

func (runner Runner) runQianchuanBatchWorks(
	ctx context.Context,
	args []string,
	cwd string,
	userHome string,
	stateRoot string,
	getenv func(string) string,
	credentialsStore authapplication.CredentialStore,
	stdout io.Writer,
) int {
	options, command, err := parseQianchuanBatchOptions(args)
	if err != nil {
		return writeQianchuanInvalidArguments(stdout, err)
	}
	configPath, err := resolveQianchuanConfigPath(options.configPath, cwd, userHome, getenv)
	if err != nil {
		return writeQianchuanPlanError(stdout, err)
	}
	service, err := runner.qianchuanCommandService(configPath, stateRoot, credentialsStore, command.Submit, true)
	if err != nil {
		return writeQianchuanPlanError(stdout, err)
	}
	result, executionErr := service.BatchWorks(ctx, command)
	if result.Mode != "" {
		if err := WriteJSONDestination(stdout, result, options.out); err != nil {
			return writeQianchuanOutputError(stdout, err)
		}
		if executionErr != nil || result.ExitCode != 0 {
			return 1
		}
		return 0
	}
	if executionErr != nil {
		return writeQianchuanPlanError(stdout, executionErr)
	}
	return writeQianchuanPlanError(stdout, errors.New("Qianchuan batch service returned an empty result"))
}

func (runner Runner) runQianchuanRemoveWorks(
	ctx context.Context,
	args []string,
	cwd string,
	userHome string,
	stateRoot string,
	getenv func(string) string,
	credentialsStore authapplication.CredentialStore,
	stdout io.Writer,
) int {
	options, command, err := parseQianchuanRemoveOptions(args)
	if err != nil {
		return writeQianchuanInvalidArguments(stdout, err)
	}
	configPath, err := resolveQianchuanConfigPath(options.configPath, cwd, userHome, getenv)
	if err != nil {
		return writeQianchuanPlanError(stdout, err)
	}
	service, err := runner.qianchuanCommandService(configPath, stateRoot, credentialsStore, command.Submit, true)
	if err != nil {
		return writeQianchuanPlanError(stdout, err)
	}
	result, executionErr := service.RemoveWorks(ctx, command)
	if result.Mode != "" {
		if err := WriteJSONDestination(stdout, result, options.out); err != nil {
			return writeQianchuanOutputError(stdout, err)
		}
		if executionErr != nil || result.ExitCode != 0 {
			return 1
		}
		return 0
	}
	if executionErr != nil {
		return writeQianchuanPlanError(stdout, executionErr)
	}
	return writeQianchuanPlanError(stdout, errors.New("Qianchuan removal service returned an empty result"))
}

func (runner Runner) runQianchuanMutation(
	ctx context.Context,
	action string,
	args []string,
	stateRoot string,
	credentialsStore authapplication.CredentialStore,
	stdout io.Writer,
) int {
	options, command, err := parseQianchuanMutationOptions(action, args)
	if err != nil {
		return writeQianchuanInvalidArguments(stdout, err)
	}
	service, err := runner.qianchuanMutationService(stateRoot, credentialsStore, command.Submit)
	if err != nil {
		return writeQianchuanPlanError(stdout, err)
	}
	result, executionErr := service.Execute(ctx, command)
	if result.Mode != "" {
		if err := WriteJSONDestination(stdout, result, options.out); err != nil {
			return writeQianchuanOutputError(stdout, err)
		}
		if executionErr != nil || result.ExitCode != 0 {
			return 1
		}
		return 0
	}
	if executionErr != nil {
		return writeQianchuanPlanError(stdout, executionErr)
	}
	return writeQianchuanPlanError(stdout, errors.New("Qianchuan mutation service returned an empty result"))
}

func (runner Runner) qianchuanCommandService(
	configPath string,
	stateRoot string,
	credentialsStore authapplication.CredentialStore,
	submit bool,
	onlineRead bool,
) (QianchuanPlanCommandService, error) {
	runtime := runner.QianchuanPlans
	if runtime.Commands != nil {
		return runtime.Commands, nil
	}
	service := applicationqianchuan.CommandService{
		Config: filesystem.ConfigStore{Path: configPath}, Links: runtime.Links, Now: runtime.Now,
	}
	if !onlineRead && !submit {
		return service, nil
	}
	components, err := runner.qianchuanPlanComponents(stateRoot, credentialsStore)
	if err != nil {
		return nil, err
	}
	service.Tokens = components.tokens
	if service.Links == nil {
		service.Links = applicationworkmetadata.Resolver{Links: adapterworkmetadata.DouyinRedirectResolver{Client: runtime.HTTPClient}}
	}
	service.MetadataLinks = func(endpoint string) (applicationqianchuan.WorkLinkResolver, error) {
		if _, err := domain.ValidateWorkMetadataEndpoint(endpoint); err != nil {
			return nil, err
		}
		return applicationworkmetadata.Resolver{Links: adapterworkmetadata.DouyinMetadataResolver{
			Endpoint: endpoint, Client: runtime.HTTPClient,
			Fallback: adapterworkmetadata.DouyinRedirectResolver{Client: runtime.HTTPClient},
		}}, nil
	}
	service.OwnerHints = runtime.OwnerHints
	if service.OwnerHints == nil {
		service.OwnerHints = filesystem.QianchuanOwnerHintCache{
			Path: filepath.Join(stateRoot, "cache", "qianchuan-work-owners.json"), Now: runtime.Now,
		}
	}
	service.Verifier = applicationqianchuan.WorkVerifier{Reader: components.reader}
	service.Locks = components.locker
	reconciler := applicationqianchuan.CurrentDayReconciler{Reader: components.reader, Now: runtime.Now}
	guard := sharedplans.GuardedExecutor{Credentials: components.credentials, Locks: components.locker, Now: runtime.Now}
	service.Create = applicationqianchuan.CreateExecutor{Guard: guard, Writer: components.writer, Reconciler: reconciler}
	service.Batch = applicationqianchuan.BatchService{
		Guard: guard, Reader: components.reader, Writer: components.writer,
		Reconciler: reconciler, Now: runtime.Now,
	}
	service.Remove = applicationqianchuan.RemoveExecutor{Guard: guard, Reader: components.reader, Writer: components.writer}
	return service, nil
}

func (runner Runner) qianchuanMutationService(
	stateRoot string,
	credentialsStore authapplication.CredentialStore,
	submit bool,
) (QianchuanPlanMutationService, error) {
	runtime := runner.QianchuanPlans
	if runtime.Mutations != nil {
		return runtime.Mutations, nil
	}
	if !submit {
		return applicationqianchuan.MutationExecutor{}, nil
	}
	components, err := runner.qianchuanPlanComponents(stateRoot, credentialsStore)
	if err != nil {
		return nil, err
	}
	return applicationqianchuan.MutationExecutor{
		Guard: sharedplans.GuardedExecutor{
			Credentials: components.credentials, Locks: components.locker, Now: runtime.Now,
		},
		Writer: components.writer, Reader: components.reader,
	}, nil
}

type qianchuanPlanComponents struct {
	reader      portqianchuan.Reader
	writer      portqianchuan.Writer
	tokens      authapplication.TokenProvider
	credentials sharedplans.CredentialProvider
	locker      sharedplans.AdvertiserLocker
}

func (runner Runner) qianchuanPlanComponents(
	stateRoot string,
	credentialsStore authapplication.CredentialStore,
) (qianchuanPlanComponents, error) {
	runtime := runner.QianchuanPlans
	factory := runtime.ClientFactory
	var err error
	if factory == nil && (runtime.Reader == nil || runtime.Writer == nil) {
		factory, err = oceanengine.NewClientFactory(oceanengine.FactoryOptions{
			SharedQianchuanControl: filesystem.QianchuanRequestController{Root: stateRoot},
		})
		if err != nil {
			return qianchuanPlanComponents{}, err
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
	tokens := runtime.Tokens
	if tokens == nil {
		tokens = &authapplication.TokenManager{
			Credentials: credentialsStore, Authorizations: authorizations,
			Locks: refreshLocker, OAuth: oauth, Now: runtime.Now,
		}
	}
	credentials := runtime.Credentials
	if credentials == nil {
		credentials = sharedplans.TokenCredentialProvider{Tokens: tokens}
	}
	locker := runtime.Locker
	if locker == nil {
		locker = filesystem.AdvertiserLockStore{Root: stateRoot}
	}
	reader := runtime.Reader
	if reader == nil {
		reader = oceanengine.QianchuanReadAdapter{Factory: factory, Retry: runtime.Retry}
	}
	writer := runtime.Writer
	if writer == nil {
		writer = oceanengine.QianchuanWriteAdapter{Factory: factory}
	}
	return qianchuanPlanComponents{
		reader: reader, writer: writer, tokens: tokens,
		credentials: credentials, locker: locker,
	}, nil
}

func resolveQianchuanConfigPath(explicit, cwd, userHome string, getenv func(string) string) (string, error) {
	path := filesystem.ResolveConfigPath(explicit, cwd, getenv, userHome)
	return absoluteLocalPath(path)
}

func readQianchuanPayload(path string, stdin io.Reader) (json.RawMessage, error) {
	var reader io.Reader
	var file *os.File
	if path == "-" {
		if stdin == nil {
			return nil, errors.New("stdin is required for --payload-file -")
		}
		reader = stdin
	} else {
		opened, err := os.Open(filepath.Clean(path))
		if err != nil {
			return nil, fmt.Errorf("read Qianchuan payload file: %w", err)
		}
		file, reader = opened, opened
		defer func() { _ = file.Close() }()
	}
	payload, err := io.ReadAll(io.LimitReader(reader, qianchuanPayloadLimit+1))
	if err != nil {
		return nil, fmt.Errorf("read Qianchuan payload: %w", err)
	}
	if int64(len(payload)) > qianchuanPayloadLimit {
		return nil, fmt.Errorf("Qianchuan payload exceeds %d bytes", qianchuanPayloadLimit)
	}
	return validateQianchuanPayload(payload)
}

func validateQianchuanPayload(payload []byte) (json.RawMessage, error) {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 || !json.Valid(payload) {
		return nil, errors.New("Qianchuan plan payload must be valid JSON")
	}
	return append(json.RawMessage(nil), payload...), nil
}

func (options *qianchuanCreateOptions) trim() {
	options.configPath = strings.TrimSpace(options.configPath)
	options.payloadFile = strings.TrimSpace(options.payloadFile)
	options.payloadJSON = strings.TrimSpace(options.payloadJSON)
	options.planTemplate = strings.TrimSpace(options.planTemplate)
	options.liveTemplate = strings.TrimSpace(options.liveTemplate)
	options.name = strings.TrimSpace(options.name)
	options.advertiserID = strings.TrimSpace(options.advertiserID)
	options.authAccountID = strings.TrimSpace(options.authAccountID)
	options.out = strings.TrimSpace(options.out)
}

func (options *qianchuanBatchOptions) trim() {
	options.configPath = strings.TrimSpace(options.configPath)
	options.planTemplate = strings.TrimSpace(options.planTemplate)
	options.authAccountID = strings.TrimSpace(options.authAccountID)
	options.out = strings.TrimSpace(options.out)
	for index := range options.workURLs {
		options.workURLs[index] = strings.TrimSpace(options.workURLs[index])
	}
}

func (options *qianchuanRemoveOptions) trim() {
	options.configPath = strings.TrimSpace(options.configPath)
	options.advertiserID = strings.TrimSpace(options.advertiserID)
	options.authAccountID = strings.TrimSpace(options.authAccountID)
	options.adID = strings.TrimSpace(options.adID)
	options.out = strings.TrimSpace(options.out)
	for index := range options.workURLs {
		options.workURLs[index] = strings.TrimSpace(options.workURLs[index])
	}
}

func (options *qianchuanMutationOptions) trim() {
	options.configPath = strings.TrimSpace(options.configPath)
	options.advertiserID = strings.TrimSpace(options.advertiserID)
	options.authAccountID = strings.TrimSpace(options.authAccountID)
	options.status = strings.TrimSpace(options.status)
	options.value = strings.TrimSpace(options.value)
	options.deepExternalAction = strings.TrimSpace(options.deepExternalAction)
	options.out = strings.TrimSpace(options.out)
}

func writeQianchuanInvalidArguments(stdout io.Writer, err error) int {
	WriteDomainError(stdout, domain.NewError("invalid_arguments", err.Error(), 2, nil))
	return 2
}

func writeQianchuanOutputError(stdout io.Writer, err error) int {
	WriteDomainError(stdout, domain.WrapError("configuration_error", "failed to write Qianchuan plan result", 2, err))
	return 2
}

func writeQianchuanPlanError(stdout io.Writer, err error) int {
	mapped := qianchuanPlanError(err)
	WriteDomainError(stdout, mapped)
	return mapped.ExitCode
}

func qianchuanPlanError(err error) *domain.Error {
	var domainErr *domain.Error
	if errors.As(err, &domainErr) {
		return domainErr
	}
	if errors.Is(err, context.Canceled) {
		return domain.NewError("interrupted", "operation interrupted", 130, nil)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return domain.NewError("timeout", "operation timed out", 1, nil)
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
	return domain.NewError("qianchuan_plan_failed", err.Error(), 1, nil)
}
