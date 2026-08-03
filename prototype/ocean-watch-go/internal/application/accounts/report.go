package accounts

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	authapplication "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/requestcontrol"
)

const (
	DefaultConcurrency = 4
	MaxConcurrency     = 8
)

type AccountStore interface {
	Read(context.Context) (domain.AccountBook, error)
}

type TokenProvider interface {
	Ensure(context.Context, authapplication.TokenQuery) (authapplication.TokenLease, error)
}

type ReportRequest struct {
	AdvertiserID string
	AccessToken  string
	StartDate    string
	EndDate      string
}

type ChannelReporter interface {
	QueryAccount(context.Context, ReportRequest) (domain.AccountMetrics, error)
}

type Query struct {
	Channels        []domain.Channel
	StartDate       string
	EndDate         string
	IncludeDisabled bool
	Concurrency     int
}

type Reporter struct {
	Store      AccountStore
	Tokens     TokenProvider
	Marketing  ChannelReporter
	Qianchuan  ChannelReporter
	Concurrent int
}

func (reporter Reporter) Report(ctx context.Context, query Query) (domain.AccountReportResult, *domain.Error) {
	if ctx == nil {
		return domain.AccountReportResult{}, domain.NewError("unexpected_error", "account report context is required", 1, nil)
	}
	if reporter.Store == nil || reporter.Tokens == nil || reporter.Marketing == nil || reporter.Qianchuan == nil {
		return domain.AccountReportResult{}, domain.NewError("unexpected_error", "account report dependencies are incomplete", 1, nil)
	}
	if err := validateDateRange(query.StartDate, query.EndDate); err != nil {
		return domain.AccountReportResult{}, domain.NewError("invalid_arguments", err.Error(), 2, nil)
	}
	book, err := reporter.Store.Read(ctx)
	if err != nil {
		return domain.AccountReportResult{}, domain.WrapError("configuration_error", "failed to read account configuration", 2, err)
	}
	selected, err := selectAccounts(book, query.Channels, query.IncludeDisabled)
	if err != nil {
		return domain.AccountReportResult{}, domain.NewError("configuration_error", err.Error(), 2, nil)
	}
	if len(selected) == 0 {
		return domain.AccountReportResult{}, domain.NewError("configuration_error", "no managed accounts matched this query", 2, nil)
	}
	concurrency := query.Concurrency
	if concurrency == 0 {
		concurrency = reporter.Concurrent
	}
	if concurrency == 0 {
		concurrency = DefaultConcurrency
	}
	if concurrency < 1 || concurrency > MaxConcurrency {
		return domain.AccountReportResult{}, domain.NewError("invalid_arguments", "concurrency must be between 1 and 8", 2, nil)
	}
	rows := make([]domain.AccountReportRow, len(selected))
	semaphore := make(chan struct{}, min(concurrency, len(selected)))
	var wait sync.WaitGroup
	for index, account := range selected {
		wait.Add(1)
		go func() {
			defer wait.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				rows[index] = failedRow(account, ctx.Err())
				return
			}
			rows[index] = reporter.queryAccount(ctx, account, query.StartDate, query.EndDate)
		}()
	}
	wait.Wait()
	return domain.NewAccountReportResult(rows, query.StartDate, query.EndDate), nil
}

func validateDateRange(startDate, endDate string) error {
	start, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		return errors.New("start-date and end-date must use YYYY-MM-DD")
	}
	end, err := time.Parse("2006-01-02", endDate)
	if err != nil {
		return errors.New("start-date and end-date must use YYYY-MM-DD")
	}
	if start.After(end) {
		return errors.New("start-date cannot be after end-date")
	}
	return nil
}

func selectAccounts(
	book domain.AccountBook,
	channels []domain.Channel,
	includeDisabled bool,
) ([]domain.ManagedAccount, error) {
	if len(channels) == 0 {
		return book.List(nil, !includeDisabled), nil
	}
	selectedChannels := map[domain.Channel]struct{}{}
	for _, channel := range channels {
		if channel != domain.Marketing && channel != domain.Qianchuan {
			return nil, fmt.Errorf("unknown channel: %s", channel)
		}
		selectedChannels[channel] = struct{}{}
	}
	result := make([]domain.ManagedAccount, 0)
	for _, account := range book.List(nil, !includeDisabled) {
		if _, selected := selectedChannels[account.Channel]; selected {
			result = append(result, account)
		}
	}
	return result, nil
}

func (reporter Reporter) queryAccount(
	ctx context.Context,
	account domain.ManagedAccount,
	startDate string,
	endDate string,
) domain.AccountReportRow {
	lease, err := reporter.Tokens.Ensure(ctx, authapplication.TokenQuery{
		Channel: string(account.Channel), AdvertiserID: account.AdvertiserID,
		AuthAccountID: account.AuthAccountID,
	})
	if err != nil {
		return failedRow(account, err)
	}
	ctx, err = authapplication.WithTokenLease(ctx, lease)
	if err != nil {
		return failedRow(account, err)
	}
	if account.Channel == domain.Qianchuan {
		ctx, err = requestcontrol.WithAdvertiser(ctx, account.AdvertiserID)
		if err != nil {
			return failedRow(account, err)
		}
	}
	adapter := reporter.Marketing
	if account.Channel == domain.Qianchuan {
		adapter = reporter.Qianchuan
	}
	metrics, err := adapter.QueryAccount(ctx, ReportRequest{
		AdvertiserID: account.AdvertiserID, AccessToken: lease.AccessToken,
		StartDate: startDate, EndDate: endDate,
	})
	if err != nil {
		return failedRow(account, err)
	}
	return domain.AccountReportRow{
		Channel: account.Channel, AdvertiserID: account.AdvertiserID,
		Name: account.Name, Enabled: account.Enabled, AuthAccountID: account.AuthAccountID,
		ChannelName: account.Channel.DisplayName(), QueryStatus: "ok",
		MetricBasis: metrics.MetricBasis, Spend: metrics.Spend, Orders: metrics.Orders,
		GMV: metrics.GMV, ROI: metrics.ROI, NetOrders1H: metrics.NetOrders1H,
		NetGMV1H: metrics.NetGMV1H, NetROI1H: metrics.NetROI1H,
		RequestIDs: append([]string(nil), metrics.RequestIDs...),
	}
}

func failedRow(account domain.ManagedAccount, err error) domain.AccountReportRow {
	failure := &domain.AccountReportFailure{
		Code: "api_error", Message: "account report failed",
		Details: map[string]any{"message": safeErrorMessage(err)},
	}
	var domainErr *domain.Error
	if errors.As(err, &domainErr) {
		failure.Code = domainErr.Code
		failure.Message = domainErr.Message
		failure.Details = cloneDetails(domainErr.Details)
	}
	return domain.AccountReportRow{
		Channel: account.Channel, AdvertiserID: account.AdvertiserID,
		Name: account.Name, Enabled: account.Enabled, AuthAccountID: account.AuthAccountID,
		ChannelName: account.Channel.DisplayName(), QueryStatus: "failed", Error: failure,
	}
}

func cloneDetails(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func safeErrorMessage(err error) string {
	if err == nil {
		return "unknown account report failure"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "account report request timed out"
	}
	if errors.Is(err, context.Canceled) {
		return "account report request was canceled"
	}
	return err.Error()
}
