package oceanengine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/oceanengine/ad_open_sdk_go/models"
	domainreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/reports"
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
	portreports "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/reports"
)

type MarketingReportsAdapter struct {
	Factory *ClientFactory
	Retry   platformretry.Policy
}

var marketingReportTopics = map[string]struct{}{
	"BASIC_DATA": {}, "BIDWORD_DATA": {}, "CREATIVE_DATA": {}, "DMP_DATA": {},
	"DPA_VIDEO_DATA": {}, "MATERIAL_BOOST_DATA": {}, "MATERIAL_DATA": {},
	"ONE_KEY_BOOST_DATA": {}, "PRODUCT_DATA": {}, "QUERY_DATA": {},
	"STD_BASIC_DATA": {}, "STD_BIDWORD_DATA": {}, "STD_DMP_DATA": {},
	"STD_MATERIAL_DATA": {}, "STD_PRODUCT_DATA": {}, "STD_QUERY_DATA": {},
	"UNI_PROJECT_DATA": {}, "UNI_PROJECT_MATERIAL_DATA": {}, "VIDEO_DUARATION_DATA": {},
}

func (adapter MarketingReportsAdapter) FetchSchema(
	ctx context.Context,
	request portreports.MarketingSchemaRequest,
) (domainreports.MarketingSchema, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainreports.MarketingSchema{}, err
	}
	if len(request.DataTopics) < 1 || len(request.DataTopics) > 10 {
		return domainreports.MarketingSchema{}, errors.New("Marketing report schema requires 1 to 10 data topics")
	}
	topics := make([]*models.ReportCustomConfigGetV30DataTopics, len(request.DataTopics))
	for index, topic := range request.DataTopics {
		if _, ok := marketingReportTopics[topic]; !ok {
			return domainreports.MarketingSchema{}, fmt.Errorf("unsupported Marketing report data topic %q", topic)
		}
		value := models.ReportCustomConfigGetV30DataTopics(topic)
		topics[index] = &value
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.ReportCustomConfigGetV30Response, error) {
			response, httpResponse, sdkErr := client.sdk.ReportCustomConfigGetV30Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).DataTopics(topics).Execute()
			if guardErr := guardMarketingReportSchema(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainreports.MarketingSchema{}, err
	}
	mapped := make([]domainreports.MarketingTopic, 0, len(result.Data.List))
	for _, topic := range result.Data.List {
		if topic == nil || topic.DataTopic == nil {
			return domainreports.MarketingSchema{}, errors.New("Marketing report schema contains a topic without data_topic")
		}
		dimensions := make([]domainreports.MarketingField, 0, len(topic.Dimensions))
		for _, field := range topic.Dimensions {
			if field == nil || strings.TrimSpace(stringValue(field.Field)) == "" {
				return domainreports.MarketingSchema{}, errors.New("Marketing report schema contains an invalid dimension")
			}
			exclusions := append([]string(nil), field.ExclusionDims...)
			exclusions = append(exclusions, field.ExclusionMetrics...)
			dimensions = append(dimensions, domainreports.MarketingField{
				Field: stringValue(field.Field), Name: stringValue(field.Name),
				Description: stringValue(field.Description), SortAble: field.SortAble,
				FilterAble: field.FilterAble, Exclusions: exclusions,
			})
		}
		metrics := make([]domainreports.MarketingField, 0, len(topic.Metrics))
		for _, field := range topic.Metrics {
			if field == nil || strings.TrimSpace(stringValue(field.Field)) == "" {
				return domainreports.MarketingSchema{}, errors.New("Marketing report schema contains an invalid metric")
			}
			metrics = append(metrics, domainreports.MarketingField{
				Field: stringValue(field.Field), Name: stringValue(field.Name),
				Description: stringValue(field.Description),
				Exclusions:  append([]string(nil), field.ExclusionDims...),
			})
		}
		mapped = append(mapped, domainreports.MarketingTopic{
			DataTopic: string(*topic.DataTopic), Dimensions: dimensions, Metrics: metrics,
		})
	}
	response, err := reportResponseMap(result)
	if err != nil {
		return domainreports.MarketingSchema{}, err
	}
	return domainreports.MarketingSchema{
		Topics: mapped, RequestID: stringValue(result.RequestId), Message: stringValue(result.Message),
		Response: response,
	}, nil
}

