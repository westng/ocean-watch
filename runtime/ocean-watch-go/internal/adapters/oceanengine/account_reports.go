package oceanengine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/oceanengine/ad_open_sdk_go/models"
	applicationaccounts "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/accounts"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
)

var marketingAccountMetrics = []string{
	"stat_cost", "in_app_order_count", "in_app_order_gmv", "in_app_order_roi",
	"in_app_order_net_count_1h", "in_app_order_net_gmv_1h", "in_app_order_net_roi_1h",
}

var qianchuanAccountMetrics = []string{
	"stat_cost", "total_pay_order_count_for_roi2",
	"total_pay_order_gmv_include_coupon_for_roi2", "total_prepay_and_pay_order_roi2",
}

type MarketingAccountReportAdapter struct {
	Factory *ClientFactory
	Retry   platformretry.Policy
}

type QianchuanAccountReportAdapter struct {
	Factory *ClientFactory
	Retry   platformretry.Policy
}

func (adapter MarketingAccountReportAdapter) QueryAccount(
	ctx context.Context,
	request applicationaccounts.ReportRequest,
) (domain.AccountMetrics, error) {
	client, advertiserID, err := reportClient(adapter.Factory, domain.Marketing, request)
	if err != nil {
		return domain.AccountMetrics{}, err
	}
	response, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.ReportCustomGetV30Response, error) {
			orderType := models.DESC_ReportCustomGetV30OrderByType
			result, httpResponse, sdkErr := client.sdk.ReportCustomGetV30Api().Get(ctx).
				Dimensions([]string{}).
				AdvertiserId(advertiserID).
				Metrics(marketingAccountMetrics).
				Filters([]*models.ReportCustomGetV30FiltersInner{}).
				StartTime(request.StartDate + " 00:00:00").
				EndTime(request.EndDate + " 23:59:59").
				OrderBy([]*models.ReportCustomGetV30OrderByInner{{Field: "stat_cost", Type: &orderType}}).
				Page(1).PageSize(100).
				DataTopic(models.BASIC_DATA_ReportCustomGetV30DataTopic).
				AccessToken(request.AccessToken).
				Execute()
			if guardErr := guardMarketingReport(result, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return result, nil
		},
	)
	if err != nil {
		return domain.AccountMetrics{}, reportDomainError(domain.Marketing, err)
	}
	metrics := response.Data.TotalMetrics
	if len(metrics) == 0 && len(response.Data.Rows) != 0 && response.Data.Rows[0] != nil {
		metrics = response.Data.Rows[0].Metrics
	}
	return mapMarketingAccountMetrics(metrics, response.RequestId)
}

func (adapter QianchuanAccountReportAdapter) QueryAccount(
	ctx context.Context,
	request applicationaccounts.ReportRequest,
) (domain.AccountMetrics, error) {
	client, advertiserID, err := reportClient(adapter.Factory, domain.Qianchuan, request)
	if err != nil {
		return domain.AccountMetrics{}, err
	}
	response, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.QianchuanReportUniPromotionGetV10Response, error) {
			result, httpResponse, sdkErr := client.sdk.QianchuanReportUniPromotionGetV10Api().Get(ctx).
				AdvertiserId(advertiserID).
				StartDate(request.StartDate + " 00:00:00").
				EndDate(request.EndDate + " 23:59:59").
				Fields(qianchuanAccountMetrics).
				MarketingGoal(models.ALL_QianchuanReportUniPromotionGetV10MarketingGoal).
				OrderPlatform(models.QIANCHUAN_QianchuanReportUniPromotionGetV10OrderPlatform).
				AccessToken(request.AccessToken).
				Execute()
			if guardErr := guardQianchuanReport(result, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return result, nil
		},
	)
	if err != nil {
		return domain.AccountMetrics{}, reportDomainError(domain.Qianchuan, err)
	}
	return mapQianchuanAccountMetrics(response.Data, response.RequestId)
}

func reportClient(
	factory *ClientFactory,
	channel domain.Channel,
	request applicationaccounts.ReportRequest,
) (*Client, int64, error) {
	if factory == nil {
		return nil, 0, errors.New("account report SDK client factory is required")
	}
	if err := domain.ValidateDecimalID(request.AdvertiserID, "advertiser_id"); err != nil {
		return nil, 0, err
	}
	advertiserID, err := strconv.ParseInt(request.AdvertiserID, 10, 64)
	if err != nil {
		return nil, 0, errors.New("advertiser_id exceeds SDK int64 range")
	}
	if strings.TrimSpace(request.AccessToken) == "" {
		return nil, 0, errors.New("account report access token is required")
	}
	if request.StartDate == "" || request.EndDate == "" {
		return nil, 0, errors.New("account report date range is required")
	}
	client, err := factory.Client(string(channel), ProfileBusiness, TimeoutStandard)
	return client, advertiserID, err
}

func readPolicy(policy platformretry.Policy) platformretry.Policy {
	if policy.Delays == nil {
		policy.Delays = append([]time.Duration(nil), DefaultReadRetryDelays...)
	}
	if policy.MaxRetryAfter == 0 {
		policy.MaxRetryAfter = DefaultMaxRetryAfter
	}
	if policy.Jitter == nil {
		policy.Jitter = defaultReadJitter
	}
	return policy
}

