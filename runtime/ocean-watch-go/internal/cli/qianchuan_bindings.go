package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	applicationqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/plans/qianchuan"
)

type qianchuanBindingOptions struct {
	advertiserID     string
	authAccountID    string
	templateID       string
	creatorID        string
	creatorVisibleID string
	productIDs       repeatedValues
	planType         string
	business         string
	businessDate     string
	groupID          string
	adID             string
	submit           bool
	out              string
}

func parseQianchuanBindingOptions(
	action string,
	args []string,
) (qianchuanBindingOptions, applicationqianchuan.BindingAuditCommand, applicationqianchuan.BindPlanCommand, error) {
	options := qianchuanBindingOptions{}
	flags := flag.NewFlagSet("qc-plans "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.advertiserID, "advertiser-id", "", "")
	flags.StringVar(&options.authAccountID, "auth-account-id", "", "")
	flags.StringVar(&options.templateID, "template-id", "", "")
	flags.StringVar(&options.creatorID, "creator-id", "", "")
	flags.StringVar(&options.creatorVisibleID, "douyin-id", "", "")
	flags.Var(&options.productIDs, "product-id", "")
	flags.StringVar(&options.planType, "plan-type", "", "")
	flags.StringVar(&options.business, "business", "", "")
	flags.StringVar(&options.businessDate, "business-date", "", "")
	flags.StringVar(&options.out, "out", "", "")
	if action == "bind" {
		flags.StringVar(&options.groupID, "group-id", "", "")
		flags.StringVar(&options.adID, "ad-id", "", "")
		flags.BoolVar(&options.submit, "submit", false, "")
	} else if action != "binding-audit" {
		return qianchuanBindingOptions{}, applicationqianchuan.BindingAuditCommand{}, applicationqianchuan.BindPlanCommand{}, errors.New("unsupported Qianchuan binding action")
	}
	if err := flags.Parse(args); err != nil {
		return qianchuanBindingOptions{}, applicationqianchuan.BindingAuditCommand{}, applicationqianchuan.BindPlanCommand{}, err
	}
	if len(flags.Args()) != 0 {
		return qianchuanBindingOptions{}, applicationqianchuan.BindingAuditCommand{}, applicationqianchuan.BindPlanCommand{}, errors.New("unexpected positional Qianchuan binding arguments")
	}
	options.trim()
	options.productIDs = splitRepeatedCSV(options.productIDs)
	if err := validateCLIPositiveID(options.advertiserID, "advertiser_id", true); err != nil {
		return qianchuanBindingOptions{}, applicationqianchuan.BindingAuditCommand{}, applicationqianchuan.BindPlanCommand{}, err
	}
	if options.templateID == "" {
		return qianchuanBindingOptions{}, applicationqianchuan.BindingAuditCommand{}, applicationqianchuan.BindPlanCommand{}, errors.New("template_id is required")
	}
	if err := validateCLIPositiveID(options.creatorID, "creator_id", true); err != nil {
		return qianchuanBindingOptions{}, applicationqianchuan.BindingAuditCommand{}, applicationqianchuan.BindPlanCommand{}, err
	}
	if len(options.productIDs) == 0 {
		return qianchuanBindingOptions{}, applicationqianchuan.BindingAuditCommand{}, applicationqianchuan.BindPlanCommand{}, errors.New("at least one product_id is required")
	}
	for _, productID := range options.productIDs {
		if err := validateCLIPositiveID(productID, "product_id", true); err != nil {
			return qianchuanBindingOptions{}, applicationqianchuan.BindingAuditCommand{}, applicationqianchuan.BindPlanCommand{}, err
		}
	}
	if err := applicationqianchuan.ValidateBusinessDate(options.businessDate); err != nil {
		return qianchuanBindingOptions{}, applicationqianchuan.BindingAuditCommand{}, applicationqianchuan.BindPlanCommand{}, err
	}
	audit := applicationqianchuan.BindingAuditCommand{
		AdvertiserID: options.advertiserID, AuthAccountID: options.authAccountID,
		TemplateID: options.templateID, CreatorID: options.creatorID,
		CreatorVisibleID: options.creatorVisibleID, ProductIDs: append([]string(nil), options.productIDs...),
		PlanType: options.planType, Business: options.business, BusinessDate: options.businessDate,
	}
	bind := applicationqianchuan.BindPlanCommand{
		BindingAuditCommand: audit, GroupID: options.groupID, AdID: options.adID, Submit: options.submit,
	}
	if action == "bind" {
		if options.groupID == "" {
			return qianchuanBindingOptions{}, applicationqianchuan.BindingAuditCommand{}, applicationqianchuan.BindPlanCommand{}, errors.New("group_id is required")
		}
		if err := validateCLIPositiveID(options.adID, "ad_id", true); err != nil {
			return qianchuanBindingOptions{}, applicationqianchuan.BindingAuditCommand{}, applicationqianchuan.BindPlanCommand{}, err
		}
	}
	return options, audit, bind, nil
}

