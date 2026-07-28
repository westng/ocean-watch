package oceanengine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/oceanengine/ad_open_sdk_go/models"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	domainqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/qianchuan"
	domainreports "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/reports"
	platformretry "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/retry"
	portqianchuan "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/qianchuan"
	portreports "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/reports"
)

const maxExactJSONInteger = float64(1 << 53)

type QianchuanReportAdapter struct {
	Factory *ClientFactory
	Retry   platformretry.Policy
}

func (adapter QianchuanReportAdapter) FetchMaterialPage(
	ctx context.Context,
	request portreports.MaterialPageRequest,
) (domainreports.MaterialPage, error) {
	page, err := positiveInt32(request.Page, "page")
	if err != nil {
		return domainreports.MaterialPage{}, err
	}
	pageSize, err := positiveInt32(request.PageSize, "page_size")
	if err != nil {
		return domainreports.MaterialPage{}, err
	}
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainreports.MaterialPage{}, err
	}
	filtering, hasFiltering, err := mapMaterialFilters(request.Filters)
	if err != nil {
		return domainreports.MaterialPage{}, err
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.QianchuanReportMaterialGetV10Response, error) {
			builder := client.sdk.QianchuanReportMaterialGetV10Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).
				StartDate(request.StartDate).EndDate(request.EndDate).Fields(request.Fields).
				OrderField(request.OrderField).
				OrderType(models.QianchuanReportMaterialGetV10OrderType(request.OrderType)).
				Page(page).PageSize(pageSize)
			if hasFiltering {
				builder = builder.Filtering(filtering)
			}
			response, httpResponse, sdkErr := builder.Execute()
			if guardErr := guardMaterialReportResponse(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainreports.MaterialPage{}, err
	}
	rows := make([]domainreports.MaterialRow, 0, len(result.Data.List))
	for _, item := range result.Data.List {
		row, mapErr := mapMaterialReportRow(item)
		if mapErr != nil {
			return domainreports.MaterialPage{}, mapErr
		}
		rows = append(rows, row)
	}
	pageInfo, err := mapReportPageInfo(
		request.Page, result.Data.PageInfo.Page, result.Data.PageInfo.TotalPage,
		result.Data.PageInfo.TotalNumber,
	)
	if err != nil {
		return domainreports.MaterialPage{}, err
	}
	return domainreports.MaterialPage{
		Rows: rows, PageInfo: pageInfo, RequestID: stringValue(result.RequestId),
	}, nil
}

func (adapter QianchuanReportAdapter) FetchPlanSchema(
	ctx context.Context,
	request portreports.PlanSchemaRequest,
) (domainreports.PlanSchema, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainreports.PlanSchema{}, err
	}
	topic := models.QianchuanReportUniPromotionConfigGetV10DataTopics(request.Topic)
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.QianchuanReportUniPromotionConfigGetV10Response, error) {
			response, httpResponse, sdkErr := client.sdk.QianchuanReportUniPromotionConfigGetV10Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).
				DataTopics([]*models.QianchuanReportUniPromotionConfigGetV10DataTopics{topic.Ptr()}).Execute()
			if guardErr := guardPlanSchemaResponse(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainreports.PlanSchema{}, err
	}
	for _, config := range result.Data.CustomConfigDatas {
		if config == nil || config.DataTopic == nil || string(*config.DataTopic) != request.Topic {
			continue
		}
		dimensions := make([]string, 0, len(config.Dimensions))
		for _, item := range config.Dimensions {
			if item == nil || item.Field == nil || strings.TrimSpace(*item.Field) == "" {
				return domainreports.PlanSchema{}, errors.New("Qianchuan plan report schema contains an invalid dimension")
			}
			dimensions = append(dimensions, *item.Field)
		}
		metrics := make([]string, 0, len(config.Metrics))
		for _, item := range config.Metrics {
			if item == nil || item.Field == nil || strings.TrimSpace(*item.Field) == "" {
				return domainreports.PlanSchema{}, errors.New("Qianchuan plan report schema contains an invalid metric")
			}
			metrics = append(metrics, *item.Field)
		}
		return domainreports.PlanSchema{
			Topic: request.Topic, Dimensions: dimensions, Metrics: metrics,
			RequestID: stringValue(result.RequestId),
		}, nil
	}
	return domainreports.PlanSchema{}, fmt.Errorf("Qianchuan plan report schema omitted topic %s", request.Topic)
}

