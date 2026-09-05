package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"strconv"
	"strings"

	applicationtemplates "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/templates"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domaintemplates "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/templates"
)

func parseQianchuanTemplateOptions(action string, args []string) (string, error) {
	switch action {
	case "list", "create", "list-live", "create-live", "update":
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

type qianchuanTemplateUpdateArgs struct {
	selector           string
	roi2Goal           *float64
	budget             *float64
	smartBidType       *string
	qcpxMode           *string
	videoScheduleType  *string
	deepExternalAction *string
	productName        *string
	productShortName   *string
	productIDs         []string
	planNameTemplate   *string
	displayName        *string
	status             *string
	submit             bool
}

func parseQianchuanTemplateUpdate(args []string) (qianchuanTemplateUpdateArgs, error) {
	flags := flag.NewFlagSet("qc-templates update", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var result qianchuanTemplateUpdateArgs
	var roi2GoalStr, budgetStr, productIDsStr string
	var smartBidType, qcpxMode, videoScheduleType, deepExternalAction string
	var productName, productShortName, planNameTemplate, displayName, status string

	flags.StringVar(&result.selector, "selector", "", "")
	flags.StringVar(&roi2GoalStr, "roi2-goal", "", "")
	flags.StringVar(&budgetStr, "budget", "", "")
	flags.StringVar(&smartBidType, "smart-bid-type", "", "")
	flags.StringVar(&qcpxMode, "qcpx-mode", "", "")
	flags.StringVar(&videoScheduleType, "video-schedule-type", "", "")
	flags.StringVar(&deepExternalAction, "deep-external-action", "", "")
	flags.StringVar(&productName, "product-name", "", "")
	flags.StringVar(&productShortName, "product-short-name", "", "")
	flags.StringVar(&productIDsStr, "product-ids", "", "")
	flags.StringVar(&planNameTemplate, "plan-name-template", "", "")
	flags.StringVar(&displayName, "display-name", "", "")
	flags.StringVar(&status, "status", "", "")
	flags.BoolVar(&result.submit, "submit", false, "")

	if err := flags.Parse(args); err != nil {
		return result, err
	}

	if len(flags.Args()) != 0 {
		return result, errors.New("unexpected positional arguments")
	}

	if result.selector == "" {
		return result, errors.New("selector is required")
	}

	if roi2GoalStr != "" {
		parsed, err := strconv.ParseFloat(roi2GoalStr, 64)
		if err != nil {
			return result, errors.New("roi2-goal must be a number")
		}
		result.roi2Goal = &parsed
	}

	if budgetStr != "" {
		parsed, err := strconv.ParseFloat(budgetStr, 64)
		if err != nil {
			return result, errors.New("budget must be a number")
		}
		result.budget = &parsed
	}

	if productIDsStr != "" {
		result.productIDs = strings.Split(productIDsStr, ",")
	}

	if smartBidType != "" {
		result.smartBidType = &smartBidType
	}
	if qcpxMode != "" {
		result.qcpxMode = &qcpxMode
	}
	if videoScheduleType != "" {
		result.videoScheduleType = &videoScheduleType
	}
	if deepExternalAction != "" {
		result.deepExternalAction = &deepExternalAction
	}
	if productName != "" {
		result.productName = &productName
	}
	if productShortName != "" {
		result.productShortName = &productShortName
	}
	if planNameTemplate != "" {
		result.planNameTemplate = &planNameTemplate
	}
	if displayName != "" {
		result.displayName = &displayName
	}
	if status != "" {
		result.status = &status
	}

	return result, nil
}


func RunQianchuanTemplates(
	ctx context.Context,
	action string,
	args []string,
	store applicationtemplates.ConfigStore,
	resolvedPath string,
	stdout io.Writer,
) int {
	if action == "update" {
		updateArgs, err := parseQianchuanTemplateUpdate(args)
		if err != nil {
			WriteDomainError(stdout, domain.NewError("configuration_error", err.Error(), 2, nil))
			return 2
		}
		lifecycle := applicationtemplates.Lifecycle{Store: store, Path: resolvedPath}
		update := domaintemplates.QianchuanProductTemplateUpdate{
			ROI2Goal:           updateArgs.roi2Goal,
			Budget:             updateArgs.budget,
			SmartBidType:       updateArgs.smartBidType,
			QcpxMode:           updateArgs.qcpxMode,
			VideoScheduleType:  updateArgs.videoScheduleType,
			DeepExternalAction: updateArgs.deepExternalAction,
			ProductName:        updateArgs.productName,
			ProductShortName:   updateArgs.productShortName,
			ProductIDs:         updateArgs.productIDs,
			PlanNameTemplate:   updateArgs.planNameTemplate,
			DisplayName:        updateArgs.displayName,
			Status:             updateArgs.status,
		}
		result, err := lifecycle.UpdateQianchuanProductTemplate(ctx, updateArgs.selector, update, updateArgs.submit)
		if err != nil {
			WriteDomainError(stdout, templateError(err))
			return 2
		}
		if err := WriteJSON(stdout, result); err != nil {
			WriteDomainError(stdout, domain.WrapError("configuration_error", "failed to write update result", 2, err))
			return 2
		}
		return 0
	}

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
	case "list-live":
		result, err = lifecycle.ListQianchuanLive(ctx)
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
