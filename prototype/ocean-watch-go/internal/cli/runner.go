package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/credentials"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/environment"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/python"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/onboarding"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/configuration"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/requestcontrol"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/resources"
)

var Version = "0.9.1"

type Runner struct {
	Routes             application.RouteManifest
	Fallback           application.Fallback
	Stdout             io.Writer
	Stderr             io.Writer
	Stdin              io.Reader
	Getenv             func(string) string
	Cwd                string
	UserHome           string
	Credentials        application.CredentialStore
	Authorizations     onboarding.AuthorizationReader
	PythonResolver     python.Resolver
	EnvironmentProbe   onboarding.EnvironmentProbe
	MCPProbe           onboarding.MCPStatusProbe
	AccountReports     AccountReportRuntime
	MarketingDiscovery MarketingDiscoveryRuntime
	MarketingMaterials MarketingMaterialRuntime
	MarketingReports   MarketingReportRuntime
	MarketingPlans     MarketingPlanRuntime
	QianchuanPlans     QianchuanPlanRuntime
	QianchuanReads     QianchuanReadRuntime
	QianchuanReports   QianchuanReportRuntime
	RequestObserver    func(string, requestcontrol.BudgetSnapshot, requestcontrol.MetricsSnapshot)
}

func (runner Runner) Execute(ctx context.Context, args []string) int {
	stdout := runner.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := runner.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	stdin := runner.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	getenv := runner.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	invocation, err := Parse(args)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("invalid_arguments", err.Error(), 2, nil))
		return 2
	}
	if invocation.Version {
		_, _ = fmt.Fprintf(stdout, "ocean-watch %s\n", Version)
		return 0
	}
	if len(invocation.PassthroughArguments) != 0 {
		return runner.Fallback.Run(ctx, invocation.PassthroughArguments, stdout, stderr)
	}
	runtime, ok := runner.Routes.RouteFor(invocation.Command.Name())
	if !ok {
		WriteDomainError(stdout, domain.NewError("runtime_route_missing", "command has no runtime route", 1, nil))
		return 1
	}
	fullArguments := append([]string{invocation.Command.Domain, invocation.Command.Action}, invocation.Arguments...)
	if runtime == application.RuntimePython || invocation.Help {
		return runner.Fallback.Run(ctx, fullArguments, stdout, stderr)
	}
	requestLimit, err := commandRequestLimit(invocation.Command)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("request_control_error", err.Error(), 1, nil))
		return 1
	}
	ctx, requestBudget, requestMetrics, err := requestcontrol.PrepareCommandContext(ctx, requestLimit)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("request_control_error", err.Error(), 1, nil))
		return 1
	}
	if runner.RequestObserver != nil {
		defer func() {
			runner.RequestObserver(
				invocation.Command.Name(), requestBudget.Snapshot(), requestMetrics.Snapshot(),
			)
		}()
	}
	cwd := runner.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	userHome := runner.UserHome
	if userHome == "" {
		userHome, _ = os.UserHomeDir()
	}
	codexRoot := filesystem.CodexHome(getenv, userHome)
	credentialStore := runner.Credentials
	if credentialStore == nil {
		credentialStore = credentials.Store{
			Root: filepath.Join(codexRoot, "ads-plan-monitor"), Getenv: getenv,
		}
	}
	authorizations := runner.Authorizations
	if authorizations == nil {
		authorizations = filesystem.AuthorizationStore{Root: filepath.Join(codexRoot, "ads-plan-monitor", "state")}
	}
	switch invocation.Command.Domain {
	case "setup":
		return runner.runSetup(
			ctx, invocation.Command.Action, invocation.Arguments,
			cwd, userHome, codexRoot, getenv, credentialStore, authorizations, stdout,
		)
	case "accounts":
		explicitConfig := argumentValue(invocation.Arguments, "--config")
		configPath := filesystem.ResolveConfigPath(explicitConfig, cwd, getenv, userHome)
		store := filesystem.AccountStore{Path: filepath.Clean(configPath)}
		if invocation.Command.Action == "report" {
			stateRoot := filepath.Join(codexRoot, "ads-plan-monitor", "state")
			return runner.runAccountReport(ctx, invocation.Arguments, store, stateRoot, credentialStore, stdout)
		}
		return RunAccounts(ctx, invocation.Command.Action, invocation.Arguments, store, stdout)
	case "auth":
		if invocation.Command.Action != "migrate" {
			WriteDomainError(stdout, domain.NewError("go_handler_missing", "Go route has no handler", 1, nil))
			return 1
		}
		explicitConfig := argumentValue(invocation.Arguments, "--config")
		configPath := filepath.Clean(filesystem.ResolveConfigPath(explicitConfig, cwd, getenv, userHome))
		resolvedPath, resolveErr := filepath.Abs(configPath)
		if resolveErr != nil {
			WriteDomainError(stdout, domain.NewError("unexpected_error", resolveErr.Error(), 1, nil))
			return 1
		}
		stateRoot := filepath.Join(codexRoot, "ads-plan-monitor", "state")
		migration := application.AuthorizationMigration{
			Config:         filesystem.ConfigStore{Path: configPath},
			Credentials:    credentialStore,
			Authorizations: filesystem.AuthorizationStore{Root: stateRoot},
			Journal:        filesystem.MigrationJournalStore{Root: stateRoot},
			ConfigPath:     resolvedPath,
		}
		return RunAuthorizationMigration(ctx, invocation.Arguments, migration, resolvedPath, stdout)
	case "runs":
		store := filesystem.RunStore{Root: filesystem.ResolveRunsRoot(getenv, userHome)}
		return RunRuns(ctx, invocation.Command.Action, invocation.Arguments, store, stdout)
	case "templates":
		explicitConfig := argumentValue(invocation.Arguments, "--config")
		configPath := filepath.Clean(filesystem.ResolveConfigPath(explicitConfig, cwd, getenv, userHome))
		store := filesystem.ConfigStore{Path: configPath}
		authorizations := filesystem.AuthorizationStore{Root: filepath.Join(filesystem.CodexHome(getenv, userHome), "ads-plan-monitor", "state")}
		return RunTemplatesInteractive(ctx, invocation.Command.Action, invocation.Arguments, store, authorizations, configPath, stdin, stdout)
	case "materials":
		stateRoot := filepath.Join(codexRoot, "ads-plan-monitor", "state")
		return runner.runMarketingMaterials(
			ctx, invocation.Command.Action, invocation.Arguments, cwd, userHome,
			stateRoot, getenv, credentialStore, stdout,
		)
	case "discover":
		stateRoot := filepath.Join(codexRoot, "ads-plan-monitor", "state")
		return runner.runMarketingDiscovery(
			ctx, invocation.Command.Action, invocation.Arguments, cwd, userHome,
			stateRoot, getenv, credentialStore, stdout,
		)
	case "reports":
		stateRoot := filepath.Join(codexRoot, "ads-plan-monitor", "state")
		return runner.runMarketingReport(
			ctx, invocation.Command.Action, invocation.Arguments, cwd, userHome,
			stateRoot, getenv, credentialStore, stdout,
		)
	case "plans":
		stateRoot := filepath.Join(codexRoot, "ads-plan-monitor", "state")
		if isQianchuanPlanAction(invocation.Command.Action) {
			return runner.runQianchuanPlan(
				ctx, invocation.Command.Domain, invocation.Command.Action, invocation.Arguments,
				cwd, userHome, stateRoot, getenv, credentialStore, stdin, stdout,
			)
		}
		return runner.runMarketingPlan(
			ctx, invocation.Command.Action, invocation.Arguments,
			stateRoot, credentialStore, stdout,
		)
	case "qc-templates":
		explicitConfig := argumentValue(invocation.Arguments, "--config")
		configPath := filepath.Clean(filesystem.ResolveConfigPath(explicitConfig, cwd, getenv, userHome))
		store := filesystem.ConfigStore{Path: configPath}
		authorizations := filesystem.AuthorizationStore{Root: filepath.Join(filesystem.CodexHome(getenv, userHome), "ads-plan-monitor", "state")}
		return RunQianchuanTemplatesInteractive(ctx, invocation.Command.Action, invocation.Arguments, store, authorizations, configPath, stdin, stdout)
	case "qc-materials", "qc-products", "qc-plans":
		if invocation.Command.Domain == "qc-plans" && isQianchuanMutationAction(invocation.Command.Action) {
			stateRoot := filepath.Join(codexRoot, "ads-plan-monitor", "state")
			return runner.runQianchuanPlan(
				ctx, invocation.Command.Domain, invocation.Command.Action, invocation.Arguments,
				cwd, userHome, stateRoot, getenv, credentialStore, stdin, stdout,
			)
		}
		if invocation.Command.Domain == "qc-materials" && invocation.Command.Action == "inspect-work" {
			WriteDomainError(stdout, domain.NewError("go_handler_missing", "Go route has no handler", 1, nil))
			return 1
		}
		explicitConfig := argumentValue(invocation.Arguments, "--config")
		configPath := filesystem.ResolveConfigPath(explicitConfig, cwd, getenv, userHome)
		stateRoot := filepath.Join(codexRoot, "ads-plan-monitor", "state")
		return runner.runQianchuanRead(
			ctx, invocation.Command.Domain, invocation.Command.Action, invocation.Arguments,
			configPath, stateRoot, credentialStore, stdout,
		)
	case "qc-reports":
		stateRoot := filepath.Join(codexRoot, "ads-plan-monitor", "state")
		return runner.runQianchuanReport(
			ctx, invocation.Command.Action, invocation.Arguments, stateRoot, credentialStore, stdout,
		)
	default:
		WriteDomainError(stdout, domain.NewError("go_handler_missing", "Go route has no handler", 1, nil))
		return 1
	}
}

