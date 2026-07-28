package cli

import (
	"context"
	"errors"
	"flag"
	"io"

	applicationtemplates "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/templates"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
)

func parseQianchuanTemplateOptions(action string, args []string) (string, error) {
	switch action {
	case "list", "create", "migrate", "list-live", "create-live", "migrate-live":
	default:
		return "", errors.New("unsupported Qianchuan template action")
	}
	flags := flag.NewFlagSet("qc-templates "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "", "")
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	if len(flags.Args()) != 0 {
		return "", errors.New("unexpected positional Qianchuan template arguments")
	}
	return *configPath, nil
}

func RunQianchuanTemplates(
	ctx context.Context,
	action string,
	args []string,
	store applicationtemplates.ConfigStore,
	resolvedPath string,
	stdout io.Writer,
) int {
	if _, err := parseQianchuanTemplateOptions(action, args); err != nil {
		WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
		return 2
	}
	lifecycle := applicationtemplates.Lifecycle{Store: store, Path: resolvedPath}
	var result map[string]any
	var err error
	switch action {
	case "list":
		result, err = lifecycle.ListQianchuanProduct(ctx)
	case "migrate":
		result, err = lifecycle.MigrateQianchuanProduct(ctx)
	case "list-live":
		result, err = lifecycle.ListQianchuanLive(ctx)
	case "migrate-live":
		result, err = lifecycle.MigrateQianchuanLive(ctx)
	default:
		err = errors.New("unsupported Qianchuan template action")
	}
	if err != nil {
		WriteDomainError(stdout, templateError(err))
		return 2
	}
	if err := WriteJSON(stdout, result); err != nil {
		WriteDomainError(stdout, domain.WrapError("configuration_error", "failed to write Qianchuan template result", 2, err))
		return 2
	}
	return 0
}
