package workmetadata

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
)

type resolverStub struct {
	mu        sync.Mutex
	active    int
	maxActive int
}

func (resolver *resolverStub) Resolve(ctx context.Context, value string) (domain.ResolvedWorkLink, error) {
	resolver.mu.Lock()
	resolver.active++
	if resolver.active > resolver.maxActive {
		resolver.maxActive = resolver.active
	}
	resolver.mu.Unlock()
	defer func() {
		resolver.mu.Lock()
		resolver.active--
		resolver.mu.Unlock()
	}()
	select {
	case <-time.After(time.Millisecond):
	case <-ctx.Done():
		return domain.ResolvedWorkLink{}, ctx.Err()
	}
	if value == "bad" {
		return domain.ResolvedWorkLink{}, domain.NewWorkLinkError("invalid_url", "invalid fixture URL")
	}
	itemID := value
	if value == "duplicate" {
		itemID = "1"
	}
	return domain.ResolvedWorkLink{
		InputURL: "https://v.douyin.com/" + value, ResolvedURL: "https://www.douyin.com/video/" + itemID,
		CanonicalURL: "https://www.douyin.com/video/" + itemID, AwemeItemID: itemID,
	}, nil
}

func TestResolveWorkLinksPreservesOrderDeduplicatesAndBoundsConcurrency(t *testing.T) {
	stub := &resolverStub{}
	result, err := (Resolver{Links: stub}).Resolve(context.Background(), ResolveRequest{
		URLs: []string{"1", "bad", "2", "duplicate", "3"}, Concurrency: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Resolved) != 3 || result.Resolved[0].AwemeItemID != "1" ||
		result.Resolved[1].AwemeItemID != "2" || result.Resolved[2].AwemeItemID != "3" {
		t.Fatalf("resolved order changed: %#v", result.Resolved)
	}
	if len(result.Skipped) != 2 || result.Skipped[0].Reason != "invalid_url" || result.Skipped[1].Reason != "duplicate_input" {
		t.Fatalf("skip classification changed: %#v", result.Skipped)
	}
	if stub.maxActive > 2 {
		t.Fatalf("resolver concurrency = %d, want <= 2", stub.maxActive)
	}
}

func TestResolveWorkLinksRejectsInvalidConcurrencyAndPropagatesCancellation(t *testing.T) {
	resolver := Resolver{Links: &resolverStub{}}
	if _, err := resolver.Resolve(context.Background(), ResolveRequest{URLs: []string{"1"}, Concurrency: 11}); err == nil {
		t.Fatal("unsafe concurrency was accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := resolver.Resolve(ctx, ResolveRequest{URLs: []string{"1"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was not preserved: %v", err)
	}
}
