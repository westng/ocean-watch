package accounts

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	authapplication "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
)

type reportStore struct{ book domain.AccountBook }

func (store reportStore) Read(context.Context) (domain.AccountBook, error) { return store.book, nil }

type reportTokens struct {
	mu      sync.Mutex
	queries []authapplication.TokenQuery
}

func (tokens *reportTokens) Ensure(_ context.Context, query authapplication.TokenQuery) (authapplication.TokenLease, error) {
	tokens.mu.Lock()
	tokens.queries = append(tokens.queries, query)
	tokens.mu.Unlock()
	return authapplication.TokenLease{
		Channel: query.Channel, AuthorizationID: "fixture-authorization",
		AccessToken: "fixture-" + query.Channel,
	}, nil
}

type channelReportStub struct {
	mu       sync.Mutex
	requests []ReportRequest
	metrics  domain.AccountMetrics
	err      error
	delay    time.Duration
}

func (stub *channelReportStub) QueryAccount(ctx context.Context, request ReportRequest) (domain.AccountMetrics, error) {
	stub.mu.Lock()
	stub.requests = append(stub.requests, request)
	stub.mu.Unlock()
	if stub.delay != 0 {
		select {
		case <-time.After(stub.delay):
		case <-ctx.Done():
			return domain.AccountMetrics{}, ctx.Err()
		}
	}
	return stub.metrics, stub.err
}

func TestAccountReportPreservesOrderAndContainsSingleAccountFailure(t *testing.T) {
	book := domain.NewAccountBook()
	book.Accounts[domain.Marketing] = []domain.ManagedAccount{{
		Channel: domain.Marketing, AdvertiserID: "1000000000000001", Name: "First", Enabled: true,
	}}
	book.Accounts[domain.Qianchuan] = []domain.ManagedAccount{{
		Channel: domain.Qianchuan, AdvertiserID: "1000000000000002", Name: "Second", Enabled: true,
	}}
	marketing := &channelReportStub{metrics: domain.AccountMetrics{
		MetricBasis: domain.MarketingMetricBasis, Spend: domain.MustDecimal("10"),
		Orders: 2, GMV: domain.MustDecimal("20"), ROI: domain.MustDecimal("2"),
	}}
	qianchuan := &channelReportStub{err: errors.New("synthetic timeout"), delay: time.Millisecond}
	tokens := &reportTokens{}
	result, err := (Reporter{
		Store: reportStore{book: book}, Tokens: tokens,
		Marketing: marketing, Qianchuan: qianchuan, Concurrent: 2,
	}).Report(context.Background(), Query{StartDate: "2026-07-25", EndDate: "2026-07-25"})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Summary.SuccessfulAccountCount != 1 || result.Summary.FailedAccountCount != 1 {
		t.Fatalf("partial failure was not preserved: %#v", result.Summary)
	}
	if got := []string{result.Accounts[0].AdvertiserID, result.Accounts[1].AdvertiserID}; !reflect.DeepEqual(got, []string{"1000000000000001", "1000000000000002"}) {
		t.Fatalf("configured account order changed: %v", got)
	}
	if len(marketing.requests) != 1 || len(qianchuan.requests) != 1 || len(tokens.queries) != 2 {
		t.Fatalf("unexpected call counts: marketing=%d qianchuan=%d tokens=%d", len(marketing.requests), len(qianchuan.requests), len(tokens.queries))
	}
}

func TestAccountReportChannelFilterAndDisabledSelection(t *testing.T) {
	book := domain.NewAccountBook()
	book.Accounts[domain.Marketing] = []domain.ManagedAccount{
		{Channel: domain.Marketing, AdvertiserID: "1000000000000001", Name: "Enabled", Enabled: true},
		{Channel: domain.Marketing, AdvertiserID: "1000000000000002", Name: "Disabled", Enabled: false},
	}
	stub := &channelReportStub{metrics: domain.AccountMetrics{MetricBasis: domain.MarketingMetricBasis}}
	result, err := (Reporter{
		Store: reportStore{book: book}, Tokens: &reportTokens{}, Marketing: stub, Qianchuan: stub,
	}).Report(context.Background(), Query{
		Channels: []domain.Channel{domain.Marketing}, StartDate: "2026-07-25", EndDate: "2026-07-25",
	})
	if err != nil || len(result.Accounts) != 1 || result.Accounts[0].Name != "Enabled" {
		t.Fatalf("default enabled selection failed: result=%#v err=%v", result.Accounts, err)
	}
}

func TestAccountReportChannelFiltersDoNotReorderConfiguredAccounts(t *testing.T) {
	book := domain.NewAccountBook()
	book.Accounts[domain.Marketing] = []domain.ManagedAccount{{
		Channel: domain.Marketing, AdvertiserID: "1000000000000001", Name: "Marketing", Enabled: true,
	}}
	book.Accounts[domain.Qianchuan] = []domain.ManagedAccount{{
		Channel: domain.Qianchuan, AdvertiserID: "1000000000000002", Name: "Qianchuan", Enabled: true,
	}}
	selected, err := selectAccounts(
		book, []domain.Channel{domain.Qianchuan, domain.Marketing, domain.Qianchuan}, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{selected[0].Name, selected[1].Name}; !reflect.DeepEqual(got, []string{"Marketing", "Qianchuan"}) {
		t.Fatalf("channel filters reordered accounts: %v", got)
	}
}
