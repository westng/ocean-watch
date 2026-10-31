package qianchuan

import (
	"context"
	"errors"
	"sort"

	applicationworkmetadata "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/workmetadata"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
)

// This adapter reuses public metadata fetched for routing. User-supplied
// creator hints remain untrusted and always go through the official verifier.
type routedLinkResolver struct {
	base     WorkLinkResolver
	prepared map[string]domain.ResolvedWorkLink
	items    []domainqianchuan.BatchItem
}

func (resolver *routedLinkResolver) Resolve(ctx context.Context, request applicationworkmetadata.ResolveRequest) (applicationworkmetadata.ResolveResult, error) {
	result, err := resolver.ResolveLinks(ctx, request)
	if err != nil {
		return result, err
	}
	return resolver.ResolveMetadata(ctx, result, request.Concurrency)
}

func (resolver *routedLinkResolver) ResolveLinks(ctx context.Context, request applicationworkmetadata.ResolveRequest) (applicationworkmetadata.ResolveResult, error) {
	result := applicationworkmetadata.ResolveResult{Resolved: []domain.ResolvedWorkLink{}, Skipped: []domain.SkippedWorkLink{}}
	missing, indexes := []string{}, []int{}
	for index, url := range request.URLs {
		if row, ok := resolver.prepared[url]; ok {
			row.InputIndex = index
			result.Resolved = append(result.Resolved, row)
		} else {
			missing = append(missing, url)
			indexes = append(indexes, index)
		}
	}
	if len(missing) > 0 {
		if resolver.base == nil {
			return result, errors.New("Qianchuan work-link resolver is required")
		}
		var resolved applicationworkmetadata.ResolveResult
		var err error
		if staged, ok := resolver.base.(StagedWorkLinkResolver); ok {
			resolved, err = staged.ResolveLinks(ctx, applicationworkmetadata.ResolveRequest{URLs: missing, Concurrency: request.Concurrency})
		} else {
			resolved, err = resolver.base.Resolve(ctx, applicationworkmetadata.ResolveRequest{URLs: missing, Concurrency: request.Concurrency})
		}
		if err != nil {
			return result, err
		}
		for _, row := range resolved.Resolved {
			if row.InputIndex >= 0 && row.InputIndex < len(indexes) {
				row.InputIndex = indexes[row.InputIndex]
				result.Resolved = append(result.Resolved, row)
			}
		}
		for _, row := range resolved.Skipped {
			if row.InputIndex >= 0 && row.InputIndex < len(indexes) {
				row.InputIndex = indexes[row.InputIndex]
				result.Skipped = append(result.Skipped, row)
			}
		}
	}
	for index := range result.Resolved {
		resolver.applyHint(&result.Resolved[index])
	}
	sort.Slice(result.Resolved, func(i, j int) bool { return result.Resolved[i].InputIndex < result.Resolved[j].InputIndex })
	seen := map[string]int{}
	check := func(id string, index int) error {
		if id == "" || index < 0 || index >= len(resolver.items) {
			return nil
		}
		if previous, exists := seen[id]; exists {
			left, right := resolver.items[previous], resolver.items[index]
			if left.PlanType != right.PlanType || left.Business != right.Business || left.ProductID != right.ProductID || left.CreatorDouyinID != right.CreatorDouyinID || left.CreatorUID != right.CreatorUID || left.CreatorName != right.CreatorName {
				return domainqianchuan.ErrDuplicateItemConflict
			}
		} else {
			seen[id] = index
		}
		return nil
	}
	for _, row := range result.Resolved {
		if err := check(row.AwemeItemID, row.InputIndex); err != nil {
			return result, err
		}
	}
	for _, row := range result.Skipped {
		if row.Reason == "duplicate_input" {
			if err := check(row.AwemeItemID, row.InputIndex); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}

func (resolver *routedLinkResolver) applyHint(row *domain.ResolvedWorkLink) {
	if row.InputIndex < 0 || row.InputIndex >= len(resolver.items) {
		return
	}
	item := resolver.items[row.InputIndex]
	row.TargetProductID = item.ProductID
	if item.CreatorDouyinID != "" {
		row.OwnerHint = &domain.WorkOwnerHint{AwemeID: item.CreatorUID, AwemeShowID: item.CreatorDouyinID}
	}
	if item.CreatorName != "" {
		row.CreatorName = item.CreatorName
	}
}

func (resolver *routedLinkResolver) ResolveMetadata(ctx context.Context, input applicationworkmetadata.ResolveResult, concurrency int) (applicationworkmetadata.ResolveResult, error) {
	result := applicationworkmetadata.ResolveResult{Resolved: []domain.ResolvedWorkLink{}, Skipped: append([]domain.SkippedWorkLink(nil), input.Skipped...)}
	missing := []domain.ResolvedWorkLink{}
	for _, row := range input.Resolved {
		resolver.applyHint(&row)
		if row.OwnerHint != nil && row.OwnerHint.AwemeShowID != "" && row.CreatorName != "" {
			result.Resolved = append(result.Resolved, row)
		} else {
			missing = append(missing, row)
		}
	}
	if len(missing) == 0 {
		return result, nil
	}
	staged, ok := resolver.base.(StagedWorkLinkResolver)
	if !ok {
		result.Resolved = append(result.Resolved, missing...)
		return result, nil
	}
	enriched, err := staged.ResolveMetadata(ctx, applicationworkmetadata.ResolveResult{Resolved: missing}, concurrency)
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		var metadataError *domain.WorkLinkError
		if !errors.As(err, &metadataError) {
			return result, err
		}
		for _, row := range missing {
			enriched.Skipped = append(enriched.Skipped, domain.SkippedWorkLink{InputIndex: row.InputIndex, AwemeItemID: row.AwemeItemID, Reason: metadataError.Code, Message: "作品信息获取未完成", Status: "skipped"})
		}
	}
	byIndex := map[int]domain.ResolvedWorkLink{}
	for _, row := range missing {
		byIndex[row.InputIndex] = row
	}
	for _, row := range enriched.Resolved {
		resolver.applyHint(&row)
		result.Resolved = append(result.Resolved, row)
	}
	for _, failure := range enriched.Skipped {
		row, exists := byIndex[failure.InputIndex]
		if !exists {
			continue
		}
		resolver.applyHint(&row)
		if row.OwnerHint != nil && row.OwnerHint.AwemeShowID != "" {
			result.Resolved = append(result.Resolved, row)
		} else {
			result.Skipped = append(result.Skipped, failure)
		}
	}
	result.MetadataPerformance = enriched.MetadataPerformance
	return result, nil
}
