package pagination

import (
	"context"
	"errors"
	"fmt"

	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
)

const DefaultMaxPages = 100

type Page[T any] struct {
	Number      int
	TotalPages  int
	TotalNumber int
	Rows        []T
}

type PageOptions[T any] struct {
	MaxPages   int
	StartPage  int
	SinglePage bool
	Key        func(T) string
	Retry      platformretry.Policy
	Classify   platformretry.Classifier
	Fetch      func(context.Context, int) (Page[T], error)
}

func CollectPages[T any](ctx context.Context, options PageOptions[T]) ([]T, error) {
	if ctx == nil {
		return nil, errors.New("pagination context is required")
	}
	if options.Fetch == nil || options.Key == nil {
		return nil, errors.New("pagination fetch and key functions are required")
	}
	maxPages := options.MaxPages
	if maxPages == 0 {
		maxPages = DefaultMaxPages
	}
	if maxPages < 1 {
		return nil, errors.New("pagination max pages must be positive")
	}
	startPage := options.StartPage
	if startPage == 0 {
		startPage = 1
	}
	if startPage < 1 {
		return nil, errors.New("pagination start page must be positive")
	}
	rows := []T{}
	seen := map[string]struct{}{}
	expectedPages, expectedTotal := -1, -1
	fetchedPages := 0
	for pageNumber := startPage; ; pageNumber++ {
		fetchedPages++
		if fetchedPages > maxPages {
			return nil, fmt.Errorf("pagination exceeds the safety cap of %d pages", maxPages)
		}
		page, err := platformretry.Do(
			ctx, options.Retry, options.Classify,
			func(ctx context.Context, _ int) (Page[T], error) {
				return options.Fetch(ctx, pageNumber)
			},
		)
		if err != nil {
			return nil, fmt.Errorf("fetch page %d: %w", pageNumber, err)
		}
		if page.Number != pageNumber || page.TotalPages < 0 || page.TotalNumber < 0 {
			return nil, fmt.Errorf("page %d returned invalid pagination metadata", pageNumber)
		}
		if page.TotalPages == 0 {
			if pageNumber != startPage || startPage != 1 || page.TotalNumber != 0 || len(page.Rows) != 0 {
				return nil, fmt.Errorf("page %d returned contradictory empty pagination metadata", pageNumber)
			}
			return []T{}, nil
		}
		if pageNumber > page.TotalPages {
			return nil, fmt.Errorf("page %d exceeds declared total pages %d", pageNumber, page.TotalPages)
		}
		if expectedPages < 0 {
			expectedPages, expectedTotal = page.TotalPages, page.TotalNumber
		} else if page.TotalPages != expectedPages || page.TotalNumber != expectedTotal {
			return nil, fmt.Errorf("page %d changed declared pagination totals", pageNumber)
		}
		for _, row := range page.Rows {
			key := options.Key(row)
			if key == "" {
				return nil, fmt.Errorf("page %d returned an empty unique key", pageNumber)
			}
			if _, exists := seen[key]; exists {
				return nil, fmt.Errorf("page %d returned duplicate unique key %q", pageNumber, key)
			}
			seen[key] = struct{}{}
			rows = append(rows, row)
		}
		if options.SinglePage {
			return rows, nil
		}
		if pageNumber == expectedPages {
			break
		}
	}
	if startPage == 1 && len(rows) != expectedTotal {
		return nil, fmt.Errorf("pagination returned %d unique rows but declared %d", len(rows), expectedTotal)
	}
	if startPage > 1 && len(rows) > expectedTotal {
		return nil, fmt.Errorf("pagination returned %d unique rows after page %d but declared %d total", len(rows), startPage, expectedTotal)
	}
	return rows, nil
}
