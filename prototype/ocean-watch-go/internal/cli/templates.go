package cli

import (
	"context"
	"errors"
	"flag"
	"io"

	applicationtemplates "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/templates"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	domaintemplates "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/templates"
)

type repeatedStrings []string

func (values *repeatedStrings) String() string { return "" }

func (values *repeatedStrings) Set(value string) error {
	*values = append(*values, value)
	return nil
}

type templateOptions struct {
	configPath                   string
	channel                      string
	selector                     string
	includeDetails               bool
	force                        bool
	submit                       bool
	out                          string
	titles                       repeatedStrings
	fromTemplate                 string
	materialSourceType           string
	templateType                 string
	confirmRemoveLegacyMaterials bool
}

func parseTemplateOptions(action string, args []string) (templateOptions, error) {
	options := templateOptions{}
	flags := flag.NewFlagSet("templates "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	switch action {
	case "create":
		flags.StringVar(&options.channel, "channel", "", "")
		flags.StringVar(&options.materialSourceType, "material-source-type", "", "")
		flags.StringVar(&options.templateType, "template-type", "", "")
	case "list":
		flags.StringVar(&options.channel, "channel", "all", "")
		flags.BoolVar(&options.includeDetails, "include-details", false, "")
	case "show":
		flags.StringVar(&options.channel, "channel", "", "")
		flags.StringVar(&options.selector, "template", "", "")
	case "migrate", "set-copy":
		flags.StringVar(&options.materialSourceType, "material-source-type", "", "")
		flags.BoolVar(&options.confirmRemoveLegacyMaterials, "confirm-remove-legacy-materials", false, "")
		flags.StringVar(&options.selector, "template", "", "")
		flags.Var(&options.titles, "title", "")
		flags.StringVar(&options.fromTemplate, "from-template", "", "")
	case "validate", "delete":
		flags.StringVar(&options.channel, "channel", "", "")
		flags.StringVar(&options.selector, "template", "", "")
		flags.BoolVar(&options.force, "force", false, "")
		flags.BoolVar(&options.submit, "submit", false, "")
		flags.StringVar(&options.out, "out", "", "")
	default:
		return templateOptions{}, errors.New("unsupported template action")
	}
	if err := flags.Parse(args); err != nil {
		return templateOptions{}, err
	}
	if len(flags.Args()) != 0 {
		return templateOptions{}, errors.New("unexpected positional template arguments")
	}
	switch action {
	case "create":
		if options.channel != "" && options.channel != "marketing" && options.channel != "qianchuan" {
			return templateOptions{}, errors.New("--channel must be marketing or qianchuan")
		}
		if options.materialSourceType != "" && options.materialSourceType != "ACCOUNT_UPLOAD" && options.materialSourceType != "CREATOR_AUTHORIZED" {
			return templateOptions{}, errors.New("--material-source-type must be ACCOUNT_UPLOAD or CREATOR_AUTHORIZED")
		}
		if options.templateType != "" && options.templateType != "product" && options.templateType != "live" {
			return templateOptions{}, errors.New("--template-type must be product or live")
		}
		if options.channel == "qianchuan" && options.materialSourceType != "" {
			return templateOptions{}, errors.New("--material-source-type is only valid for the marketing channel")
		}
		if options.channel == "marketing" && options.templateType != "" {
			return templateOptions{}, errors.New("--template-type is only valid for the qianchuan channel")
		}
	case "list":
		if options.channel != "all" && options.channel != "marketing" && options.channel != "qianchuan" {
			return templateOptions{}, errors.New("--channel must be all, marketing, or qianchuan")
		}
	case "show":
		if options.channel != "marketing" && options.channel != "qianchuan" {
			return templateOptions{}, errors.New("--channel must be marketing or qianchuan")
		}
		if options.selector == "" {
			return templateOptions{}, errors.New("--template is required for show")
		}
	case "migrate":
		if options.materialSourceType != "" {
			return templateOptions{}, errors.New("--material-source-type is only valid for create-wizard")
		}
		if options.selector != "" || len(options.titles) != 0 || options.fromTemplate != "" {
			return templateOptions{}, errors.New("--template, --title, and --from-template are only valid for set-copy")
		}
	case "set-copy":
		if options.materialSourceType != "" {
			return templateOptions{}, errors.New("--material-source-type is only valid for create-wizard")
		}
		if options.confirmRemoveLegacyMaterials {
			return templateOptions{}, errors.New("--confirm-remove-legacy-materials is only valid for migrate")
		}
		if options.selector == "" {
			return templateOptions{}, errors.New("--template is required for set-copy")
		}
		if (len(options.titles) == 0) == (options.fromTemplate == "") {
			return templateOptions{}, errors.New("one of --title or --from-template is required for set-copy")
		}
	case "validate":
		if options.channel != "" && options.channel != "marketing" && options.channel != "qianchuan" {
			return templateOptions{}, errors.New("--channel must be marketing or qianchuan")
		}
		if options.force || options.submit {
			return templateOptions{}, errors.New("--force and --submit are only valid for delete")
		}
		if options.selector != "" && options.channel == "" {
			return templateOptions{}, errors.New("--channel is required when validating one template")
		}
	case "delete":
		if options.channel != "marketing" && options.channel != "qianchuan" {
			return templateOptions{}, errors.New("delete requires --channel and --template")
		}
		if options.selector == "" {
			return templateOptions{}, errors.New("delete requires --channel and --template")
		}
		if options.channel == "qianchuan" && options.force {
			return templateOptions{}, errors.New("--force is supported only for Marketing template references")
		}
	}
	return options, nil
}

func RunTemplates(
	ctx context.Context,
	action string,
	args []string,
	store applicationtemplates.ConfigStore,
	resolvedPath string,
	stdout io.Writer,
) int {
	options, err := parseTemplateOptions(action, args)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
		return 2
	}
	query := applicationtemplates.Query{Store: store, Path: resolvedPath}
	lifecycle := applicationtemplates.Lifecycle{Store: store, Path: resolvedPath}
	var result map[string]any
	switch action {
	case "list":
		result, err = query.List(ctx, options.channel, options.includeDetails)
	case "show":
		result, err = query.Show(ctx, options.channel, options.selector)
	case "validate":
		result, err = lifecycle.Validate(ctx, options.channel, options.selector)
	case "delete":
		result, err = lifecycle.Delete(ctx, options.channel, options.selector, options.force, options.submit)
	case "set-copy":
		result, err = lifecycle.SetCopy(ctx, options.selector, options.titles, options.fromTemplate)
	case "migrate":
		result, err = lifecycle.MigrateMarketing(ctx, options.confirmRemoveLegacyMaterials)
	default:
		err = errors.New("unsupported template action")
	}
	if err != nil {
		var legacyError *domaintemplates.LegacyMaterialError
		if errors.As(err, &legacyError) {
			_ = WriteJSON(stdout, map[string]any{
				"config":             resolvedPath,
				"command":            action,
				"changed":            false,
				"error_code":         "legacy_material_selection_requires_confirmation",
				"error":              legacyError.Error(),
				"affected_templates": legacyError.Templates,
				"required_flag":      "--confirm-remove-legacy-materials",
			})
			return 2
		}
		mapped := templateActionError(action, err)
		WriteDomainError(stdout, mapped)
		return mapped.ExitCode
	}
	if err := WriteJSONDestination(stdout, result, options.out); err != nil {
		WriteDomainError(stdout, domain.WrapError("configuration_error", "failed to write template result", 2, err))
		return 2
	}
	if action == "validate" && result["ok"] != true {
		return 1
	}
	return 0
}

func templateActionError(action string, err error) *domain.Error {
	if action == "migrate" || action == "set-copy" {
		return domain.NewError("unexpected_error", err.Error(), 1, nil)
	}
	return templateError(err)
}

func templateError(err error) *domain.Error {
	var templateErr *domaintemplates.Error
	if errors.As(err, &templateErr) {
		return domain.NewError("configuration_error", templateErr.Message, 2, templateErr.Details)
	}
	return domain.NewError("configuration_error", err.Error(), 2, nil)
}