func (adapter MarketingReportsAdapter) FetchReportPage(
	ctx context.Context,
	request portreports.MarketingReportPageRequest,
) (domainreports.MarketingReportPage, error) {
	sdkPage, err := positiveInt32(request.Page, "page")
	if err != nil {
		return domainreports.MarketingReportPage{}, err
	}
	sdkPageSize, err := positiveInt32(request.PageSize, "page_size")
	if err != nil {
		return domainreports.MarketingReportPage{}, err
	}
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainreports.MarketingReportPage{}, err
	}
	if _, ok := marketingReportTopics[request.DataTopic]; !ok {
		return domainreports.MarketingReportPage{}, fmt.Errorf("unsupported Marketing report data topic %q", request.DataTopic)
	}
	if len(request.Dimensions) == 0 || len(request.Metrics) == 0 {
		return domainreports.MarketingReportPage{}, errors.New("Marketing report dimensions and metrics are required")
	}
	if request.Page < 1 || request.PageSize < 1 || request.PageSize > 100 {
		return domainreports.MarketingReportPage{}, errors.New("Marketing report page must be positive and page_size at most 100")
	}
	if request.OrderType != "ASC" && request.OrderType != "DESC" {
		return domainreports.MarketingReportPage{}, errors.New("Marketing report order type must be ASC or DESC")
	}
	filters := make([]*models.ReportCustomGetV30FiltersInner, len(request.Filters))
	for index, filter := range request.Filters {
		operator := filter.Operator
		filters[index] = &models.ReportCustomGetV30FiltersInner{
			Field: filter.Field, Type: filter.Type, Operator: &operator,
			Values: append([]string(nil), filter.Values...),
		}
	}
	orderType := models.ReportCustomGetV30OrderByType(request.OrderType)
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.ReportCustomGetV30Response, error) {
			response, httpResponse, sdkErr := client.sdk.ReportCustomGetV30Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).
				Dimensions(request.Dimensions).Metrics(request.Metrics).Filters(filters).
				StartTime(request.StartTime).EndTime(request.EndTime).
				OrderBy([]*models.ReportCustomGetV30OrderByInner{{Field: request.OrderField, Type: &orderType}}).
				Page(sdkPage).PageSize(sdkPageSize).
				DataTopic(models.ReportCustomGetV30DataTopic(request.DataTopic)).Execute()
			if guardErr := guardMarketingReport(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainreports.MarketingReportPage{}, err
	}
	page, pageSize, totalPages, totalNumber, err := marketingReportPageInfo(
		result.Data.PageInfo, request.Page, request.PageSize,
	)
	if err != nil {
		return domainreports.MarketingReportPage{}, err
	}
	rows := make([]domainreports.MarketingReportRow, 0, len(result.Data.Rows))
	for _, row := range result.Data.Rows {
		if row == nil {
			return domainreports.MarketingReportPage{}, errors.New("Marketing report contains a null row")
		}
		rows = append(rows, domainreports.MarketingReportRow{
			Dimensions: cloneStringMap(row.Dimensions), Metrics: cloneStringMap(row.Metrics),
		})
	}
	return domainreports.MarketingReportPage{
		Rows: rows, TotalMetrics: cloneStringMap(result.Data.TotalMetrics),
		Page: page, PageSize: pageSize, TotalPages: totalPages, TotalNumber: totalNumber,
		RequestID: stringValue(result.RequestId), Message: stringValue(result.Message),
	}, nil
}

func (adapter MarketingReportsAdapter) FetchPromotionPage(
	ctx context.Context,
	request portreports.MarketingPromotionPageRequest,
) (domainreports.MarketingPromotionPage, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return domainreports.MarketingPromotionPage{}, err
	}
	filtering := models.PromotionListV30Filtering{}
	if request.ProjectID != "" {
		value, parseErr := parsePositiveID(request.ProjectID, "project_id")
		if parseErr != nil {
			return domainreports.MarketingPromotionPage{}, parseErr
		}
		filtering.ProjectId = &value
	}
	filtering.Ids, err = parseDiscoveryIDs(request.PromotionIDs, "promotion_id")
	if err != nil {
		return domainreports.MarketingPromotionPage{}, err
	}
	if request.Page < 1 || request.PageSize < 1 || request.PageSize > 100 {
		return domainreports.MarketingPromotionPage{}, errors.New("Marketing promotion page must be positive and page_size at most 100")
	}
	fields := []string{
		"promotion_id", "project_id", "advertiser_id", "promotion_name", "status",
		"status_first", "status_second", "opt_status", "source", "promotion_materials",
		"promotion_create_time", "promotion_modify_time",
	}
	result, err := platformretry.Do(
		ctx, readPolicy(adapter.Retry), ClassifyReadError,
		func(ctx context.Context, _ int) (*models.PromotionListV30Response, error) {
			response, httpResponse, sdkErr := client.sdk.PromotionListV30Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).Filtering(filtering).
				Fields(fields).Page(int64(request.Page)).PageSize(int64(request.PageSize)).Execute()
			if guardErr := guardPromotionDiscovery(response, httpResponse, sdkErr); guardErr != nil {
				return nil, guardErr
			}
			return response, nil
		},
	)
	if err != nil {
		return domainreports.MarketingPromotionPage{}, err
	}
	if result.Data.PageInfo == nil {
		return domainreports.MarketingPromotionPage{}, errors.New("Marketing promotion response is missing page_info")
	}
	pageInfo, err := mapMarketingPageInfo(request.Page, request.PageSize,
		result.Data.PageInfo.Page, result.Data.PageInfo.PageSize,
		result.Data.PageInfo.TotalPage, result.Data.PageInfo.TotalNumber)
	if err != nil {
		return domainreports.MarketingPromotionPage{}, err
	}
	rows := make([]domainreports.MarketingPromotion, 0, len(result.Data.List))
	for _, row := range result.Data.List {
		mapped, mapErr := mapMarketingPromotion(row)
		if mapErr != nil {
			return domainreports.MarketingPromotionPage{}, mapErr
		}
		rows = append(rows, mapped)
	}
	return domainreports.MarketingPromotionPage{
		Rows: rows, Page: pageInfo.Page, PageSize: pageInfo.PageSize,
		TotalPages: pageInfo.TotalPages, TotalNumber: pageInfo.TotalNumber,
		RequestID: stringValue(result.RequestId), Message: stringValue(result.Message),
	}, nil
}

