package cli

import (
	"context"
	"errors"
	"flag"
	"io"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domaintemplates "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/templates"
)

type authorizationMigrator interface {
	Migrate(context.Context, bool) (map[string]any, error)
}

type authMigrationOptions struct {
	configPath                   string
	confirmRemoveLegacyMaterials bool
}

func parseAuthMigrationOptions(args []string) (authMigrationOptions, error) {
	options := authMigrationOptions{}
	flags := flag.NewFlagSet("auth migrate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.BoolVar(&options.confirmRemoveLegacyMaterials, "confirm-remove-legacy-materials", false, "")
	if err := flags.Parse(args); err != nil {
		return authMigrationOptions{}, err
	}
	if len(flags.Args()) != 0 {
		return authMigrationOptions{}, errors.New("unexpected positional authorization migration arguments")
	}
	return options, nil
}

func RunAuthorizationMigration(
	ctx context.Context,
	args []string,
	migration authorizationMigrator,
	resolvedPath string,
	stdout io.Writer,
) int {
	options, err := parseAuthMigrationOptions(args)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("invalid_arguments", err.Error(), 2, nil))
		return 2
	}
	result, err := migration.Migrate(ctx, options.confirmRemoveLegacyMaterials)
	if err != nil {
		var legacyError *domaintemplates.LegacyMaterialError
		if errors.As(err, &legacyError) {
			_ = WriteJSON(stdout, map[string]any{
				"config":             resolvedPath,
				"changed":            false,
				"error_code":         "legacy_material_selection_requires_confirmation",
				"error":              legacyError.Error(),
				"affected_templates": legacyError.Templates,
				"required_flag":      "--confirm-remove-legacy-materials",
			})
			return 2
		}
		WriteDomainError(stdout, domain.NewError("unexpected_error", err.Error(), 1, nil))
		return 1
	}
	if err := WriteJSON(stdout, result); err != nil {
		return 1
	}
	return 0
}