func (options *qianchuanBindingOptions) trim() {
	options.advertiserID = strings.TrimSpace(options.advertiserID)
	options.authAccountID = strings.TrimSpace(options.authAccountID)
	options.templateID = strings.TrimSpace(options.templateID)
	options.creatorID = strings.TrimSpace(options.creatorID)
	options.creatorVisibleID = strings.TrimSpace(options.creatorVisibleID)
	for index := range options.productIDs {
		options.productIDs[index] = strings.TrimSpace(options.productIDs[index])
	}
	options.planType = strings.TrimSpace(options.planType)
	options.business = strings.TrimSpace(options.business)
	options.businessDate = strings.TrimSpace(options.businessDate)
	options.groupID = strings.TrimSpace(options.groupID)
	options.adID = strings.TrimSpace(options.adID)
	options.out = strings.TrimSpace(options.out)
}

func (runner Runner) runQianchuanBinding(
	ctx context.Context,
	action string,
	args []string,
	stateRoot string,
	credentialsStore authapplication.CredentialStore,
	stdout io.Writer,
) int {
	options, auditCommand, bindCommand, err := parseQianchuanBindingOptions(action, args)
	if err != nil {
		return writeQianchuanInvalidArguments(stdout, err)
	}
	service, err := runner.qianchuanPlanBindingService(stateRoot, credentialsStore)
	if err != nil {
		return writeQianchuanPlanError(stdout, err)
	}
	var result any
	switch action {
	case "binding-audit":
		result, err = service.Audit(ctx, auditCommand)
	case "bind":
		result, err = service.Bind(ctx, bindCommand)
	}
	if err != nil {
		return writeQianchuanPlanError(stdout, err)
	}
	if err := WriteJSONDestination(stdout, result, options.out); err != nil {
		return writeQianchuanOutputError(stdout, err)
	}
	return 0
}

func (runner Runner) qianchuanPlanBindingService(
	stateRoot string,
	credentialsStore authapplication.CredentialStore,
) (QianchuanPlanBindingService, error) {
	runtime := runner.QianchuanPlans
	if runtime.PlanBindings != nil {
		return runtime.PlanBindings, nil
	}
	components, err := runner.qianchuanPlanComponents(stateRoot, credentialsStore, 0)
	if err != nil {
		return nil, err
	}
	bindings := runtime.Bindings
	if bindings == nil {
		bindings = filesystem.QianchuanPlanBindingStore{Root: stateRoot}
	}
	return applicationqianchuan.BindingMigrationService{
		Tokens: components.tokens,
		Reconciler: applicationqianchuan.CurrentDayReconciler{
			Reader: components.reader, Bindings: bindings, Now: runtime.Now,
		},
		Bindings: bindings, Locks: components.locker, Now: runtime.Now,
	}, nil
}