func (adapter MarketingReportsAdapter) client(advertiserID, accessToken string) (*Client, int64, error) {
	if adapter.Factory == nil {
		return nil, 0, errors.New("Marketing report SDK client factory is required")
	}
	parsed, err := parsePositiveID(advertiserID, "advertiser_id")
	if err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(accessToken) == "" {
		return nil, 0, errors.New("Marketing report access token is required")
	}
	client, err := adapter.Factory.Client("marketing", ProfileBusiness, TimeoutStandard)
	return client, parsed, err
}

func guardMarketingReportSchema(
	response *models.ReportCustomConfigGetV30Response,
	httpResponse *http.Response,
	sdkErr error,
) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
	}
	return GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId,
		true, response.Data != nil)
}

func marketingReportPageInfo(
	value *models.ReportCustomGetV30ResponseDataPageInfo,
	requestedPage int,
	requestedPageSize int,
) (int, int, int, int, error) {
	if value == nil || value.Page == nil || value.PageSize == nil || value.TotalPage == nil || value.TotalNumber == nil {
		return 0, 0, 0, 0, errors.New("Marketing report response is missing page_info")
	}
	values := []*int64{value.Page, value.PageSize, value.TotalPage, value.TotalNumber}
	for _, item := range values {
		if *item < 0 || *item > int64(^uint(0)>>1) {
			return 0, 0, 0, 0, errors.New("Marketing report page_info exceeds supported range")
		}
	}
	page, pageSize := int(*value.Page), int(*value.PageSize)
	if page != requestedPage || pageSize != requestedPageSize {
		return 0, 0, 0, 0, errors.New("Marketing report response changed requested pagination")
	}
	return page, pageSize, int(*value.TotalPage), int(*value.TotalNumber), nil
}

func mapMarketingPromotion(value *models.PromotionListV30ResponseDataListInner) (domainreports.MarketingPromotion, error) {
	if value == nil || value.PromotionId == nil || *value.PromotionId <= 0 {
		return domainreports.MarketingPromotion{}, errors.New("Marketing promotion response contains an invalid promotion_id")
	}
	row := domainreports.MarketingPromotion{
		PromotionID: strconv.FormatInt(*value.PromotionId, 10), PromotionName: stringValue(value.PromotionName),
		PromotionStatus: enumValue(value.Status), PromotionStatusFirst: enumValue(value.StatusFirst),
		PromotionOptStatus: enumValue(value.OptStatus),
	}
	if value.ProjectId != nil {
		if *value.ProjectId <= 0 {
			return domainreports.MarketingPromotion{}, errors.New("Marketing promotion response contains an invalid project_id")
		}
		row.ProjectID = strconv.FormatInt(*value.ProjectId, 10)
	}
	for _, status := range value.StatusSecond {
		if status != nil {
			row.PromotionStatusSecond = append(row.PromotionStatusSecond, string(*status))
		}
	}
	row.Materials = []domainreports.MarketingVideoMaterial{}
	if value.PromotionMaterials == nil {
		return row, nil
	}
	for _, material := range value.PromotionMaterials.VideoMaterialList {
		if material == nil || material.MaterialId == nil {
			continue
		}
		if *material.MaterialId <= 0 {
			return domainreports.MarketingPromotion{}, errors.New("Marketing promotion response contains an invalid material_id")
		}
		row.Materials = append(row.Materials, domainreports.MarketingVideoMaterial{
			MaterialID: strconv.FormatInt(*material.MaterialId, 10), VideoID: stringValue(material.VideoId),
			VideoCoverID: stringValue(material.VideoCoverId), MaterialStatus: enumValue(material.MaterialStatus),
			MaterialOptStatus: enumValue(material.MaterialOptStatus), ImageMode: enumValue(material.ImageMode),
			MaterialCreateTime: stringValue(material.CreateTime),
		})
	}
	return row, nil
}

func reportResponseMap(value any) (map[string]any, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode Marketing report response: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	result := map[string]any{}
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode Marketing report response: %w", err)
	}
	return result, nil
}

func cloneStringMap(value map[string]string) map[string]string {
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}
