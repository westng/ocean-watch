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

func (adapter QianchuanReportAdapter) FetchSchemas(
	ctx context.Context,
	request portreports.SchemaRequest,
) ([]domainreports.QianchuanSchema, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return nil, err
	}
	topics := make([]*models.QianchuanReportUniPromotionConfigGetV10DataTopics, len(request.Topics))
	for index, topic := range request.Topics {
		typed := models.QianchuanReportUniPromotionConfigGetV10DataTopics(topic)
		topics[index] = typed.Ptr()
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.QianchuanReportUniPromotionConfigGetV10Response, error) {
			builder := client.sdk.QianchuanReportUniPromotionConfigGetV10Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).
				DataTopics(topics)
			if request.DataPeriod != "" {
				builder = builder.DataPeriod(models.QianchuanReportUniPromotionConfigGetV10DataPeriod(request.DataPeriod))
			}
			response, httpResponse, sdkErr := builder.Execute()
			if guardErr := guardPlanSchemaResponse(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return nil, err
	}
	schemasByTopic := make(map[string]domainreports.QianchuanSchema, len(result.Data.CustomConfigDatas))
	for _, config := range result.Data.CustomConfigDatas {
		if config == nil || config.DataTopic == nil {
			continue
		}
		dimensions := make([]string, 0, len(config.Dimensions))
		for _, item := range config.Dimensions {
			if item == nil || item.Field == nil || strings.TrimSpace(*item.Field) == "" {
				return nil, errors.New("Qianchuan report schema contains an invalid dimension")
			}
			dimensions = append(dimensions, *item.Field)
		}
		metrics := make([]string, 0, len(config.Metrics))
		for _, item := range config.Metrics {
			if item == nil || item.Field == nil || strings.TrimSpace(*item.Field) == "" {
				return nil, errors.New("Qianchuan report schema contains an invalid metric")
			}
			metrics = append(metrics, *item.Field)
		}
		topic := string(*config.DataTopic)
		schemasByTopic[topic] = domainreports.QianchuanSchema{
			Topic: topic, Dimensions: dimensions, Metrics: metrics,
			RequestID: stringValue(result.RequestId),
		}
	}
	schemas := make([]domainreports.QianchuanSchema, 0, len(request.Topics))
	for _, topic := range request.Topics {
		schema, ok := schemasByTopic[topic]
		if !ok {
			return nil, fmt.Errorf("Qianchuan report schema omitted topic %s", topic)
		}
		schemas = append(schemas, schema)
	}
	return schemas, nil
}

func (adapter QianchuanReportAdapter) FetchPlanSchema(
	ctx context.Context,
	request portreports.PlanSchemaRequest,
) (domainreports.PlanSchema, error) {
	schemas, err := adapter.FetchSchemas(ctx, portreports.SchemaRequest{
		AdvertiserID: request.AdvertiserID, AccessToken: request.AccessToken,
		Topics: []string{request.Topic},
	})
	if err != nil {
		return domainreports.PlanSchema{}, err
	}
	return schemas[0], nil
}

func (adapter QianchuanReportAdapter) FetchDataPage(
	ctx context.Context,
	request portreports.DataPageRequest,
) (domainreports.QianchuanReportPage, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainreports.QianchuanReportPage{}, err
	}
	orderField, orderType := request.OrderField, request.OrderType
	orderBy := []*models.QianchuanReportUniPromotionDataGetV10OrderByInner{{
		Field: &orderField, Type: &orderType,
	}}
	filters := make([]*models.QianchuanReportUniPromotionDataGetV10FiltersInner, len(request.Filters))
	for index, filter := range request.Filters {
		filters[index] = &models.QianchuanReportUniPromotionDataGetV10FiltersInner{
			Field: filter.Field, Operator: filter.Operator, Values: append([]string(nil), filter.Values...),
		}
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.QianchuanReportUniPromotionDataGetV10Response, error) {
			builder := client.sdk.QianchuanReportUniPromotionDataGetV10Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).
				DataTopic(models.QianchuanReportUniPromotionDataGetV10DataTopic(request.Topic)).
				Dimensions(request.Dimensions).Metrics(request.Metrics).
				Filters(filters).
				StartTime(request.StartTime).EndTime(request.EndTime).OrderBy(orderBy).
				Page(int64(request.Page)).
				PageSize(models.QianchuanReportUniPromotionDataGetV10PageSize(request.PageSize))
			if request.DataPeriod != "" {
				builder = builder.DataPeriod(models.QianchuanReportUniPromotionDataGetV10DataPeriod(request.DataPeriod))
			}
			response, httpResponse, sdkErr := builder.Execute()
			if guardErr := guardPlanMetricResponse(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainreports.QianchuanReportPage{}, err
	}
	rows := make([]domainreports.QianchuanReportRow, 0, len(result.Data.Rows))
	for _, item := range result.Data.Rows {
		row, mapErr := mapQianchuanReportRow(item, request.Dimensions, request.Metrics)
		if mapErr != nil {
			return domainreports.QianchuanReportPage{}, mapErr
		}
		rows = append(rows, row)
	}
	pageInfo, err := mapReportPageInfo(
		request.Page, result.Data.PageInfo.Page, result.Data.PageInfo.TotalPage,
		result.Data.PageInfo.TotalNumber,
	)
	if err != nil {
		return domainreports.QianchuanReportPage{}, err
	}
	return domainreports.QianchuanReportPage{
		Rows: rows, PageInfo: pageInfo, RequestID: stringValue(result.RequestId),
	}, nil
}

