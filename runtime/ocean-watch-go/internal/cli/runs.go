package cli

import (
	"context"
	"errors"
	"flag"
	"io"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type runOptions struct {
	runID string
	limit int
	out   string
}

func parseRunOptions(action string, args []string) (runOptions, error) {
	options := runOptions{}
	flags := flag.NewFlagSet("runs "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.runID, "run-id", "", "")
	flags.IntVar(&options.limit, "limit", 50, "")
	flags.StringVar(&options.out, "out", "", "")
	if err := flags.Parse(args); err != nil {
		return runOptions{}, err
	}
	if len(flags.Args()) != 0 {
		return runOptions{}, errors.New("unexpected positional run arguments")
	}
	if action == "show" && options.runID == "" {
		return runOptions{}, errors.New("--run-id is required for show")
	}
	if action == "list" && options.runID != "" {
		return runOptions{}, errors.New("--run-id is only valid for show")
	}
	return options, nil
}

func RunRuns(ctx context.Context, action string, args []string, store application.RunStore, stdout io.Writer) int {
	options, err := parseRunOptions(action, args)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
		return 2
	}
	var result any
	switch action {
	case "list":
		rows, err := store.List(ctx, options.limit)
		if err != nil {
			WriteDomainError(stdout, runError(err))
			return 2
		}
		result = RunListEnvelope{Mode: "run_history", RunCount: len(rows), Runs: rows}
	case "show":
		summary, journal, err := store.Show(ctx, options.runID)
		if err != nil {
			WriteDomainError(stdout, runError(err))
			return 2
		}
		result = RunDetailEnvelope{Mode: "run_detail", Summary: summary, Run: journal}
	default:
		WriteDomainError(stdout, domain.NewError("configuration_error", "unsupported run action", 2, nil))
		return 2
	}
	if err := WriteJSONDestination(stdout, result, options.out); err != nil {
		WriteDomainError(stdout, domain.WrapError("configuration_error", "failed to write output", 2, err))
		return 2
	}
	return 0
}

func runError(err error) *domain.Error {
	var notFound *filesystem.RunNotFoundError
	if errors.As(err, &notFound) {
		return domain.NewError("configuration_error", err.Error(), 2, map[string]any{"run_id": notFound.RunID})
	}
	var unreadable *filesystem.RunUnreadableError
	if errors.As(err, &unreadable) {
		return domain.NewError("configuration_error", err.Error(), 2, map[string]any{"run_id": unreadable.RunID})
	}
	var schema *filesystem.RunSchemaError
	if errors.As(err, &schema) {
		return domain.NewError("configuration_error", err.Error(), 2, map[string]any{"run_id": schema.RunID})
	}
	return domain.NewError("configuration_error", err.Error(), 2, nil)
}
