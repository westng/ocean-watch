package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"path/filepath"
	"strings"

	pythonruntime "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/python"
	metadataadapter "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/workmetadata"
	metadataapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/workmetadata"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type WorkMetadataService interface {
	Resolve(context.Context, metadataapplication.ResolveRequest) (metadataapplication.ResolveResult, error)
}

type WorkMetadataRuntime struct {
	Service    WorkMetadataService
	PluginRoot string
}

type inspectWorkOptions struct {
	configPath  string
	workURLs    repeatedValues
	concurrency int
	out         string
}

type inspectWorkEnvelope struct {
	OK                  bool                      `json:"ok"`
	Mode                string                    `json:"mode"`
	MetadataIntegration string                    `json:"metadata_integration"`
	InputCount          int                       `json:"input_count"`
	ResolvedCount       int                       `json:"resolved_count"`
	SkippedCount        int                       `json:"skipped_count"`
	Resolved            []domain.ResolvedWorkLink `json:"resolved"`
	Skipped             []domain.SkippedWorkLink  `json:"skipped"`
	MetadataPerformance map[string]any            `json:"metadata_performance,omitempty"`
	Config              string                    `json:"config,omitempty"`
}

func parseInspectWorkOptions(args []string) (inspectWorkOptions, error) {
	options := inspectWorkOptions{concurrency: 8}
	flags := flag.NewFlagSet("qc-materials inspect-work", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.Var(&options.workURLs, "work-url", "")
	flags.IntVar(&options.concurrency, "concurrency", 8, "")
	flags.StringVar(&options.out, "out", "", "")
	if err := flags.Parse(args); err != nil {
		return inspectWorkOptions{}, err
	}
	if len(flags.Args()) != 0 {
		return inspectWorkOptions{}, errors.New("unexpected positional work inspection arguments")
	}
	if len(options.workURLs) == 0 {
		return inspectWorkOptions{}, errors.New("at least one --work-url is required")
	}
	if options.concurrency < 1 || options.concurrency > metadataapplication.MaxConcurrency {
		return inspectWorkOptions{}, errors.New("concurrency must be between 1 and 10")
	}
	return options, nil
}

func RunInspectWork(
	ctx context.Context,
	args []string,
	service WorkMetadataService,
	configPath string,
	stdout io.Writer,
) int {
	options, err := parseInspectWorkOptions(args)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("invalid_arguments", err.Error(), 2, nil))
		return 2
	}
	if service == nil {
		WriteDomainError(stdout, domain.NewError("unexpected_error", "work metadata service is unavailable", 1, nil))
		return 1
	}
	result, err := service.Resolve(ctx, metadataapplication.ResolveRequest{
		URLs: append([]string(nil), options.workURLs...), Concurrency: options.concurrency,
	})
	if err != nil {
		var workErr *domain.WorkLinkError
		if errors.As(err, &workErr) {
			WriteDomainError(stdout, domain.NewError(workErr.Code, workErr.Message, 1, nil))
		} else {
			WriteDomainError(stdout, domain.NewError("work_inspection_failed", "作品解析失败", 1, nil))
		}
		return 1
	}
	envelope := inspectWorkEnvelope{
		OK: len(result.Skipped) == 0, Mode: "qianchuan_work_inspection",
		MetadataIntegration: "f2_cli", InputCount: len(options.workURLs),
		ResolvedCount: len(result.Resolved), SkippedCount: len(result.Skipped),
		Resolved: result.Resolved, Skipped: result.Skipped,
		MetadataPerformance: result.MetadataPerformance, Config: configPath,
	}
	if err := WriteJSONDestination(stdout, envelope, options.out); err != nil {
		WriteDomainError(stdout, domain.WrapError("configuration_error", "failed to write output", 2, err))
		return 2
	}
	if !envelope.OK {
		return 1
	}
	return 0
}

func (runner Runner) runInspectWork(
	ctx context.Context,
	args []string,
	configPath string,
	stdout io.Writer,
) int {
	runtime := runner.WorkMetadata
	service := runtime.Service
	if service == nil {
		pluginRoot := strings.TrimSpace(runtime.PluginRoot)
		if pluginRoot == "" {
			pluginRoot = strings.TrimSpace(runner.PluginRoot)
		}
		if pluginRoot != "" {
			pluginRoot = filepath.Clean(pluginRoot)
		}
		service = metadataapplication.Resolver{
			Links: metadataadapter.DouyinRedirectResolver{},
			Metadata: metadataadapter.F2Resolver{
				Python: runner.PythonResolver, Directory: pluginRoot,
				Entrypoint: filepath.Join(pluginRoot, "f2", "resolve.py"),
			},
		}
		if pluginRoot == "" {
			service = metadataapplication.Resolver{
				Links:    metadataadapter.DouyinRedirectResolver{},
				Metadata: metadataadapter.F2Resolver{Python: pythonruntime.Resolver{}},
			}
		}
	}
	return RunInspectWork(ctx, args, service, configPath, stdout)
}
