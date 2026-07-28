package workmetadata

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	portworkmetadata "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/workmetadata"
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
	Resolved []domain.ResolvedWorkLink `json:"resolved"`
	Skipped  []domain.SkippedWorkLink  `json:"skipped"`
}

type Resolver struct {
	Links portworkmetadata.Resolver
}

func (resolver Resolver) Resolve(ctx context.Context, request ResolveRequest) (ResolveResult, error) {
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

type resolvedRow struct {
	value domain.ResolvedWorkLink
	err   error
	input string
}
