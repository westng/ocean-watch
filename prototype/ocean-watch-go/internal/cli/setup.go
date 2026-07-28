package cli

import (
	"context"
	"errors"
	"flag"
	"io"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/onboarding"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/configuration"
)

type doctorOptions struct {
	configPath  string
	channel     string
	redirectURI string
	out         string
}

type initializeOptions struct {
	configPath string
	homeConfig bool
	force      bool
}

type validateOptions struct {
	configPath   string
	mode         string
	planTemplate string
}

type workMetadataOptions struct {
	configPath string
	homeConfig bool
	endpoint   string
	clear      bool
	out        string
}

func parseDoctorOptions(args []string) (doctorOptions, error) {
	options := doctorOptions{channel: "marketing"}
	flags := flag.NewFlagSet("setup doctor", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.StringVar(&options.channel, "channel", "marketing", "")
	flags.StringVar(&options.redirectURI, "redirect-uri", "", "")
	flags.StringVar(&options.out, "out", "", "")
	if err := flags.Parse(args); err != nil {
		return doctorOptions{}, err
	}
	if len(flags.Args()) != 0 {
		return doctorOptions{}, errors.New("unexpected positional setup arguments")
	}
	if options.channel != "marketing" && options.channel != "qianchuan" {
		return doctorOptions{}, errors.New("--channel must be marketing or qianchuan")
	}
	return options, nil
}

func parseInitializeOptions(args []string) (initializeOptions, error) {
	options := initializeOptions{}
	flags := flag.NewFlagSet("setup init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.BoolVar(&options.homeConfig, "home-config", false, "")
	flags.BoolVar(&options.force, "force", false, "")
	if err := flags.Parse(args); err != nil {
		return initializeOptions{}, err
	}
	if len(flags.Args()) != 0 {
		return initializeOptions{}, errors.New("unexpected positional setup arguments")
	}
	return options, nil
}

func parseValidateOptions(args []string) (validateOptions, error) {
	options := validateOptions{mode: "all"}
	flags := flag.NewFlagSet("setup validate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.StringVar(&options.mode, "mode", "all", "")
	flags.StringVar(&options.planTemplate, "plan-template", "", "")
	if err := flags.Parse(args); err != nil {
		return validateOptions{}, err
	}
	if len(flags.Args()) != 0 {
		return validateOptions{}, errors.New("unexpected positional setup arguments")
	}
	if !onboarding.ValidationModes[options.mode] {
		return validateOptions{}, errors.New("--mode must be query, create-preview, create-submit, or all")
	}
	return options, nil
}

func parseWorkMetadataOptions(args []string) (workMetadataOptions, error) {
	options := workMetadataOptions{}
	flags := flag.NewFlagSet("setup work-metadata", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.BoolVar(&options.homeConfig, "home-config", false, "")
	flags.StringVar(&options.endpoint, "endpoint", "", "")
	flags.BoolVar(&options.clear, "clear", false, "")
	flags.StringVar(&options.out, "out", "", "")
	if err := flags.Parse(args); err != nil {
		return workMetadataOptions{}, err
	}
	if len(flags.Args()) != 0 {
		return workMetadataOptions{}, errors.New("unexpected positional setup arguments")
	}
	if options.configPath != "" && options.homeConfig {
		return workMetadataOptions{}, errors.New("--config and --home-config cannot be used together")
	}
	if options.endpoint != "" && options.clear {
		return workMetadataOptions{}, errors.New("argument --clear: not allowed with argument --endpoint")
	}
	return options, nil
}

func RunWorkMetadata(
	ctx context.Context,
	args []string,
	service application.WorkMetadata,
	configExists bool,
	stdout io.Writer,
) int {
	options, err := parseWorkMetadataOptions(args)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
		return 2
	}
	if !configExists {
		configPath := service.RequestedPath
		if configPath == "" {
			configPath = service.Path
		}
		WriteDomainError(stdout, domain.NewError(
			"configuration_error",
			"local config does not exist; run setup init before configuring integrations",
			2,
			map[string]any{"config": configPath},
		))
		return 2
	}
	var result application.WorkMetadataStatus
	if options.endpoint != "" {
		result, err = service.Set(ctx, options.endpoint)
	} else if options.clear {
		result, err = service.Clear(ctx)
	} else {
		result, err = service.Status(ctx)
	}
	if err != nil {
		WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
		return 2
	}
	if err := WriteJSONDestination(stdout, result, options.out); err != nil {
		WriteDomainError(stdout, domain.WrapError("configuration_error", "failed to write output", 2, err))
		return 2
	}
	return 0
}

func RunDoctor(
	ctx context.Context,
	args []string,
	doctor onboarding.Doctor,
	redirectURI string,
	stdout io.Writer,
) int {
	options, err := parseDoctorOptions(args)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
		return 2
	}
	if options.redirectURI != "" {
		redirectURI = options.redirectURI
	}
	result := doctor.Report(ctx, options.channel, redirectURI)
	if err := WriteJSONDestination(stdout, result, options.out); err != nil {
		WriteDomainError(stdout, domain.WrapError("configuration_error", "failed to write environment report", 2, err))
		return 2
	}
	if !result.OK {
		return 1
	}
	return 0
}

func RunInitialize(
	ctx context.Context,
	args []string,
	initializer onboarding.Initializer,
	template map[string]any,
	stdout io.Writer,
) int {
	options, err := parseInitializeOptions(args)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
		return 2
	}
	result, err := initializer.Run(ctx, template, options.force)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
		return 2
	}
	if err := WriteJSON(stdout, result); err != nil {
		return 1
	}
	return 0
}

func RunValidate(
	ctx context.Context,
	args []string,
	validator onboarding.Validator,
	configPath string,
	config map[string]any,
	stdout io.Writer,
) int {
	options, err := parseValidateOptions(args)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
		return 2
	}
	result, err := validator.Validate(ctx, config, options.mode, options.planTemplate)
	if err != nil {
		var channelError *configuration.ChannelError
		if errors.As(err, &channelError) {
			_ = WriteJSON(stdout, map[string]any{
				"config": configPath, "validation_mode": options.mode,
				"selected_mode_ready": false, "channel": channelError.Channel,
				"error_code": channelError.Code, "error": channelError.Error(),
			})
			return 1
		}
		WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
		return 2
	}
	result["config"] = configPath
	if err := WriteJSON(stdout, result); err != nil {
		return 1
	}
	if result["selected_mode_ready"] != true {
		return 1
	}
	return 0
}
