package qianchuan

import (
	"context"
	"errors"
	"sort"
	"strings"

	applicationworkmetadata "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/workmetadata"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	domaintemplates "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/templates"
)

// Every routed batch retains its own advertiser-bound snapshot. There is no
// cross-advertiser submit ID and routing itself can never dispatch a write.
type RoutedBatchResult struct {
	InputIndexes []int                `json:"input_indexes"`
	Template     BatchTemplateSummary `json:"template"`
	Status       string               `json:"status"`
	FailureStage string               `json:"failure_stage,omitempty"`
	Result       *BatchCommandResult  `json:"result,omitempty"`
	Cause        error                `json:"-"`
}

type RoutingCandidate struct {
	TemplateID   string   `json:"template_id"`
	AdvertiserID string   `json:"advertiser_id"`
	ProductIDs   []string `json:"product_ids"`
}

type RoutingPending struct {
	InputIndex int                `json:"input_index"`
	Reason     string             `json:"reason"`
	Candidates []RoutingCandidate `json:"candidates"`
}

func needsBatchRouting(command BatchWorksCommand) bool {
	if strings.TrimSpace(command.PlanTemplate) == "" {
		return true
	}
	for _, item := range command.Items {
		if item.PlanTemplate != "" || item.ProductID != "" || item.ProductName != "" || item.AdvertiserID != "" || item.AuthAccountID != "" || item.CreatorUID != "" || item.CreatorDouyinID != "" || item.CreatorName != "" {
			return true
		}
	}
	return false
}

type routedWorkBatch struct {
	template      domaintemplates.QianchuanPlanPayload
	authAccountID string
	items         []domainqianchuan.BatchItem
	indexes       []int
	prepared      map[string]domain.ResolvedWorkLink
	conflict      bool
}