func guardMarketingReport(
	response *models.ReportCustomGetV30Response,
	httpResponse *http.Response,
	sdkErr error,
) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(
		httpResponse, sdkErr, response.Code, response.Message, response.RequestId,
		true, response.Data != nil,
	)
}

func guardQianchuanReport(
	response *models.QianchuanReportUniPromotionGetV10Response,
	httpResponse *http.Response,
	sdkErr error,
) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(
		httpResponse, sdkErr, response.Code, response.Message, response.RequestId,
		true, response.Data != nil,
	)
}

func mapMarketingAccountMetrics(values map[string]string, requestID *string) (domain.AccountMetrics, error) {
	parsed := map[string]domain.Decimal{}
	for _, field := range marketingAccountMetrics {
		value, ok := values[field]
		if !ok || strings.TrimSpace(value) == "" {
			return domain.AccountMetrics{}, fmt.Errorf("Marketing account report is missing metric %s", field)
		}
		decimalValue, err := domain.ParseDecimal(value)
		if err != nil {
			return domain.AccountMetrics{}, fmt.Errorf("Marketing account report metric %s is invalid", field)
		}
		parsed[field] = decimalValue
	}
	orders, err := parsed["in_app_order_count"].Int64Exact()
	if err != nil {
		return domain.AccountMetrics{}, errors.New("Marketing account report order count is not an integer")
	}
	netOrders, err := parsed["in_app_order_net_count_1h"].Int64Exact()
	if err != nil {
		return domain.AccountMetrics{}, errors.New("Marketing account report one-hour order count is not an integer")
	}
	netGMV := parsed["in_app_order_net_gmv_1h"].Round(2)
	netROI := parsed["in_app_order_net_roi_1h"].Round(4)
	return domain.AccountMetrics{
		MetricBasis: cloneMetricBasis(domain.MarketingMetricBasis),
		Spend:       parsed["stat_cost"].Round(2), Orders: orders,
		GMV: parsed["in_app_order_gmv"].Round(2), ROI: parsed["in_app_order_roi"].Round(4),
		NetOrders1H: &netOrders, NetGMV1H: &netGMV, NetROI1H: &netROI,
		RequestIDs: requestIDs(requestID),
	}, nil
}

func mapQianchuanAccountMetrics(
	values *models.QianchuanReportUniPromotionGetV10ResponseData,
	requestID *string,
) (domain.AccountMetrics, error) {
	if values == nil || values.StatCost == nil || values.TotalPayOrderCountForRoi2 == nil ||
		values.TotalPayOrderGmvIncludeCouponForRoi2 == nil || values.TotalPrepayAndPayOrderRoi2 == nil {
		return domain.AccountMetrics{}, errors.New("Qianchuan account report is missing required metrics")
	}
	spend, err := domain.DecimalFromFloat64(*values.StatCost)
	if err != nil {
		return domain.AccountMetrics{}, errors.New("Qianchuan account spend is invalid")
	}
	ordersDecimal, err := domain.DecimalFromFloat64(*values.TotalPayOrderCountForRoi2)
	if err != nil {
		return domain.AccountMetrics{}, errors.New("Qianchuan account order count is invalid")
	}
	orders, err := ordersDecimal.Int64Exact()
	if err != nil {
		return domain.AccountMetrics{}, errors.New("Qianchuan account order count is not an integer")
	}
	gmv, err := domain.DecimalFromFloat64(*values.TotalPayOrderGmvIncludeCouponForRoi2)
	if err != nil {
		return domain.AccountMetrics{}, errors.New("Qianchuan account GMV is invalid")
	}
	roi, err := domain.DecimalFromFloat64(*values.TotalPrepayAndPayOrderRoi2)
	if err != nil {
		return domain.AccountMetrics{}, errors.New("Qianchuan account ROI is invalid")
	}
	return domain.AccountMetrics{
		MetricBasis: cloneMetricBasis(domain.QianchuanMetricBasis),
		Spend:       spend.Round(2), Orders: orders, GMV: gmv.Round(2), ROI: roi.Round(4),
		RequestIDs: requestIDs(requestID),
	}, nil
}

func reportDomainError(channel domain.Channel, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, requestcontrol.ErrAuthorizationScopeMissing) ||
		errors.Is(err, requestcontrol.ErrRequestBudgetMissing) ||
		errors.Is(err, requestcontrol.ErrRequestBudgetExceeded) {
		return err
	}
	details := map[string]any{}
	var envelope *EnvelopeError
	if errors.As(err, &envelope) {
		details["code"] = envelope.Code
		details["http_status"] = envelope.HTTPStatus
		details["message"] = Redact(envelope.Message)
		if envelope.RequestID != "" {
			details["request_id"] = envelope.RequestID
		}
	} else {
		details["message"] = Redact(err.Error())
	}
	message := "Marketing account report failed"
	if channel == domain.Qianchuan {
		message = "Qianchuan account report failed"
	}
	return domain.NewError("api_error", message, 1, details)
}

func cloneMetricBasis(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func requestIDs(requestID *string) []string {
	if requestID == nil || strings.TrimSpace(*requestID) == "" {
		return []string{}
	}
	return []string{*requestID}
}