func (adapter QianchuanReportAdapter) FetchPlanMetricPage(
	ctx context.Context,
	request portreports.PlanMetricPageRequest,
) (domainreports.PlanMetricPage, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainreports.PlanMetricPage{}, err
	}
	orderField, orderType := request.OrderField, request.OrderType
	orderBy := []*models.QianchuanReportUniPromotionDataGetV10OrderByInner{{
		Field: &orderField, Type: &orderType,
	}}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.QianchuanReportUniPromotionDataGetV10Response, error) {
			response, httpResponse, sdkErr := client.sdk.QianchuanReportUniPromotionDataGetV10Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).
				DataTopic(models.QianchuanReportUniPromotionDataGetV10DataTopic(request.Topic)).
				Dimensions(request.Dimensions).Metrics(request.Metrics).
				Filters([]*models.QianchuanReportUniPromotionDataGetV10FiltersInner{}).
				StartTime(request.StartTime).EndTime(request.EndTime).OrderBy(orderBy).
				Page(int64(request.Page)).
				PageSize(models.QianchuanReportUniPromotionDataGetV10PageSize(request.PageSize)).Execute()
			if guardErr := guardPlanMetricResponse(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainreports.PlanMetricPage{}, err
	}
	rows := make([]domainreports.PlanMetricRow, 0, len(result.Data.Rows))
	for _, item := range result.Data.Rows {
		row, mapErr := mapPlanMetricRow(item, request.Metrics)
		if mapErr != nil {
			return domainreports.PlanMetricPage{}, mapErr
		}
		rows = append(rows, row)
	}
	pageInfo, err := mapReportPageInfo(
		request.Page, result.Data.PageInfo.Page, result.Data.PageInfo.TotalPage,
		result.Data.PageInfo.TotalNumber,
	)
	if err != nil {
		return domainreports.PlanMetricPage{}, err
	}
	return domainreports.PlanMetricPage{
		Rows: rows, PageInfo: pageInfo, RequestID: stringValue(result.RequestId),
	}, nil
}

func (adapter QianchuanReportAdapter) FetchPlanMetadataPage(
	ctx context.Context,
	request portreports.PlanMetadataPageRequest,
) (domainqianchuan.PlanPage, error) {
	return QianchuanReadAdapter(adapter).FetchPlans(
		ctx,
		portqianchuan.PlanPageRequest{
			AdvertiserID: request.AdvertiserID, AccessToken: request.AccessToken,
			StartTime: request.StartTime, EndTime: request.EndTime, Status: request.Status,
			MarketingGoal: request.MarketingGoal, AdlabScene: request.AdlabScene,
			NeedCompensateInfo: request.NeedCompensateInfo, Page: request.Page, PageSize: request.PageSize,
		},
	)
}

func (adapter QianchuanReportAdapter) client(advertiserID, accessToken string) (*Client, int64, error) {
	if adapter.Factory == nil {
		return nil, 0, errors.New("Qianchuan report SDK client factory is required")
	}
	parsed, err := parsePositiveID(advertiserID, "advertiser_id")
	if err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(accessToken) == "" {
		return nil, 0, errors.New("Qianchuan report access token is required")
	}
	client, err := adapter.Factory.Client("qianchuan", ProfileBusiness, TimeoutStandard)
	return client, parsed, err
}

func mapMaterialFilters(filters portreports.MaterialFilters) (models.QianchuanReportMaterialGetV10Filtering, bool, error) {
	result := models.QianchuanReportMaterialGetV10Filtering{}
	materialIDs, err := parseIDs(filters.MaterialIDs, "material_id")
	if err != nil {
		return result, false, err
	}
	result.MaterialId = materialIDs
	if filters.MaterialType != "" {
		result.MaterialType = models.QianchuanReportMaterialGetV10FilteringMaterialType(filters.MaterialType)
	}
	for _, value := range filters.MaterialMode {
		typed := models.QianchuanReportMaterialGetV10FilteringMaterialMode(value)
		result.MaterialMode = append(result.MaterialMode, typed.Ptr())
	}
	for _, value := range filters.VideoSource {
		typed := models.QianchuanReportMaterialGetV10FilteringVideoSource(value)
		result.VideoSource = append(result.VideoSource, typed.Ptr())
	}
	hasFiltering := len(result.MaterialId) != 0 || filters.MaterialType != "" ||
		len(result.MaterialMode) != 0 || len(result.VideoSource) != 0
	return result, hasFiltering, nil
}

