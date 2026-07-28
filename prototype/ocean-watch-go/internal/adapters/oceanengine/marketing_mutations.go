package oceanengine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/oceanengine/ad_open_sdk_go/models"
	domainplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/plans"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/pagination"
	portmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/marketing"
)

const marketingMutationBatchLimit = 10

type marketingMutationInput struct {
	advertiserID int64
	objectIDs    []int64
	objectSet    map[int64]struct{}
	numericValue float64
}

func (adapter MarketingPlanAdapter) ApplyMutation(
	ctx context.Context,
	request portmarketing.MutationRequest,
) (portmarketing.MutationWriteResult, error) {
	input, err := normalizeMarketingMutationRequest(ctx, request)
	if err != nil {
		return portmarketing.MutationWriteResult{}, notSentWriteFailure(err)
	}
	if adapter.Factory == nil {
		return portmarketing.MutationWriteResult{}, notSentWriteFailure(
			errors.New("Ocean Engine client factory is required"),
		)
	}
	client, err := adapter.Factory.Client("marketing", ProfileBusiness, TimeoutStandard)
	if err != nil {
		return portmarketing.MutationWriteResult{}, notSentWriteFailure(err)
	}
	switch request.Kind {
	case portmarketing.MutationProjectStatus:
		data := make([]*models.ProjectStatusUpdateV30RequestDataInner, 0, len(input.objectIDs))
		for _, objectID := range input.objectIDs {
			data = append(data, &models.ProjectStatusUpdateV30RequestDataInner{
				ProjectId: objectID,
				OptStatus: models.ProjectStatusUpdateV30DataOptStatus(request.Status),
			})
		}
		response, httpResponse, sdkErr := client.sdk.ProjectStatusUpdateV30Api().Post(ctx).
			AccessToken(request.AccessToken).ProjectStatusUpdateV30Request(
			models.ProjectStatusUpdateV30Request{AdvertiserId: input.advertiserID, Data: data},
		).Execute()
		if response == nil {
			return finishMarketingMutationWrite(httpResponse, sdkErr, nil, nil, nil, false, nil, nil)
		}
		rowErrors, mapErr := projectStatusMutationErrors(response.Data, input.objectSet)
		return finishMarketingMutationWrite(
			httpResponse, sdkErr, response.Code, response.Message, response.RequestId,
			response.Data != nil, rowErrors, mapErr,
		)
	case portmarketing.MutationPromotionStatus:
		data := make([]*models.PromotionStatusUpdateV30RequestDataInner, 0, len(input.objectIDs))
		for _, objectID := range input.objectIDs {
			data = append(data, &models.PromotionStatusUpdateV30RequestDataInner{
				PromotionId: objectID,
				OptStatus:   models.PromotionStatusUpdateV30DataOptStatus(request.Status),
			})
		}
		response, httpResponse, sdkErr := client.sdk.PromotionStatusUpdateV30Api().Post(ctx).
			AccessToken(request.AccessToken).PromotionStatusUpdateV30Request(
			models.PromotionStatusUpdateV30Request{AdvertiserId: input.advertiserID, Data: data},
		).Execute()
		if response == nil {
			return finishMarketingMutationWrite(httpResponse, sdkErr, nil, nil, nil, false, nil, nil)
		}
		rowErrors, mapErr := promotionStatusMutationErrors(response.Data, input.objectSet)
		return finishMarketingMutationWrite(
			httpResponse, sdkErr, response.Code, response.Message, response.RequestId,
			response.Data != nil, rowErrors, mapErr,
		)
	case portmarketing.MutationPromotionBudget:
		data := make([]*models.PromotionBudgetUpdateV30RequestDataInner, 0, len(input.objectIDs))
		for _, objectID := range input.objectIDs {
			data = append(data, &models.PromotionBudgetUpdateV30RequestDataInner{
				PromotionId: objectID, Budget: input.numericValue,
			})
		}
		response, httpResponse, sdkErr := client.sdk.PromotionBudgetUpdateV30Api().Post(ctx).
			AccessToken(request.AccessToken).PromotionBudgetUpdateV30Request(
			models.PromotionBudgetUpdateV30Request{AdvertiserId: input.advertiserID, Data: data},
		).Execute()
		if response == nil {
			return finishMarketingMutationWrite(httpResponse, sdkErr, nil, nil, nil, false, nil, nil)
		}
		rowErrors, mapErr := promotionBudgetMutationErrors(response.Data, input.objectSet)
		return finishMarketingMutationWrite(
			httpResponse, sdkErr, response.Code, response.Message, response.RequestId,
			response.Data != nil, rowErrors, mapErr,
		)
	case portmarketing.MutationPromotionBid:
		data := make([]*models.PromotionBidUpdateV30RequestDataInner, 0, len(input.objectIDs))
		for _, objectID := range input.objectIDs {
			data = append(data, &models.PromotionBidUpdateV30RequestDataInner{
				PromotionId: objectID, Bid: input.numericValue,
			})
		}
		response, httpResponse, sdkErr := client.sdk.PromotionBidUpdateV30Api().Post(ctx).
			AccessToken(request.AccessToken).PromotionBidUpdateV30Request(
			models.PromotionBidUpdateV30Request{AdvertiserId: input.advertiserID, Data: data},
		).Execute()
		if response == nil {
			return finishMarketingMutationWrite(httpResponse, sdkErr, nil, nil, nil, false, nil, nil)
		}
		rowErrors, mapErr := promotionBidMutationErrors(response.Data, input.objectSet)
		return finishMarketingMutationWrite(
			httpResponse, sdkErr, response.Code, response.Message, response.RequestId,
			response.Data != nil, rowErrors, mapErr,
		)
	case portmarketing.MutationProjectROI:
		data := make([]*models.ProjectRoigoalUpdateV30RequestDataInner, 0, len(input.objectIDs))
		for _, objectID := range input.objectIDs {
			value := input.numericValue
			data = append(data, &models.ProjectRoigoalUpdateV30RequestDataInner{
				ProjectId: objectID, RoiGoal: &value,
			})
		}
		response, httpResponse, sdkErr := client.sdk.ProjectRoigoalUpdateV30Api().Post(ctx).
			AccessToken(request.AccessToken).ProjectRoigoalUpdateV30Request(
			models.ProjectRoigoalUpdateV30Request{AdvertiserId: input.advertiserID, Data: data},
		).Execute()
		if response == nil {
			return finishMarketingMutationWrite(httpResponse, sdkErr, nil, nil, nil, false, nil, nil)
		}
		rowErrors, mapErr := projectROIMutationErrors(response.Data, input.objectSet)
		return finishMarketingMutationWrite(
			httpResponse, sdkErr, response.Code, response.Message, response.RequestId,
			response.Data != nil, rowErrors, mapErr,
		)
	default:
		return portmarketing.MutationWriteResult{}, notSentWriteFailure(
			errors.New("Marketing mutation operation is unsupported"),
		)
	}
}

