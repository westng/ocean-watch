package workmetadata

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	portworkmetadata "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/workmetadata"
)

const (
	DefaultConcurrency = 4
	MaxConcurrency     = 10
)

type ResolveRequest struct {
	URLs        []string
	Concurrency int
}

type ResolveResult struct {
	Resolved            []domain.ResolvedWorkLink `json:"resolved"`
	Skipped             []domain.SkippedWorkLink  `json:"skipped"`
	MetadataPerformance map[string]any            `json:"metadata_performance,omitempty"`
}

type Resolver struct {
	Links    portworkmetadata.Resolver
	Metadata portworkmetadata.MetadataResolver
}

func (resolver Resolver) Resolve(ctx context.Context, request ResolveRequest) (ResolveResult, error) {
	result, err := resolver.ResolveLinks(ctx, request)
	if err != nil {
		return ResolveResult{}, err
	}
	return resolver.ResolveMetadata(ctx, result, request.Concurrency)
}

// ResolveLinks performs only URL normalization/redirect resolution. Keeping
// this stage separate lets callers consult a scoped hint cache before invoking
// the comparatively expensive metadata resolver.
func (resolver Resolver) ResolveLinks(ctx context.Context, request ResolveRequest) (ResolveResult, error) {
	if ctx == nil {
		return ResolveResult{}, errors.New("work-link context is required")
	}
	if resolver.Links == nil {
		return ResolveResult{}, errors.New("work-link resolver is required")
	}
	concurrency := request.Concurrency
	if concurrency == 0 {
		concurrency = DefaultConcurrency
	}
	if concurrency < 1 || concurrency > MaxConcurrency {
		return ResolveResult{}, errors.New("concurrency must be between 1 and 10")
	}
	if len(request.URLs) == 0 {
		return ResolveResult{}, errors.New("at least one work URL is required")
	}
	rows := make([]resolvedRow, len(request.URLs))
	semaphore := make(chan struct{}, min(concurrency, len(request.URLs)))
	var wait sync.WaitGroup
	for index, value := range request.URLs {
		wait.Add(1)
		go func(index int, value string) {
			defer wait.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				rows[index] = resolvedRow{err: ctx.Err()}
				return
			}
			row, err := resolver.Links.Resolve(ctx, value)
			row.InputIndex = index
			rows[index] = resolvedRow{value: row, err: err, input: value}
		}(index, value)
	}
	wait.Wait()
	if err := ctx.Err(); err != nil {
		return ResolveResult{}, err
	}
	result := ResolveResult{Resolved: []domain.ResolvedWorkLink{}, Skipped: []domain.SkippedWorkLink{}}
	seen := map[string]struct{}{}
	for index, row := range rows {
		if row.err != nil {
			var linkErr *domain.WorkLinkError
			reason, message := "link_resolution_failed", "作品链接解析失败"
			if errors.As(row.err, &linkErr) {
				reason, message = linkErr.Code, linkErr.Message
			}
			result.Skipped = append(result.Skipped, domain.SkippedWorkLink{
				InputIndex: index, InputURL: strings.TrimSpace(row.input), Status: "skipped",
				Reason: reason, Message: message,
			})
			continue
		}
		if _, duplicate := seen[row.value.AwemeItemID]; duplicate {
			result.Skipped = append(result.Skipped, domain.SkippedWorkLink{
				InputIndex: row.value.InputIndex, InputURL: row.value.InputURL,
				ResolvedURL: row.value.ResolvedURL, CanonicalURL: row.value.CanonicalURL,
				AwemeItemID: row.value.AwemeItemID, Status: "skipped", Reason: "duplicate_input",
				Message: "同一作品在本批次中重复出现",
			})
			continue
		}
		seen[row.value.AwemeItemID] = struct{}{}
		result.Resolved = append(result.Resolved, row.value)
	}
	return result, nil
}

// ResolveMetadata enriches only the resolved rows in result. Callers can pass
// a subset of rows to resolve just cache misses while retaining original input
// indexes for deterministic skipped-row reporting.
func (resolver Resolver) ResolveMetadata(ctx context.Context, result ResolveResult, concurrency int) (ResolveResult, error) {
	if ctx == nil {
		return ResolveResult{}, errors.New("work-link context is required")
	}
	if concurrency == 0 {
		concurrency = DefaultConcurrency
	}
	if concurrency < 1 || concurrency > MaxConcurrency {
		return ResolveResult{}, errors.New("concurrency must be between 1 and 10")
	}
	if resolver.Metadata == nil || len(result.Resolved) == 0 {
		return result, nil
	}
	workIDs := make([]string, 0, len(result.Resolved))
	for _, row := range result.Resolved {
		workIDs = append(workIDs, row.AwemeItemID)
	}
	metadata, err := resolver.Metadata.ResolveMany(ctx, workIDs, concurrency)
	if err != nil {
		return ResolveResult{}, err
	}
	resolved := make([]domain.ResolvedWorkLink, 0, len(result.Resolved))
	for _, row := range result.Resolved {
		value, ok := metadata.Rows[row.AwemeItemID]
		if !ok {
			failure := metadata.Errors[row.AwemeItemID]
			reason, message := failure.Code, failure.Message
			if reason == "" {
				reason, message = "f2_metadata_query_failed", "F2 未返回可用的公开作品元数据"
			}
			result.Skipped = append(result.Skipped, domain.SkippedWorkLink{
				InputIndex: row.InputIndex, InputURL: row.InputURL, ResolvedURL: row.ResolvedURL,
				CanonicalURL: row.CanonicalURL, AwemeItemID: row.AwemeItemID,
				Status: "skipped", Reason: reason, Message: message,
			})
			continue
		}
		row.CreatorName = value.CreatorName
		row.OwnerHint = &domain.WorkOwnerHint{AwemeID: value.AwemeID, AwemeShowID: value.AwemeShowID}
		if value.ProductID != "" {
			row.ProductHint = &domain.WorkProductHint{ProductID: value.ProductID, ProductName: value.ProductName}
		}
		row.Metadata = append([]byte(nil), value.Metadata...)
		resolved = append(resolved, row)
	}
	result.Resolved = resolved
	result.MetadataPerformance = metadata.Performance
	return result, nil
}

type resolvedRow struct {
	value domain.ResolvedWorkLink
	err   error
	input string
}
