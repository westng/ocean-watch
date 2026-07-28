package pagination

import (
	"context"
	"errors"
	"fmt"

	platformretry "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/retry"
)

type CursorPage[T any] struct {
	Cursor      string
	NextCursor  string
	HasMore     bool
	TotalNumber *int
	Rows        []T
}

type CursorOptions[T any] struct {
	InitialCursor string
	MaxPages      int
	Key           func(T) string
	Retry         platformretry.Policy
	Classify      platformretry.Classifier
	Fetch         func(context.Context, string) (CursorPage[T], error)
}

func CollectCursor[T any](ctx context.Context, options CursorOptions[T]) ([]T, error) {
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
	cursor := options.InitialCursor
	rows := []T{}
	seenRows, seenCursors := map[string]struct{}{}, map[string]struct{}{cursor: {}}
	expectedTotal := -1
	for pageNumber := 1; pageNumber <= maxPages; pageNumber++ {
		page, err := platformretry.Do(
			ctx, options.Retry, options.Classify,
			func(ctx context.Context, _ int) (CursorPage[T], error) {
				return options.Fetch(ctx, cursor)
			},
		)
		if err != nil {
			return nil, fmt.Errorf("fetch cursor page %d: %w", pageNumber, err)
		}
		if page.Cursor != cursor {
			return nil, fmt.Errorf("cursor page %d returned a mismatched cursor", pageNumber)
		}
		if page.TotalNumber != nil {
			if *page.TotalNumber < 0 {
				return nil, fmt.Errorf("cursor page %d returned a negative total", pageNumber)
			}
			if expectedTotal < 0 {
				expectedTotal = *page.TotalNumber
			} else if expectedTotal != *page.TotalNumber {
				return nil, fmt.Errorf("cursor page %d changed the declared total", pageNumber)
			}
		}
		for _, row := range page.Rows {
			key := options.Key(row)
			if key == "" {
				return nil, fmt.Errorf("cursor page %d returned an empty unique key", pageNumber)
			}
			if _, exists := seenRows[key]; exists {
				return nil, fmt.Errorf("cursor page %d returned duplicate unique key %q", pageNumber, key)
			}
			seenRows[key] = struct{}{}
			rows = append(rows, row)
		}
		if !page.HasMore {
			if expectedTotal >= 0 && len(rows) != expectedTotal {
				return nil, fmt.Errorf("cursor pagination returned %d rows but declared %d", len(rows), expectedTotal)
			}
			return rows, nil
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			return nil, fmt.Errorf("cursor pagination stalled on page %d", pageNumber)
		}
		if _, exists := seenCursors[page.NextCursor]; exists {
			return nil, fmt.Errorf("cursor pagination repeated a prior cursor on page %d", pageNumber)
		}
		seenCursors[page.NextCursor] = struct{}{}
		cursor = page.NextCursor
	}
	return nil, fmt.Errorf("cursor pagination exceeds the safety cap of %d pages", maxPages)
}
