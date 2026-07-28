package onboarding

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/configuration"
	domaintemplates "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/templates"
)

var ValidationModes = map[string]bool{
	"query": true, "create-preview": true, "create-submit": true, "all": true,
}

type Validator struct {
	State LocalState
}

func (validator Validator) Validate(
	ctx context.Context,
	raw map[string]any,
	mode string,
	planTemplate string,
) (map[string]any, error) {
	if !ValidationModes[mode] {
		return nil, fmt.Errorf("unsupported validation mode: %s", mode)
	}
	channel := configuration.SelectedChannel(raw, "")
	capability := "query"
	if channel == "qianchuan" {
		capability = "qianchuan_materials"
	}
	runtimeConfig, _, err := configuration.Runtime(raw, channel, capability)
	if err != nil {
		return nil, err
	}
	advertiserID := configuredAdvertiserStatusID(runtimeConfig)
	snapshot, err := validator.State.Snapshot(ctx, channel, advertiserID, raw)
	if err != nil {
		return nil, err
	}
	api := configuration.Object(runtimeConfig["api"])
	for key, value := range snapshot.Runtime {
		api[key] = configuration.Clone(value)
	}
	runtimeConfig["api"] = api

	queryMissing := []string{}
	if configuration.ContainsUnresolved(configuration.Value(runtimeConfig, "api.base_url")) {
		queryMissing = append(queryMissing, "api.base_url")
	}
	if configuration.ContainsUnresolved(configuration.Value(runtimeConfig, "account.advertiser_id")) {
		queryMissing = append(queryMissing, "account.advertiser_id")
	}
	if configuration.Missing(snapshot.App["app_id"]) || configuration.Missing(snapshot.App["secret"]) {
		queryMissing = append(queryMissing, "local app_id and secret")
	}
	if configuration.Missing(snapshot.Runtime["access_token"]) && configuration.Missing(snapshot.Runtime["refresh_token"]) {
		queryMissing = append(queryMissing, "local access_token or refresh_token")
	}

	selectedTemplate := any(nil)
	if planTemplate != "" {
		selectedTemplate = planTemplate
	}
	previewMissing := []string{}
	submitMissing := []string{}
	templateError := ""
	if planTemplate == "" {
		templateError = "no business plan template selected; pass an explicit plan template"
	} else {
		previewMissing, submitMissing, templateError = validateMarketingTemplate(
			runtimeConfig,
			channel,
			planTemplate,
		)
	}
	if configuration.Missing(snapshot.App["app_id"]) || configuration.Missing(snapshot.App["secret"]) {
		submitMissing = append(submitMissing, "local app_id and secret")
	}
	if configuration.Missing(snapshot.Runtime["access_token"]) && configuration.Missing(snapshot.Runtime["refresh_token"]) {
		submitMissing = append(submitMissing, "local access_token or refresh_token")
	}
	previewMissing = uniqueOrdered(previewMissing)
	submitMissing = uniqueOrdered(submitMissing)
	readiness := map[string]bool{
		"query":          len(queryMissing) == 0,
		"create-preview": templateError == "" && len(previewMissing) == 0,
		"create-submit":  templateError == "" && len(submitMissing) == 0,
	}
	selectedReady := readiness[mode]
	if mode == "all" {
		selectedReady = readiness["query"] && readiness["create-preview"] && readiness["create-submit"]
	}
	var templateErrorValue any
	if templateError != "" {
		templateErrorValue = templateError
	}
	return map[string]any{
		"selected_plan_template":          selectedTemplate,
		"channel":                         channel,
		"plan_template_error":             templateErrorValue,
		"ok_for_query_data":               readiness["query"],
		"ok_for_create_payload_preview":   readiness["create-preview"],
		"ok_for_create_api_submission":    readiness["create-submit"],
		"missing_query_required":          anyStrings(queryMissing),
		"missing_create_preview_required": anyStrings(previewMissing),
		"missing_create_submit_required":  anyStrings(submitMissing),
		"readiness":                       readiness,
		"validation_mode":                 mode,
		"selected_mode_ready":             selectedReady,
		"credential_status":               snapshot.Status,
	}, nil
}

func validateMarketingTemplate(
	config map[string]any,
	channel string,
	selector string,
) ([]string, []string, string) {
	shown, err := domaintemplates.Show(config, "marketing", selector)
	if err != nil {
		available := sortedTemplateNames(config)
		return nil, nil, fmt.Sprintf("unknown plan template: %s; available: %s", selector, strings.Join(available, ", "))
	}
	candidate := configuration.Object(shown["template"])
	bindings := configuration.Object(candidate["bindings"])
	boundChannel := strings.TrimSpace(fmt.Sprint(bindings["channel"]))
	if boundChannel == "" || boundChannel == "<nil>" {
		boundChannel = "marketing"
	}
	if boundChannel != channel {
		return nil, nil, fmt.Sprintf("plan template %s is bound to channel %s, not channel %s", selector, boundChannel, channel)
	}
	requestedAdvertiser := configuration.Value(config, "account.advertiser_id")
	boundAdvertiser := bindings["advertiser_id"]
	if !configuration.Missing(boundAdvertiser) && !configuration.Missing(requestedAdvertiser) && fmt.Sprint(boundAdvertiser) != fmt.Sprint(requestedAdvertiser) {
		return nil, nil, fmt.Sprintf(
			"plan template %s is bound to advertiser %v, not advertiser %v",
			selector, boundAdvertiser, requestedAdvertiser,
		)
	}
	if bindingError := candidate["binding_error"]; bindingError != nil {
		return nil, nil, fmt.Sprint(bindingError)
	}
	readiness := domaintemplates.MarketingCandidateReadiness(config, selector, candidate)
	templateMissing := stringsFromAny(readiness["template_missing_fields"])
	runtimeMissing := stringsFromAny(readiness["runtime_missing_fields"])
	previewMissing := append([]string(nil), templateMissing...)
	for _, field := range runtimeMissing {
		if field != "api.access_token" {
			previewMissing = append(previewMissing, field)
		}
	}
	submitMissing := append(append([]string(nil), templateMissing...), runtimeMissing...)
	return previewMissing, submitMissing, ""
}

func configuredAdvertiserStatusID(config map[string]any) string {
	value := configuration.Value(config, "account.advertiser_id")
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func validConfiguredAdvertiserID(config map[string]any) string {
	text := configuredAdvertiserStatusID(config)
	if text == "" || configuration.Missing(text) {
		return ""
	}
	for _, character := range text {
		if character < '0' || character > '9' {
			return ""
		}
	}
	if strings.TrimLeft(text, "0") == "" {
		return ""
	}
	return text
}

func sortedTemplateNames(config map[string]any) []string {
	templates := configuration.Object(config["plan_templates"])
	names := make([]string, 0, len(templates))
	for name := range templates {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return []string{"<none>"}
	}
	return names
}

func stringsFromAny(value any) []string {
	values, _ := value.([]any)
	result := make([]string, 0, len(values))
	for _, item := range values {
		result = append(result, fmt.Sprint(item))
	}
	return result
}

func uniqueOrdered(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func anyStrings(values []string) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}