func mapMaterialReportRow(
	item *models.QianchuanReportMaterialGetV10ResponseDataListInner,
) (domainreports.MaterialRow, error) {
	if item == nil || item.MaterialId == nil || *item.MaterialId <= 0 {
		return domainreports.MaterialRow{}, errors.New("Qianchuan material report contains an invalid material ID")
	}
	payload, err := json.Marshal(item)
	if err != nil {
		return domainreports.MaterialRow{}, fmt.Errorf("encode Qianchuan material report row: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	values := map[string]any{}
	if err := decoder.Decode(&values); err != nil {
		return domainreports.MaterialRow{}, fmt.Errorf("decode Qianchuan material report row: %w", err)
	}
	if fields, ok := values["fields"].(map[string]any); ok {
		for key, value := range fields {
			values[key] = value
		}
	}
	delete(values, "fields")
	materialID := strconv.FormatInt(*item.MaterialId, 10)
	values["material_id"] = materialID
	related := make([]string, len(item.RelatedAdIds))
	for index, value := range item.RelatedAdIds {
		if value <= 0 {
			return domainreports.MaterialRow{}, errors.New("Qianchuan material report contains an invalid related plan ID")
		}
		related[index] = strconv.FormatInt(value, 10)
	}
	values["related_ad_ids"] = related
	return domainreports.MaterialRow{MaterialID: materialID, Values: values}, nil
}

func mapPlanMetricRow(
	item *models.QianchuanReportUniPromotionDataGetV10ResponseDataRowsInner,
	fields []string,
) (domainreports.PlanMetricRow, error) {
	if item == nil {
		return domainreports.PlanMetricRow{}, errors.New("Qianchuan plan report contains a null row")
	}
	idValue, ok := item.Dimensions["ad_id"]
	if !ok {
		return domainreports.PlanMetricRow{}, errors.New("Qianchuan plan report omitted ad_id")
	}
	adID, err := dynamicID(idValue)
	if err != nil {
		return domainreports.PlanMetricRow{}, fmt.Errorf("Qianchuan plan report returned an invalid ad_id: %w", err)
	}
	metrics := make(map[string]domain.Decimal, len(fields))
	for _, field := range fields {
		container, exists := item.Metrics[field]
		if !exists {
			return domainreports.PlanMetricRow{}, fmt.Errorf("Qianchuan plan report omitted required metric %s", field)
		}
		value, exists := dynamicValue(container)
		if !exists {
			return domainreports.PlanMetricRow{}, fmt.Errorf("Qianchuan plan report omitted required metric value %s", field)
		}
		parsed, parseErr := dynamicDecimal(value)
		if parseErr != nil {
			return domainreports.PlanMetricRow{}, fmt.Errorf("Qianchuan plan report returned an invalid metric %s: %w", field, parseErr)
		}
		metrics[field] = parsed
	}
	return domainreports.PlanMetricRow{AdID: adID, Metrics: metrics}, nil
}

func dynamicValue(container map[string]interface{}) (any, bool) {
	if container == nil {
		return nil, false
	}
	for _, key := range []string{"Value", "value"} {
		if value, exists := container[key]; exists && value != nil && fmt.Sprint(value) != "" {
			return value, true
		}
	}
	for _, key := range []string{"ValueStr", "value_str"} {
		if value, exists := container[key]; exists && value != nil && fmt.Sprint(value) != "" {
			return value, true
		}
	}
	return nil, false
}

func dynamicID(container map[string]interface{}) (string, error) {
	value, exists := dynamicStringValue(container)
	if !exists {
		value, exists = dynamicNumericValue(container)
	}
	if !exists {
		return "", errors.New("value is missing")
	}
	switch typed := value.(type) {
	case string:
		if _, err := parsePositiveID(typed, "ad_id"); err != nil {
			return "", err
		}
		return typed, nil
	case float64:
		if !isFinite(typed) || typed <= 0 || typed != math.Trunc(typed) || typed > maxExactJSONInteger {
			return "", errors.New("numeric value is not an exact positive ID")
		}
		return strconv.FormatFloat(typed, 'f', 0, 64), nil
	case json.Number:
		text := typed.String()
		if _, err := parsePositiveID(text, "ad_id"); err != nil {
			return "", err
		}
		return text, nil
	default:
		text := fmt.Sprint(value)
		if _, err := parsePositiveID(text, "ad_id"); err != nil {
			return "", err
		}
		return text, nil
	}
}

func dynamicStringValue(container map[string]interface{}) (any, bool) {
	if container == nil {
		return nil, false
	}
	for _, key := range []string{"ValueStr", "value_str"} {
		if value, exists := container[key]; exists && value != nil && fmt.Sprint(value) != "" {
			return value, true
		}
	}
	return nil, false
}

func dynamicNumericValue(container map[string]interface{}) (any, bool) {
	if container == nil {
		return nil, false
	}
	for _, key := range []string{"Value", "value"} {
		if value, exists := container[key]; exists && value != nil && fmt.Sprint(value) != "" {
			return value, true
		}
	}
	return nil, false
}

func dynamicDecimal(value any) (domain.Decimal, error) {
	var text string
	switch typed := value.(type) {
	case float64:
		if !isFinite(typed) {
			return domain.Decimal{}, errors.New("value is not finite")
		}
		text = strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		text = typed.String()
	case string:
		text = typed
	default:
		text = fmt.Sprint(value)
	}
	return domain.ParseDecimal(text)
}

func isFinite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func mapReportPageInfo(
	expected int,
	page *int64,
	totalPages *int64,
	totalNumber *int64,
) (domainqianchuan.PageInfo, error) {
	return mapInt64PageInfo(expected, page, totalPages, totalNumber)
}

func guardMaterialReportResponse(
	response *models.QianchuanReportMaterialGetV10Response,
	httpResponse *http.Response,
	sdkErr error,
) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	hasData := response.Data != nil && response.Data.PageInfo.Page != nil &&
		response.Data.PageInfo.TotalPage != nil && response.Data.PageInfo.TotalNumber != nil
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, hasData)
}

func guardPlanSchemaResponse(
	response *models.QianchuanReportUniPromotionConfigGetV10Response,
	httpResponse *http.Response,
	sdkErr error,
) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil)
}

func guardPlanMetricResponse(
	response *models.QianchuanReportUniPromotionDataGetV10Response,
	httpResponse *http.Response,
	sdkErr error,
) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	hasData := response.Data != nil && response.Data.PageInfo != nil
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, hasData)
}