func (service CommandService) routeBatchWorks(ctx context.Context, command BatchWorksCommand) (BatchCommandResult, error) {
	if command.Submit {
		return BatchCommandResult{}, errors.New("mixed work routing requires preflight; submit each confirmed child preflight_id separately")
	}
	if service.Config == nil {
		return BatchCommandResult{}, errors.New("Qianchuan batch configuration is required")
	}
	if command.Concurrency == 0 {
		command.Concurrency = DefaultBatchConcurrency
	}
	if command.Concurrency < 1 || command.Concurrency > 10 || len(command.Items) < 1 || len(command.Items) > 100 {
		return BatchCommandResult{}, errors.New("invalid routed batch size or concurrency")
	}
	command.Items = domainqianchuan.NormalizeBatchItems(command.Items)
	if err := validateBatchCommandItems(command.Items); err != nil {
		return BatchCommandResult{}, err
	}
	config, err := service.Config.Read(ctx)
	if err != nil {
		return BatchCommandResult{}, preflightStageError("configuration", err)
	}
	templates, err := domaintemplates.QianchuanProductPlanPayloads(config)
	if err != nil {
		return BatchCommandResult{}, preflightStageError("template", err)
	}
	sort.Slice(templates, func(i, j int) bool { return templates[i].TemplateID < templates[j].TemplateID })
	result := BatchCommandResult{Routed: true, Batches: []RoutedBatchResult{}, Pending: []RoutingPending{}, BatchResult: BatchResult{
		Mode: "dry_run", Channel: "qianchuan", Counts: map[string]int{"input_works": len(command.Items)},
	}}
	// Only rows without an explicit product/template need public metadata for
	// routing. One shared call resolves those rows, and children reuse its data.
	unknown := []domainqianchuan.BatchItem{}
	for _, item := range command.Items {
		if item.PlanTemplate == "" && command.PlanTemplate == "" && item.ProductID == "" && item.ProductName == "" {
			unknown = append(unknown, item)
		}
	}
	prepared := map[int]domain.ResolvedWorkLink{}
	metadataFailures := map[int]string{}
	if len(unknown) > 0 {
		if service.Links == nil {
			return BatchCommandResult{}, errors.New("Qianchuan work-link resolver is required")
		}
		metadata, resolveErr := service.Links.Resolve(ctx, applicationworkmetadata.ResolveRequest{URLs: batchItemURLs(unknown), Concurrency: command.Concurrency})
		if ctx.Err() != nil {
			return BatchCommandResult{}, ctx.Err()
		}
		if resolveErr != nil {
			reason := "product_metadata_unavailable"
			var metadataError *domain.WorkLinkError
			if errors.As(resolveErr, &metadataError) {
				reason = routingMetadataReason(metadataError.Code)
			}
			for _, item := range unknown {
				metadataFailures[item.InputIndex] = reason
			}
		} else {
			byWorkID := map[string]domain.ResolvedWorkLink{}
			for _, row := range metadata.Resolved {
				if row.InputIndex >= 0 && row.InputIndex < len(unknown) {
					prepared[unknown[row.InputIndex].InputIndex] = row
					byWorkID[row.AwemeItemID] = row
				}
			}
			for _, row := range metadata.Skipped {
				if row.InputIndex >= 0 && row.InputIndex < len(unknown) {
					// Keep duplicate rows until the child checks all row settings.
					// Otherwise a public-resolution dedupe could hide a conflict.
					if shared, exists := byWorkID[row.AwemeItemID]; exists && row.Reason == "duplicate_input" {
						shared.InputURL = row.InputURL
						prepared[unknown[row.InputIndex].InputIndex] = shared
						continue
					}
					metadataFailures[unknown[row.InputIndex].InputIndex] = routingMetadataReason(row.Reason)
				}
			}
		}
	}
	batches := []routedWorkBatch{}
	for _, item := range command.Items {
		if item.ProductID != "" && !validPositiveID(item.ProductID) || item.AdvertiserID != "" && !validPositiveID(item.AdvertiserID) || item.CreatorUID != "" && !validPositiveID(item.CreatorUID) {
			result.Pending = append(result.Pending, RoutingPending{InputIndex: item.InputIndex, Reason: "invalid_routing_identity", Candidates: []RoutingCandidate{}})
			continue
		}
		if item.CreatorUID != "" && item.CreatorDouyinID == "" {
			result.Pending = append(result.Pending, RoutingPending{InputIndex: item.InputIndex, Reason: "missing_creator_douyin_id", Candidates: []RoutingCandidate{}})
			continue
		}
		selector := item.PlanTemplate
		if selector == "" {
			selector = command.PlanTemplate
		}
		hints := []string{}
		if selector == "" && item.ProductID == "" && item.ProductName == "" {
			if row, ok := prepared[item.InputIndex]; ok {
				for _, product := range row.ProductHints {
					hints = append(hints, product.ProductID)
				}
				if len(hints) == 0 && row.ProductHint != nil {
					hints = append(hints, row.ProductHint.ProductID)
				}
			}
			if len(hints) == 0 {
				reason := metadataFailures[item.InputIndex]
				if reason == "" {
					reason = "missing_product_identity"
				}
				result.Pending = append(result.Pending, RoutingPending{InputIndex: item.InputIndex, Reason: reason, Candidates: []RoutingCandidate{}})
				continue
			}
			unique := map[string]struct{}{}
			for _, hint := range hints {
				if validPositiveID(hint) {
					unique[hint] = struct{}{}
				}
			}
			if len(unique) != 1 {
				reason := "multiple_work_products"
				if len(unique) == 0 {
					reason = "missing_product_identity"
				}
				result.Pending = append(result.Pending, RoutingPending{InputIndex: item.InputIndex, Reason: reason, Candidates: []RoutingCandidate{}})
				continue
			}
			hints = hints[:0]
			for product := range unique {
				hints = append(hints, product)
			}
		}
		candidates := []domaintemplates.QianchuanPlanPayload{}
		for _, template := range templates {
			if selector != "" && selector != template.TemplateID && selector != template.DisplayName {
				continue
			}
			if item.AdvertiserID != "" && item.AdvertiserID != template.AdvertiserID {
				continue
			}
			if item.ProductID != "" && !containsRouteProduct(template.ProductIDs, item.ProductID) {
				continue
			}
			if item.ProductName != "" && item.ProductName != template.ProductName && item.ProductName != template.ProductShortName {
				continue
			}
			if len(hints) > 0 && !overlapsRouteProducts(template.ProductIDs, hints) {
				continue
			}
			candidates = append(candidates, template)
		}
		if len(candidates) != 1 {
			pending := RoutingPending{InputIndex: item.InputIndex, Reason: "routing_not_found", Candidates: []RoutingCandidate{}}
			if len(candidates) > 1 {
				pending.Reason = "routing_ambiguous"
			}
			for _, candidate := range candidates {
				pending.Candidates = append(pending.Candidates, RoutingCandidate{TemplateID: candidate.TemplateID, AdvertiserID: candidate.AdvertiserID, ProductIDs: append([]string(nil), candidate.ProductIDs...)})
			}
			result.Pending = append(result.Pending, pending)
			continue
		}
		template := candidates[0]
		if item.ProductID == "" && len(hints) > 0 {
			item.ProductID = hints[0]
		}
		authAccountID := item.AuthAccountID
		if authAccountID == "" {
			authAccountID = command.AuthAccountID
		}
		batchIndex := -1
		for index, batch := range batches {
			if batch.template.TemplateID == template.TemplateID && batch.template.AdvertiserID == template.AdvertiserID {
				batchIndex = index
				break
			}
		}
		if batchIndex == -1 {
			batchIndex = len(batches)
			batches = append(batches, routedWorkBatch{template: template, authAccountID: authAccountID, prepared: map[string]domain.ResolvedWorkLink{}})
		}
		batch := &batches[batchIndex]
		if batch.authAccountID != authAccountID {
			batch.conflict = true
		}
		batch.indexes = append(batch.indexes, item.InputIndex)
		batch.items = append(batch.items, item)
		if row, ok := prepared[item.InputIndex]; ok {
			batch.prepared[item.WorkURL] = row
		}
	}
	// Serial child execution bounds reads across the whole operation and keeps
	// each credential, owner cache, snapshot and reconciliation advertiser scoped.
	for _, batch := range batches {
		if err := ctx.Err(); err != nil {
			return BatchCommandResult{}, err
		}
		if batch.conflict {
			for _, index := range batch.indexes {
				result.Pending = append(result.Pending, RoutingPending{InputIndex: index, Reason: "authorization_selector_conflict", Candidates: []RoutingCandidate{}})
			}
			continue
		}
		childService := service
		childService.Config = routeConfigSnapshot{config: config}
		childService.Links = &routedLinkResolver{base: service.Links, prepared: batch.prepared, items: batch.items}
		items := make([]domainqianchuan.BatchItem, len(batch.items))
		for index, item := range batch.items {
			items[index] = domainqianchuan.BatchItem{InputIndex: index, WorkURL: item.WorkURL, PlanType: item.PlanType, Business: item.Business}
		}
		child, childErr := childService.BatchWorks(ctx, BatchWorksCommand{PlanTemplate: batch.template.TemplateID, Items: items, Concurrency: command.Concurrency, AuthAccountID: batch.authAccountID, IncludePayloads: command.IncludePayloads})
		entry := RoutedBatchResult{InputIndexes: batch.indexes, Template: BatchTemplateSummary{TemplateID: batch.template.TemplateID, Name: batch.template.DisplayName, AdvertiserID: batch.template.AdvertiserID, ProductIDs: append([]string(nil), batch.template.ProductIDs...)}, Status: "preflight_complete"}
		if childErr != nil {
			entry.Cause = childErr
			entry.Status, entry.FailureStage = "preflight_failed", "preflight"
			var stage *PreflightStageError
			if errors.As(childErr, &stage) {
				entry.FailureStage = stage.Stage
			}
			result.ExitCode = 1
		}
		if child.Mode != "" {
			for index := range child.Skipped {
				local := child.Skipped[index].InputIndex
				if local >= 0 && local < len(batch.indexes) {
					child.Skipped[index].InputIndex = batch.indexes[local]
				}
			}
			entry.Result = &child
			if child.ExitCode != 0 {
				result.ExitCode = 1
			}
		}
		result.Batches = append(result.Batches, entry)
	}
	if len(result.Pending) > 0 {
		result.ExitCode = 1
	}
	sort.Slice(result.Pending, func(i, j int) bool { return result.Pending[i].InputIndex < result.Pending[j].InputIndex })
	result.Counts["routed_batches"], result.Counts["pending_works"] = len(result.Batches), len(result.Pending)
	result.Counts["failed_batches"] = 0
	for _, batch := range result.Batches {
		if batch.Status == "preflight_failed" {
			result.Counts["failed_batches"]++
		}
		if batch.Result != nil {
			for key, count := range batch.Result.Counts {
				result.Counts[key] += count
			}
		}
	}
	return result, nil
}

type routeConfigSnapshot struct{ config map[string]any }

func (snapshot routeConfigSnapshot) Read(context.Context) (map[string]any, error) {
	return snapshot.config, nil
}

func containsRouteProduct(products []string, product string) bool {
	for _, id := range products {
		if id == product {
			return true
		}
	}
	return false
}
func overlapsRouteProducts(products, hints []string) bool {
	for _, hint := range hints {
		if containsRouteProduct(products, hint) {
			return true
		}
	}
	return false
}

func routingMetadataReason(code string) string {
	switch code {
	case "f2_metadata_query_failed", "f2_empty_response", "f2_incomplete_work_metadata", "f2_invalid_work_metadata", "f2_work_id_mismatch", "f2_response_parse_failed", "f2_http_error", "f2_network_error", "f2_work_timeout", "f2_batch_deadline_exceeded", "f2_runtime_unavailable", "f2_cli_timeout", "f2_response_too_large", "invalid_f2_response", "f2_cli_failed", "duplicate_input", "invalid_url", "untrusted_host", "untrusted_port", "missing_work_id", "redirect_failed", "too_many_redirects":
		return code
	default:
		return "product_metadata_unavailable"
	}
}