func (adapter MarketingPlanAdapter) ReadMutation(
	ctx context.Context,
	request portmarketing.MutationRequest,
) ([]portmarketing.MutationSnapshot, error) {
	input, err := normalizeMarketingMutationRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	if adapter.Factory == nil {
		return nil, errors.New("Ocean Engine client factory is required")
	}
	client, err := adapter.Factory.Client("marketing", ProfileBusiness, TimeoutStandard)
	if err != nil {
		return nil, err
	}
	switch request.Kind {
	case portmarketing.MutationProjectStatus, portmarketing.MutationProjectROI:
		return adapter.readProjectMutation(ctx, client, request, input)
	case portmarketing.MutationPromotionStatus, portmarketing.MutationPromotionBudget,
		portmarketing.MutationPromotionBid:
		return adapter.readPromotionMutation(ctx, client, request, input)
	default:
		return nil, errors.New("Marketing mutation operation is unsupported")
	}
}

func (adapter MarketingPlanAdapter) readProjectMutation(
	ctx context.Context,
	client *Client,
	request portmarketing.MutationRequest,
	input marketingMutationInput,
) ([]portmarketing.MutationSnapshot, error) {
	fields := []string{"project_id", "opt_status"}
	if request.Kind == portmarketing.MutationProjectROI {
		fields = []string{"project_id", "delivery_setting"}
	}
	filtering := models.ProjectListV30Filtering{Ids: input.objectIDs}
	rows, err := pagination.CollectPages(ctx, pagination.PageOptions[portmarketing.MutationSnapshot]{
		Retry: readPolicy(adapter.Retry), Classify: ClassifyReadError,
		Key: func(row portmarketing.MutationSnapshot) string { return row.ObjectID },
		Fetch: func(ctx context.Context, page int) (pagination.Page[portmarketing.MutationSnapshot], error) {
			response, httpResponse, sdkErr := client.sdk.ProjectListV30Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(input.advertiserID).
				Fields(fields).Filtering(filtering).Page(int64(page)).PageSize(marketingReconciliationPageSize).
				Execute()
			if guardErr := guardProjectDiscovery(response, httpResponse, sdkErr); guardErr != nil {
				return pagination.Page[portmarketing.MutationSnapshot]{}, guardErr
			}
			pageInfo, mapErr := mapMarketingPageInfo(
				page, marketingReconciliationPageSize,
				response.Data.PageInfo.Page, response.Data.PageInfo.PageSize,
				response.Data.PageInfo.TotalPage, response.Data.PageInfo.TotalNumber,
			)
			if mapErr != nil {
				return pagination.Page[portmarketing.MutationSnapshot]{}, mapErr
			}
			snapshots := make([]portmarketing.MutationSnapshot, 0, len(response.Data.List))
			for _, item := range response.Data.List {
				if item == nil || item.ProjectId == nil || *item.ProjectId <= 0 {
					return pagination.Page[portmarketing.MutationSnapshot]{},
						errors.New("Marketing project readback row is missing project_id")
				}
				if _, expected := input.objectSet[*item.ProjectId]; !expected {
					return pagination.Page[portmarketing.MutationSnapshot]{},
						errors.New("Marketing project readback returned an unexpected project_id")
				}
				snapshot := portmarketing.MutationSnapshot{ObjectID: strconv.FormatInt(*item.ProjectId, 10)}
				if request.Kind == portmarketing.MutationProjectStatus && item.OptStatus != nil {
					snapshot.Status = string(*item.OptStatus)
				}
				if request.Kind == portmarketing.MutationProjectROI && item.DeliverySetting != nil &&
					item.DeliverySetting.RoiGoal != nil {
					snapshot.Value, mapErr = marketingMutationFloat(*item.DeliverySetting.RoiGoal)
					if mapErr != nil {
						return pagination.Page[portmarketing.MutationSnapshot]{}, mapErr
					}
				}
				snapshots = append(snapshots, snapshot)
			}
			return pagination.Page[portmarketing.MutationSnapshot]{
				Number: pageInfo.Page, TotalPages: pageInfo.TotalPages,
				TotalNumber: pageInfo.TotalNumber, Rows: snapshots,
			}, nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("read back Marketing project mutation: %w", err)
	}
	return rows, nil
}

func (adapter MarketingPlanAdapter) readPromotionMutation(
	ctx context.Context,
	client *Client,
	request portmarketing.MutationRequest,
	input marketingMutationInput,
) ([]portmarketing.MutationSnapshot, error) {
	fields := []string{"promotion_id", "opt_status"}
	switch request.Kind {
	case portmarketing.MutationPromotionBudget:
		fields = []string{"promotion_id", "budget"}
	case portmarketing.MutationPromotionBid:
		fields = []string{"promotion_id", "bid"}
	}
	filtering := models.PromotionListV30Filtering{Ids: input.objectIDs}
	rows, err := pagination.CollectPages(ctx, pagination.PageOptions[portmarketing.MutationSnapshot]{
		Retry: readPolicy(adapter.Retry), Classify: ClassifyReadError,
		Key: func(row portmarketing.MutationSnapshot) string { return row.ObjectID },
		Fetch: func(ctx context.Context, page int) (pagination.Page[portmarketing.MutationSnapshot], error) {
			response, httpResponse, sdkErr := client.sdk.PromotionListV30Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(input.advertiserID).Filtering(filtering).
				Fields(fields).Page(int64(page)).PageSize(marketingReconciliationPageSize).Execute()
			if guardErr := guardPromotionDiscovery(response, httpResponse, sdkErr); guardErr != nil {
				return pagination.Page[portmarketing.MutationSnapshot]{}, guardErr
			}
			pageInfo, mapErr := mapMarketingPageInfo(
				page, marketingReconciliationPageSize,
				response.Data.PageInfo.Page, response.Data.PageInfo.PageSize,
				response.Data.PageInfo.TotalPage, response.Data.PageInfo.TotalNumber,
			)
			if mapErr != nil {
				return pagination.Page[portmarketing.MutationSnapshot]{}, mapErr
			}
			snapshots := make([]portmarketing.MutationSnapshot, 0, len(response.Data.List))
			for _, item := range response.Data.List {
				if item == nil || item.PromotionId == nil || *item.PromotionId <= 0 {
					return pagination.Page[portmarketing.MutationSnapshot]{},
						errors.New("Marketing promotion readback row is missing promotion_id")
				}
				if _, expected := input.objectSet[*item.PromotionId]; !expected {
					return pagination.Page[portmarketing.MutationSnapshot]{},
						errors.New("Marketing promotion readback returned an unexpected promotion_id")
				}
				snapshot := portmarketing.MutationSnapshot{ObjectID: strconv.FormatInt(*item.PromotionId, 10)}
				switch request.Kind {
				case portmarketing.MutationPromotionStatus:
					if item.OptStatus != nil {
						snapshot.Status = string(*item.OptStatus)
					}
				case portmarketing.MutationPromotionBudget:
					if item.Budget != nil {
						snapshot.Value, mapErr = marketingMutationFloat(*item.Budget)
					}
				case portmarketing.MutationPromotionBid:
					if item.Bid != nil {
						snapshot.Value, mapErr = marketingMutationFloat(*item.Bid)
					}
				}
				if mapErr != nil {
					return pagination.Page[portmarketing.MutationSnapshot]{}, mapErr
				}
				snapshots = append(snapshots, snapshot)
			}
			return pagination.Page[portmarketing.MutationSnapshot]{
				Number: pageInfo.Page, TotalPages: pageInfo.TotalPages,
				TotalNumber: pageInfo.TotalNumber, Rows: snapshots,
			}, nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("read back Marketing promotion mutation: %w", err)
	}
	return rows, nil
}

func normalizeMarketingMutationRequest(
	ctx context.Context,
	request portmarketing.MutationRequest,
) (marketingMutationInput, error) {
	if ctx == nil {
		return marketingMutationInput{}, errors.New("Marketing mutation context is required")
	}
	if strings.TrimSpace(request.AccessToken) == "" {
		return marketingMutationInput{}, errors.New("Marketing mutation access token is required")
	}
	advertiserID, err := parsePositiveID(request.AdvertiserID, "advertiser_id")
	if err != nil {
		return marketingMutationInput{}, err
	}
	if len(request.ObjectIDs) == 0 || len(request.ObjectIDs) > marketingMutationBatchLimit {
		return marketingMutationInput{}, fmt.Errorf(
			"Marketing mutation requires 1 to %d object IDs", marketingMutationBatchLimit,
		)
	}
	objectIDs := make([]int64, 0, len(request.ObjectIDs))
	objectSet := make(map[int64]struct{}, len(request.ObjectIDs))
	for index, value := range request.ObjectIDs {
		objectID, parseErr := parsePositiveID(value, fmt.Sprintf("object_id[%d]", index))
		if parseErr != nil {
			return marketingMutationInput{}, parseErr
		}
		if _, exists := objectSet[objectID]; exists {
			return marketingMutationInput{}, errors.New("Marketing mutation object IDs must be unique")
		}
		objectSet[objectID] = struct{}{}
		objectIDs = append(objectIDs, objectID)
	}
	input := marketingMutationInput{
		advertiserID: advertiserID, objectIDs: objectIDs, objectSet: objectSet,
	}
	switch request.Kind {
	case portmarketing.MutationProjectStatus, portmarketing.MutationPromotionStatus:
		if request.Status != "ENABLE" && request.Status != "DISABLE" {
			return marketingMutationInput{}, errors.New("Marketing mutation status must be ENABLE or DISABLE")
		}
		if strings.TrimSpace(request.Value) != "" {
			return marketingMutationInput{}, errors.New("Marketing status mutation does not accept a value")
		}
	case portmarketing.MutationPromotionBudget, portmarketing.MutationProjectROI:
		if strings.TrimSpace(request.Status) != "" {
			return marketingMutationInput{}, errors.New("Marketing numeric mutation does not accept a status")
		}
		input.numericValue, err = parseMarketingMutationDecimal(request.Value, nil, nil)
	case portmarketing.MutationPromotionBid:
		if strings.TrimSpace(request.Status) != "" {
			return marketingMutationInput{}, errors.New("Marketing numeric mutation does not accept a status")
		}
		minimum, maximum := big.NewRat(1, 100), big.NewRat(10000, 1)
		input.numericValue, err = parseMarketingMutationDecimal(request.Value, minimum, maximum)
	default:
		return marketingMutationInput{}, errors.New("Marketing mutation operation is unsupported")
	}
	if err != nil {
		return marketingMutationInput{}, err
	}
	return input, nil
}

func parseMarketingMutationDecimal(value string, minimum, maximum *big.Rat) (float64, error) {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, ".")
	if value == "" || strings.ContainsAny(value, "eE") || len(parts) > 2 ||
		parts[0] == "" || (len(parts) == 2 && (parts[1] == "" || len(parts[1]) > 2)) {
		return 0, errors.New("Marketing mutation value must be a plain positive decimal with at most two decimal places")
	}
	for _, part := range parts {
		for _, character := range part {
			if character < '0' || character > '9' {
				return 0, errors.New("Marketing mutation value must be a plain positive decimal")
			}
		}
	}
	rational, ok := new(big.Rat).SetString(value)
	if !ok || rational.Sign() <= 0 {
		return 0, errors.New("Marketing mutation value must be greater than zero")
	}
	if minimum != nil && rational.Cmp(minimum) < 0 {
		return 0, errors.New("Marketing mutation value is below the supported minimum")
	}
	if maximum != nil && rational.Cmp(maximum) > 0 {
		return 0, errors.New("Marketing mutation value exceeds the supported maximum")
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, errors.New("Marketing mutation value exceeds the supported numeric range")
	}
	return parsed, nil
}

func finishMarketingMutationWrite(
	httpResponse *http.Response,
	sdkErr error,
	code *int64,
	message *string,
	requestID *string,
	hasData bool,
	rowErrors map[string]string,
	mapErr error,
) (portmarketing.MutationWriteResult, error) {
	result := portmarketing.MutationWriteResult{
		RequestID: stringPointerValue(requestID), RowErrors: rowErrors,
	}
	if err := GuardEnvelope(httpResponse, sdkErr, code, message, requestID, true, hasData); err != nil {
		return result, classifyWriteFailure(err, httpResponse)
	}
	if mapErr != nil {
		return result, &domainplans.DispatchFailure{
			State: domainplans.DispatchAcknowledged,
			Cause: fmt.Errorf("map Marketing mutation response: %w", mapErr),
		}
	}
	return result, nil
}

func projectStatusMutationErrors(
	data *models.ProjectStatusUpdateV30ResponseData,
	allowed map[int64]struct{},
) (map[string]string, error) {
	if data == nil {
		return nil, nil
	}
	result := map[string]string{}
	for _, row := range data.Errors {
		if row == nil {
			return nil, errors.New("project status response contains an empty error row")
		}
		if err := addMarketingMutationError(result, row.ProjectId, row.ErrorMessage, allowed); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func promotionStatusMutationErrors(
	data *models.PromotionStatusUpdateV30ResponseData,
	allowed map[int64]struct{},
) (map[string]string, error) {
	if data == nil {
		return nil, nil
	}
	result := map[string]string{}
	for _, row := range data.Errors {
		if row == nil {
			return nil, errors.New("promotion status response contains an empty error row")
		}
		if err := addMarketingMutationError(result, row.PromotionId, row.ErrorMessage, allowed); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func promotionBudgetMutationErrors(
	data *models.PromotionBudgetUpdateV30ResponseData,
	allowed map[int64]struct{},
) (map[string]string, error) {
	if data == nil {
		return nil, nil
	}
	result := map[string]string{}
	for _, row := range data.Errors {
		if row == nil {
			return nil, errors.New("promotion budget response contains an empty error row")
		}
		if err := addMarketingMutationError(result, row.PromotionId, row.ErrorMessage, allowed); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func promotionBidMutationErrors(
	data *models.PromotionBidUpdateV30ResponseData,
	allowed map[int64]struct{},
) (map[string]string, error) {
	if data == nil {
		return nil, nil
	}
	result := map[string]string{}
	for _, row := range data.Errors {
		if row == nil {
			return nil, errors.New("promotion bid response contains an empty error row")
		}
		if err := addMarketingMutationError(result, row.PromotionId, row.ErrorMessage, allowed); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func projectROIMutationErrors(
	data *models.ProjectRoigoalUpdateV30ResponseData,
	allowed map[int64]struct{},
) (map[string]string, error) {
	if data == nil {
		return nil, nil
	}
	result := map[string]string{}
	for _, row := range data.Errors {
		if row == nil {
			return nil, errors.New("project ROI response contains an empty error row")
		}
		if err := addMarketingMutationError(result, row.ProjectId, row.ErrorMessage, allowed); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func addMarketingMutationError(
	result map[string]string,
	objectID *int64,
	message *string,
	allowed map[int64]struct{},
) error {
	if objectID == nil || *objectID <= 0 {
		return errors.New("Marketing mutation error row is missing a valid object ID")
	}
	if _, exists := allowed[*objectID]; !exists {
		return errors.New("Marketing mutation error row contains an unexpected object ID")
	}
	text := Redact(strings.TrimSpace(stringPointerValue(message)))
	if text == "" {
		text = "official row update failed"
	}
	key := strconv.FormatInt(*objectID, 10)
	if previous := result[key]; previous != "" {
		result[key] = previous + "; " + text
	} else {
		result[key] = text
	}
	return nil
}

func marketingMutationFloat(value float64) (string, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return "", errors.New("Marketing mutation readback contains an invalid numeric value")
	}
	return strconv.FormatFloat(value, 'f', -1, 64), nil
}