func isQianchuanPlanAction(action string) bool {
	switch action {
	case "create-qianchuan", "batch-qianchuan-works", "remove-qianchuan-work":
		return true
	default:
		return false
	}
}

func isQianchuanMutationAction(action string) bool {
	switch action {
	case "update-status", "update-budget", "update-roi":
		return true
	default:
		return false
	}
}

func (runner Runner) runSetup(
	ctx context.Context,
	action string,
	args []string,
	cwd string,
	userHome string,
	codexRoot string,
	getenv func(string) string,
	credentialStore onboarding.CredentialReader,
	authorizations onboarding.AuthorizationReader,
	stdout io.Writer,
) int {
	state := onboarding.LocalState{Credentials: credentialStore, Authorizations: authorizations}
	probe := runner.EnvironmentProbe
	if probe == nil {
		probe = environment.Probe{PythonResolver: runner.PythonResolver, Credentials: credentialStore}
	}
	doctor := onboarding.Doctor{Probe: probe}
	switch action {
	case "doctor":
		options, err := parseDoctorOptions(args)
		if err != nil {
			WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
			return 2
		}
		redirectURI := configuration.Channels[options.channel].RedirectURI
		configPath := filepath.Clean(filesystem.ResolveConfigPath(options.configPath, cwd, getenv, userHome))
		if raw, readErr := (filesystem.ConfigStore{Path: configPath}).Read(ctx); readErr == nil {
			if runtimeConfig, _, runtimeErr := configuration.Runtime(raw, options.channel, "oauth"); runtimeErr == nil {
				if configured := strings.TrimSpace(fmt.Sprint(configuration.Value(runtimeConfig, "oauth.redirect_uri"))); configured != "" && configured != "<nil>" {
					redirectURI = configured
				}
			}
		}
		return RunDoctor(ctx, args, doctor, redirectURI, stdout)
	case "init":
		options, err := parseInitializeOptions(args)
		if err != nil {
			WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
			return 2
		}
		configPath, err := absoluteLocalPath(filesystem.ResolveInitializationConfigPath(
			options.configPath, options.homeConfig, cwd, getenv, userHome,
		))
		if err != nil {
			WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
			return 2
		}
		template, err := resources.DefaultConfig()
		if err != nil {
			WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
			return 2
		}
		pluginRoot := filesystem.ResolvePluginRoot(cwd)
		skillRoot := resolveSkillRoot(pluginRoot, getenv)
		mcpProbe := runner.MCPProbe
		if mcpProbe == nil {
			mcpProbe = environment.MCPProbe{}
		}
		initializer := onboarding.Initializer{
			Store: filesystem.ConfigStore{Path: configPath}, State: state,
			Doctor: doctor, MCP: mcpProbe, ConfigPath: configPath,
			SkillRoot: skillRoot, Command: "ocean-watch",
		}
		return RunInitialize(ctx, args, initializer, template, stdout)
	case "validate":
		options, err := parseValidateOptions(args)
		if err != nil {
			WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
			return 2
		}
		configPath, err := absoluteLocalPath(filesystem.ResolveConfigPath(options.configPath, cwd, getenv, userHome))
		if err != nil {
			WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
			return 2
		}
		config, err := (filesystem.ConfigStore{Path: configPath}).Read(ctx)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				WriteDomainError(stdout, domain.NewError("configuration_error", "missing config: "+configPath, 2, nil))
				return 2
			}
			WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
			return 2
		}
		return RunValidate(ctx, args, onboarding.Validator{State: state}, configPath, config, stdout)
	case "work-metadata":
		options, err := parseWorkMetadataOptions(args)
		if err != nil {
			WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
			return 2
		}
		configPath := ""
		if options.homeConfig {
			configPath = filepath.Join(codexRoot, "ads-plan-monitor", "config.json")
		} else {
			configPath = filesystem.ResolveConfigPath(options.configPath, cwd, getenv, userHome)
		}
		configPath = filepath.Clean(configPath)
		resolvedPath, resolveErr := absoluteLocalPath(configPath)
		if resolveErr != nil {
			resolvedPath = configPath
		}
		store := filesystem.ConfigStore{Path: configPath}
		_, statErr := os.Stat(configPath)
		service := application.WorkMetadata{Store: store, Path: resolvedPath, RequestedPath: configPath}
		return RunWorkMetadata(ctx, args, service, statErr == nil, stdout)
	default:
		WriteDomainError(stdout, domain.NewError("go_handler_missing", "Go route has no handler", 1, nil))
		return 1
	}
}

func resolveSkillRoot(pluginRoot string, getenv func(string) string) string {
	if pluginRoot != "" {
		return filepath.Join(pluginRoot, "skills", "ads-plan-monitor")
	}
	entrypoint := strings.TrimSpace(getenv("OCEAN_WATCH_PYTHON_ENTRYPOINT"))
	if filepath.Base(entrypoint) == "run.py" {
		return filepath.Dir(entrypoint)
	}
	return ""
}

func absoluteLocalPath(path string) (string, error) {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	if canonical, canonicalErr := filepath.EvalSymlinks(filepath.Dir(absolute)); canonicalErr == nil {
		return filepath.Join(canonical, filepath.Base(absolute)), nil
	}
	return absolute, nil
}

func argumentValue(args []string, name string) string {
	for index, argument := range args {
		if argument == name && index+1 < len(args) {
			return args[index+1]
		}
		prefix := name + "="
		if len(argument) > len(prefix) && argument[:len(prefix)] == prefix {
			return argument[len(prefix):]
		}
	}
	return ""
}