func (adapter QianchuanReportAdapter) FetchPlanMetricPage(
	ctx context.Context,
	request portreports.PlanMetricPageRequest,
) (domainreports.PlanMetricPage, error) {
	page, err := adapter.FetchDataPage(ctx, portreports.DataPageRequest{
		AdvertiserID: request.AdvertiserID, AccessToken: request.AccessToken,
		Topic: request.Topic, Dimensions: request.Dimensions, Metrics: request.Metrics,
		StartTime: request.StartTime, EndTime: request.EndTime,
		OrderField: request.OrderField, OrderType: request.OrderType,
		Page: request.Page, PageSize: request.PageSize,
	})
	if err != nil {
		return domainreports.PlanMetricPage{}, err
	}
	rows := make([]domainreports.PlanMetricRow, len(page.Rows))
	for index, row := range page.Rows {
		mapped, mapErr := planMetricRow(row, request.Metrics)
		if mapErr != nil {
			return domainreports.PlanMetricPage{}, mapErr
		}
		rows[index] = mapped
	}
	return domainreports.PlanMetricPage{
		Rows: rows, PageInfo: page.PageInfo, RequestID: page.RequestID,
	}, nil
}

func (adapter QianchuanReportAdapter) FetchAllPromotion(
	ctx context.Context,
	request portreports.AggregateRequest,
) (domainreports.QianchuanAggregate, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainreports.QianchuanAggregate{}, err
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.QianchuanReportAllPromotionGetV10Response, error) {
			builder := client.sdk.QianchuanReportAllPromotionGetV10Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).
				StartTime(request.StartTime).EndTime(request.EndTime).Fields(request.Fields).
				AdlabScene(models.QianchuanReportAllPromotionGetV10AdlabScene(request.AdlabScene)).
				MarketingGoal(models.QianchuanReportAllPromotionGetV10MarketingGoal(request.MarketingGoal)).
				OrderPlatform(models.QianchuanReportAllPromotionGetV10OrderPlatform(request.OrderPlatform))
			if request.DataPeriod != "" {
				builder = builder.DataPeriod(models.QianchuanReportAllPromotionGetV10DataPeriod(request.DataPeriod))
			}
			response, httpResponse, sdkErr := builder.Execute()
			if guardErr := guardAllPromotionResponse(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainreports.QianchuanAggregate{}, err
	}
	values, err := modelValues(result.Data)
	if err != nil {
		return domainreports.QianchuanAggregate{}, err
	}
	return domainreports.QianchuanAggregate{Values: values, RequestID: stringValue(result.RequestId)}, nil
}

func (adapter QianchuanReportAdapter) FetchUniPromotion(
	ctx context.Context,
	request portreports.AggregateRequest,
) (domainreports.QianchuanAggregate, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainreports.QianchuanAggregate{}, err
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.QianchuanReportUniPromotionGetV10Response, error) {
			response, httpResponse, sdkErr := client.sdk.QianchuanReportUniPromotionGetV10Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).
				StartDate(request.StartTime).EndDate(request.EndTime).Fields(request.Fields).
				MarketingGoal(models.QianchuanReportUniPromotionGetV10MarketingGoal(request.MarketingGoal)).
				OrderPlatform(models.QianchuanReportUniPromotionGetV10OrderPlatform(request.OrderPlatform)).Execute()
			if guardErr := guardUniPromotionResponse(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainreports.QianchuanAggregate{}, err
	}
	values, err := modelValues(result.Data)
	if err != nil {
		return domainreports.QianchuanAggregate{}, err
	}
	return domainreports.QianchuanAggregate{Values: values, RequestID: stringValue(result.RequestId)}, nil
}

