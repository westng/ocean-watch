package pagination

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	platformretry "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/retry"
)

type fixtureRow struct{ ID string }

func TestPageLocalRetryStateMachine(t *testing.T) {
	sequence := []int{}
	pageTwoAttempts := 0
	rows, err := CollectPages(context.Background(), PageOptions[fixtureRow]{
		Retry: platformretry.Policy{
			Delays: []time.Duration{0, 0},
			Sleep:  func(context.Context, time.Duration) error { return nil },
		},
		Classify: func(error) (bool, time.Duration) { return true, 0 },
		Key:      func(row fixtureRow) string { return row.ID },
		Fetch: func(_ context.Context, page int) (Page[fixtureRow], error) {
			sequence = append(sequence, page)
			if page == 2 && pageTwoAttempts < 2 {
				pageTwoAttempts++
				return Page[fixtureRow]{}, errors.New("transient")
			}
			return Page[fixtureRow]{
				Number: page, TotalPages: 3, TotalNumber: 3,
				Rows: []fixtureRow{{ID: string(rune('0' + page))}},
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sequence, []int{1, 2, 2, 2, 3}) {
		t.Fatalf("page sequence = %v", sequence)
	}
	if len(rows) != 3 {
		t.Fatalf("row count = %d", len(rows))
	}
}

func TestPageLocalRetryStateMachineFailsClosed(t *testing.T) {
	tests := []struct {
		name  string
		fetch func(context.Context, int) (Page[fixtureRow], error)
	}{
		{
			name: "duplicate",
			fetch: func(_ context.Context, page int) (Page[fixtureRow], error) {
				return Page[fixtureRow]{Number: page, TotalPages: 2, TotalNumber: 2, Rows: []fixtureRow{{ID: "same"}}}, nil
			},
		},
		{
			name: "contradictory-total",
			fetch: func(_ context.Context, page int) (Page[fixtureRow], error) {
				return Page[fixtureRow]{Number: page, TotalPages: 1, TotalNumber: 2, Rows: []fixtureRow{{ID: "one"}}}, nil
			},
		},
		{
			name: "changed-pages",
			fetch: func(_ context.Context, page int) (Page[fixtureRow], error) {
				totalPages := 2
				if page == 2 {
					totalPages = 3
				}
				return Page[fixtureRow]{Number: page, TotalPages: totalPages, TotalNumber: 2, Rows: []fixtureRow{{ID: string(rune('0' + page))}}}, nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if rows, err := CollectPages(context.Background(), PageOptions[fixtureRow]{
				Key: func(row fixtureRow) string { return row.ID }, Fetch: test.fetch,
			}); err == nil || rows != nil {
				t.Fatalf("invalid pagination returned rows=%v err=%v", rows, err)
			}
		})
	}
}

func TestPageLocalRetryStateMachineRejectsStalledCursor(t *testing.T) {
	total := 2
	_, err := CollectCursor(context.Background(), CursorOptions[fixtureRow]{
		InitialCursor: "0", Key: func(row fixtureRow) string { return row.ID },
		Fetch: func(_ context.Context, cursor string) (CursorPage[fixtureRow], error) {
			return CursorPage[fixtureRow]{
				Cursor: cursor, NextCursor: cursor, HasMore: true, TotalNumber: &total,
				Rows: []fixtureRow{{ID: "one"}},
			}, nil
		},
	})
	if err == nil {
		t.Fatal("stalled cursor was accepted")
	}
}

func TestPageTraversalSupportsCompatibleStartAndSinglePage(t *testing.T) {
	sequence := []int{}
	rows, err := CollectPages(context.Background(), PageOptions[fixtureRow]{
		StartPage: 2,
		Key:       func(row fixtureRow) string { return row.ID },
		Fetch: func(_ context.Context, page int) (Page[fixtureRow], error) {
			sequence = append(sequence, page)
			return Page[fixtureRow]{
				Number: page, TotalPages: 3, TotalNumber: 3,
				Rows: []fixtureRow{{ID: string(rune('0' + page))}},
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sequence, []int{2, 3}) || len(rows) != 2 {
		t.Fatalf("start-page traversal changed: sequence=%v rows=%v", sequence, rows)
	}

	sequence = nil
	rows, err = CollectPages(context.Background(), PageOptions[fixtureRow]{
		StartPage:  2,
		SinglePage: true,
		Key:        func(row fixtureRow) string { return row.ID },
		Fetch: func(_ context.Context, page int) (Page[fixtureRow], error) {
			sequence = append(sequence, page)
			return Page[fixtureRow]{
				Number: page, TotalPages: 3, TotalNumber: 3,
				Rows: []fixtureRow{{ID: "two"}},
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sequence, []int{2}) || !reflect.DeepEqual(rows, []fixtureRow{{ID: "two"}}) {
		t.Fatalf("single-page traversal changed: sequence=%v rows=%v", sequence, rows)
	}
}
