package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/adapters/filesystem"
	authapplication "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/auth"
	applicationmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/plans/marketing"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
)

const creatorBatchManifestLimit int64 = 4 << 20

type marketingCreatorBatchOptions struct {
	configPath     string
	jobsFile       string
	concurrency    int
	journal        string
	preflight      bool
	submit         bool
	includePayload bool
	out            string
	channel        string
	authAccountID  string
}

func parseMarketingCreatorBatchOptions(args []string) (marketingCreatorBatchOptions, error) {
	options := marketingCreatorBatchOptions{
		concurrency: applicationmarketing.CreatorBatchConcurrency, channel: "marketing",
	}
	flags := flag.NewFlagSet("plans batch-creator", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.StringVar(&options.jobsFile, "jobs-file", "", "")
	flags.IntVar(&options.concurrency, "concurrency", applicationmarketing.CreatorBatchConcurrency, "")
	flags.StringVar(&options.journal, "journal", "", "")
	flags.BoolVar(&options.preflight, "preflight", false, "")
	flags.BoolVar(&options.submit, "submit", false, "")
	flags.BoolVar(&options.includePayload, "include-payloads", false, "")
	flags.StringVar(&options.out, "out", "", "")
	flags.StringVar(&options.channel, "channel", "marketing", "")
	flags.StringVar(&options.authAccountID, "auth-account-id", "", "")
	if err := flags.Parse(args); err != nil {
		return marketingCreatorBatchOptions{}, err
	}
	if len(flags.Args()) != 0 {
		return marketingCreatorBatchOptions{}, errors.New("unexpected positional Marketing creator batch arguments")
	}
	options.configPath = strings.TrimSpace(options.configPath)
	options.jobsFile = strings.TrimSpace(options.jobsFile)
	options.journal = strings.TrimSpace(options.journal)
	options.out = strings.TrimSpace(options.out)
	options.channel = strings.TrimSpace(options.channel)
	options.authAccountID = strings.TrimSpace(options.authAccountID)
	if options.jobsFile == "" {
		return marketingCreatorBatchOptions{}, errors.New("--jobs-file is required")
	}
	if options.preflight && options.submit {
		return marketingCreatorBatchOptions{}, errors.New("--preflight and --submit cannot be used together")
	}
	if options.channel != "marketing" {
		return marketingCreatorBatchOptions{}, errors.New("Marketing creator batches only support --channel marketing")
	}
	if options.concurrency < 1 || options.concurrency > applicationmarketing.CreatorBatchMaxConcurrency {
		return marketingCreatorBatchOptions{}, fmt.Errorf(
			"concurrency must be between 1 and %d", applicationmarketing.CreatorBatchMaxConcurrency,
		)
	}
	return options, nil
}

func (runner Runner) runMarketingCreatorBatch(
	ctx context.Context,
	args []string,
	stateRoot string,
	credentialsStore authapplication.CredentialStore,
	stdout io.Writer,
) int {
	options, err := parseMarketingCreatorBatchOptions(args)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("invalid_arguments", err.Error(), 2, nil))
		return 2
	}
	runID, err := managedCreatorBatchRunID(options.journal, stateRoot, runner.userHome())
	if err != nil {
		WriteDomainError(stdout, domain.NewError("invalid_batch_journal", err.Error(), 2, nil))
		return 2
	}
	manifest, err := readCreatorBatchManifest(options.jobsFile, runner.userHome())
	if err != nil {
		WriteDomainError(stdout, domain.NewError("invalid_batch_manifest", err.Error(), 2, nil))
		return 2
	}
	request := applicationmarketing.CreatorBatchRequest{
		ManifestPayload: manifest, Channel: options.channel,
		AuthAccountID: options.authAccountID, RunID: runID,
		Preflight: options.preflight, Submit: options.submit,
		IncludePayloads: options.includePayload, MaxConcurrency: options.concurrency,
	}
	service := runner.MarketingPlans.CreatorBatchService
	if service == nil {
		configPath := runner.resolveMarketingPlanConfigPath(options.configPath)
		configReader := applicationmarketing.ConfigReader(filesystem.ConfigStore{Path: configPath})
		if runner.MarketingPlans.ConfigFactory != nil {
			configReader = runner.MarketingPlans.ConfigFactory(configPath)
		}
		components, buildErr := runner.buildMarketingCreateComponents(
			configReader, true, stateRoot, credentialsStore,
		)
		if buildErr != nil {
			mapped := marketingPlanError(buildErr)
			WriteDomainError(stdout, mapped)
			return mapped.ExitCode
		}
		service = applicationmarketing.CreatorBatchService{
			Preparer: components.Preparer, Executor: components.Executor,
			Journals: filesystem.OperationJournalStore{Root: stateRoot},
			Now:      runner.MarketingPlans.Now,
		}
	}
	result, executionErr := service.Execute(ctx, request)
	if result.Mode != "" {
		if result.JournalUsed && result.RunID != "" {
			path, pathErr := (filesystem.OperationJournalStore{Root: stateRoot}).ManagedPath(result.RunID)
			if pathErr != nil {
				WriteDomainError(stdout, domain.NewError("invalid_batch_journal", pathErr.Error(), 2, nil))
				return 2
			}
			result.Journal = &path
		}
		if err := WriteJSONDestination(stdout, result, options.out); err != nil {
			WriteDomainError(stdout, domain.WrapError(
				"configuration_error", "failed to write Marketing creator batch result", 2, err,
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
		"marketing_plan_write_failed", "Marketing creator batch service returned an empty result", 1, nil,
	))
	return 1
}

func managedCreatorBatchRunID(explicit, stateRoot, userHome string) (string, error) {
	explicit = strings.TrimSpace(explicit)
	if explicit == "" {
		return "", nil
	}
	path := expandCLIHome(explicit, userHome)
	return (filesystem.OperationJournalStore{Root: stateRoot}).RunIDFromManagedPath(path)
}

func readCreatorBatchManifest(path, userHome string) ([]byte, error) {
	path = expandCLIHome(strings.TrimSpace(path), userHome)
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read jobs file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat jobs file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("jobs file must be a regular file")
	}
	if info.Size() > creatorBatchManifestLimit {
		return nil, fmt.Errorf("jobs file exceeds %d bytes", creatorBatchManifestLimit)
	}
	payload, err := io.ReadAll(io.LimitReader(file, creatorBatchManifestLimit+1))
	if err != nil {
		return nil, fmt.Errorf("read jobs file: %w", err)
	}
	if int64(len(payload)) > creatorBatchManifestLimit {
		return nil, fmt.Errorf("jobs file exceeds %d bytes", creatorBatchManifestLimit)
	}
	return payload, nil
}

func (runner Runner) userHome() string {
	if runner.UserHome != "" {
		return runner.UserHome
	}
	home, _ := os.UserHomeDir()
	return home
}

func expandCLIHome(path, userHome string) string {
	if path == "~" {
		return userHome
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(userHome, path[2:])
	}
	return path
}