func (adapter QianchuanReportAdapter) FetchRoomDimensionPage(
	ctx context.Context,
	request portreports.DimensionPageRequest,
) (domainreports.QianchuanDimensionPage, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainreports.QianchuanDimensionPage{}, err
	}
	dimensionID, err := parsePositiveID(request.DimensionID, "room_id")
	if err != nil {
		return domainreports.QianchuanDimensionPage{}, err
	}
	page, err := positiveInt32(request.Page, "page")
	if err != nil {
		return domainreports.QianchuanDimensionPage{}, err
	}
	pageSize, err := positiveInt32(request.PageSize, "page_size")
	if err != nil {
		return domainreports.QianchuanDimensionPage{}, err
	}
	metrics := make([]*models.QianchuanReportUniPromotionDimensionDataRoomGetV10Metrics, len(request.Metrics))
	for index, metric := range request.Metrics {
		typed := models.QianchuanReportUniPromotionDimensionDataRoomGetV10Metrics(metric)
		metrics[index] = typed.Ptr()
	}
	filtering := models.QianchuanReportUniPromotionDimensionDataRoomGetV10Filtering{}
	if request.OrderPlatform != "" {
		value := models.QianchuanReportUniPromotionDimensionDataRoomGetV10FilteringOrderPlatform(request.OrderPlatform)
		filtering.OrderPlatform = value.Ptr()
	}
	if request.SmartBidType != "" {
		value := models.QianchuanReportUniPromotionDimensionDataRoomGetV10FilteringSmartBidType(request.SmartBidType)
		filtering.SmartBidType = value.Ptr()
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.QianchuanReportUniPromotionDimensionDataRoomGetV10Response, error) {
			response, httpResponse, sdkErr := client.sdk.QianchuanReportUniPromotionDimensionDataRoomGetV10Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).RoomId(dimensionID).
				StartTime(request.StartTime).EndTime(request.EndTime).
				Dimension(models.QianchuanReportUniPromotionDimensionDataRoomGetV10Dimension(request.Dimension)).
				Metrics(metrics).OrderField(request.OrderField).
				OrderType(models.QianchuanReportUniPromotionDimensionDataRoomGetV10OrderType(request.OrderType)).
				Page(page).PageSize(pageSize).Filtering(filtering).Execute()
			if guardErr := guardRoomDimensionResponse(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainreports.QianchuanDimensionPage{}, err
	}
	rows, err := modelRows(result.Data.List)
	if err != nil {
		return domainreports.QianchuanDimensionPage{}, err
	}
	info, err := mapReportPageInfo(request.Page, result.Data.PageInfo.Page, result.Data.PageInfo.TotalPage, result.Data.PageInfo.TotalNumber)
	return domainreports.QianchuanDimensionPage{Rows: rows, PageInfo: info, RequestID: stringValue(result.RequestId)}, err
}

func (adapter QianchuanReportAdapter) FetchAuthorDimensionPage(
	ctx context.Context,
	request portreports.DimensionPageRequest,
) (domainreports.QianchuanDimensionPage, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainreports.QianchuanDimensionPage{}, err
	}
	dimensionID, err := parsePositiveID(request.DimensionID, "aweme_id")
	if err != nil {
		return domainreports.QianchuanDimensionPage{}, err
	}
	page, err := positiveInt32(request.Page, "page")
	if err != nil {
		return domainreports.QianchuanDimensionPage{}, err
	}
	pageSize, err := positiveInt32(request.PageSize, "page_size")
	if err != nil {
		return domainreports.QianchuanDimensionPage{}, err
	}
	metrics := make([]*models.QianchuanReportUniPromotionDimensionDataAuthorGetV10Metrics, len(request.Metrics))
	for index, metric := range request.Metrics {
		typed := models.QianchuanReportUniPromotionDimensionDataAuthorGetV10Metrics(metric)
		metrics[index] = typed.Ptr()
	}
	filtering := models.QianchuanReportUniPromotionDimensionDataAuthorGetV10Filtering{}
	if request.OrderPlatform != "" {
		value := models.QianchuanReportUniPromotionDimensionDataAuthorGetV10FilteringOrderPlatform(request.OrderPlatform)
		filtering.OrderPlatform = value.Ptr()
	}
	if request.SmartBidType != "" {
		value := models.QianchuanReportUniPromotionDimensionDataAuthorGetV10FilteringSmartBidType(request.SmartBidType)
		filtering.SmartBidType = value.Ptr()
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.QianchuanReportUniPromotionDimensionDataAuthorGetV10Response, error) {
			response, httpResponse, sdkErr := client.sdk.QianchuanReportUniPromotionDimensionDataAuthorGetV10Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).AwemeId(dimensionID).
				MarketingGoal(models.QianchuanReportUniPromotionDimensionDataAuthorGetV10MarketingGoal(request.MarketingGoal)).
				Metrics(metrics).StartTime(request.StartTime).EndTime(request.EndTime).
				Dimension(models.QianchuanReportUniPromotionDimensionDataAuthorGetV10Dimension(request.Dimension)).
				OrderType(models.QianchuanReportUniPromotionDimensionDataAuthorGetV10OrderType(request.OrderType)).
				OrderField(request.OrderField).Filtering(filtering).Page(page).PageSize(pageSize).Execute()
			if guardErr := guardAuthorDimensionResponse(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainreports.QianchuanDimensionPage{}, err
	}
	rows, err := modelRows(result.Data.List)
	if err != nil {
		return domainreports.QianchuanDimensionPage{}, err
	}
	info, err := mapReportPageInfo(request.Page, result.Data.PageInfo.Page, result.Data.PageInfo.TotalPage, result.Data.PageInfo.TotalNumber)
	return domainreports.QianchuanDimensionPage{Rows: rows, PageInfo: info, RequestID: stringValue(result.RequestId)}, err
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

func mapQianchuanReportRow(
	item *models.QianchuanReportUniPromotionDataGetV10ResponseDataRowsInner,
	dimensionFields []string,
	metricFields []string,
) (domainreports.QianchuanReportRow, error) {
	if item == nil {
		return domainreports.QianchuanReportRow{}, errors.New("Qianchuan report contains a null row")
	}
	dimensions := make(map[string]any, len(dimensionFields))
	for _, field := range dimensionFields {
		container, exists := item.Dimensions[field]
		if !exists {
			return domainreports.QianchuanReportRow{}, fmt.Errorf("Qianchuan report omitted required dimension %s", field)
		}
		value, exists := dynamicStringValue(container)
		if !exists {
			value, exists = dynamicNumericValue(container)
		}
		if !exists {
			return domainreports.QianchuanReportRow{}, fmt.Errorf("Qianchuan report omitted required dimension value %s", field)
		}
		dimensions[field] = value
	}
	metrics := make(map[string]any, len(metricFields))
	for _, field := range metricFields {
		container, exists := item.Metrics[field]
		if !exists {
			return domainreports.QianchuanReportRow{}, fmt.Errorf("Qianchuan report omitted required metric %s", field)
		}
		value, exists := dynamicValue(container)
		if !exists {
			return domainreports.QianchuanReportRow{}, fmt.Errorf("Qianchuan report omitted required metric value %s", field)
		}
		metrics[field] = value
	}
	return domainreports.QianchuanReportRow{Dimensions: dimensions, Metrics: metrics}, nil
}

func planMetricRow(row domainreports.QianchuanReportRow, fields []string) (domainreports.PlanMetricRow, error) {
	idContainer := map[string]interface{}{"ValueStr": row.Dimensions["ad_id"]}
	adID, err := dynamicID(idContainer)
	if err != nil {
		return domainreports.PlanMetricRow{}, fmt.Errorf("Qianchuan plan report returned an invalid ad_id: %w", err)
	}
	metrics := make(map[string]domain.Decimal, len(fields))
	for _, field := range fields {
		value, exists := row.Metrics[field]
		if !exists {
			return domainreports.PlanMetricRow{}, fmt.Errorf("Qianchuan plan report omitted required metric %s", field)
		}
		parsed, parseErr := dynamicDecimal(value)
		if parseErr != nil {
			return domainreports.PlanMetricRow{}, fmt.Errorf("Qianchuan plan report returned an invalid metric %s: %w", field, parseErr)
		}
		metrics[field] = parsed
	}
	return domainreports.PlanMetricRow{AdID: adID, Metrics: metrics}, nil
}

func modelValues(value any) (map[string]any, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode Qianchuan report model: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	result := map[string]any{}
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode Qianchuan report model: %w", err)
	}
	return result, nil
}

func modelRows[T any](values []*T) ([]domainreports.QianchuanDimensionRow, error) {
	rows := make([]domainreports.QianchuanDimensionRow, 0, len(values))
	for _, value := range values {
		if value == nil {
			return nil, errors.New("Qianchuan dimension report contains a null row")
		}
		mapped, err := modelValues(value)
		if err != nil {
			return nil, err
		}
		rows = append(rows, domainreports.QianchuanDimensionRow{Values: mapped})
	}
	return rows, nil
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

func guardAllPromotionResponse(
	response *models.QianchuanReportAllPromotionGetV10Response,
	httpResponse *http.Response,
	sdkErr error,
) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil)
}

func guardUniPromotionResponse(
	response *models.QianchuanReportUniPromotionGetV10Response,
	httpResponse *http.Response,
	sdkErr error,
) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil)
}

func guardRoomDimensionResponse(
	response *models.QianchuanReportUniPromotionDimensionDataRoomGetV10Response,
	httpResponse *http.Response,
	sdkErr error,
) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	hasData := response.Data != nil && response.Data.PageInfo != nil
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, hasData)
}

func guardAuthorDimensionResponse(
	response *models.QianchuanReportUniPromotionDimensionDataAuthorGetV10Response,
	httpResponse *http.Response,
	sdkErr error,
) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	hasData := response.Data != nil && response.Data.PageInfo != nil
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, hasData)
}
