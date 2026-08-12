package oceanengine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/oceanengine/ad_open_sdk_go/models"
	domainplans "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/plans"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

const qianchuanMutationBatchLimit = 10

type QianchuanWriteAdapter struct {
	Factory *ClientFactory
}

func (adapter QianchuanWriteAdapter) ValidateCreatePlan(
	advertiserID string,
	payload json.RawMessage,
) error {
	parsedAdvertiserID, err := parsePositiveID(advertiserID, "advertiser_id")
	if err != nil {
		return err
	}
	var decoded models.QianchuanUniAwemeAdCreateV10Request
	if err := decodeStrictQianchuanPayload(payload, &decoded, "plan create"); err != nil {
		return err
	}
	if decoded.AdvertiserId != parsedAdvertiserID {
		return errors.New("Qianchuan plan advertiser_id does not match write scope")
	}
	return nil
}

func (adapter QianchuanWriteAdapter) CreatePlan(
	ctx context.Context,
	request portqianchuan.CreatePlanRequest,
) (portqianchuan.WriteResult, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return portqianchuan.WriteResult{}, notSentWriteFailure(err)
	}
	var payload models.QianchuanUniAwemeAdCreateV10Request
	if err := decodeStrictQianchuanPayload(request.Payload, &payload, "plan create"); err != nil {
		return portqianchuan.WriteResult{}, notSentWriteFailure(err)
	}
	if payload.AdvertiserId != advertiserID {
		return portqianchuan.WriteResult{}, notSentWriteFailure(
			errors.New("Qianchuan plan advertiser_id does not match write scope"),
		)
	}
	response, httpResponse, sdkErr := client.sdk.QianchuanUniAwemeAdCreateV10Api().Post(ctx).
		AccessToken(request.AccessToken).QianchuanUniAwemeAdCreateV10Request(payload).Execute()
	if err := guardQianchuanCreate(response, httpResponse, sdkErr); err != nil {
		return portqianchuan.WriteResult{}, classifyWriteFailure(err, httpResponse)
	}
	if response.Data.AdId == nil || *response.Data.AdId <= 0 {
		return portqianchuan.WriteResult{}, unknownWriteFailure(
			errors.New("Qianchuan plan success response is missing ad_id"),
		)
	}
	return portqianchuan.WriteResult{
		ObjectID:  strconv.FormatInt(*response.Data.AdId, 10),
		RequestID: stringValue(response.RequestId), RowErrors: []portqianchuan.RowError{},
	}, nil
}

func (adapter QianchuanWriteAdapter) AddMaterials(
	ctx context.Context,
	request portqianchuan.MaterialWriteRequest,
) (portqianchuan.WriteResult, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return portqianchuan.WriteResult{}, notSentWriteFailure(err)
	}
	adID, err := parsePositiveID(request.AdID, "ad_id")
	if err != nil {
		return portqianchuan.WriteResult{}, notSentWriteFailure(err)
	}
	var payload models.QianchuanUniPromotionAdMaterialAddV10Request
	if err := decodeStrictQianchuanPayload(request.Payload, &payload, "material add"); err != nil {
		return portqianchuan.WriteResult{}, notSentWriteFailure(err)
	}
	if payload.AdvertiserId != advertiserID || payload.AdId != adID {
		return portqianchuan.WriteResult{}, notSentWriteFailure(
			errors.New("Qianchuan material payload does not match write scope"),
		)
	}
	response, httpResponse, sdkErr := client.sdk.QianchuanUniPromotionAdMaterialAddV10Api().Post(ctx).
		AccessToken(request.AccessToken).QianchuanUniPromotionAdMaterialAddV10Request(payload).Execute()
	if err := guardQianchuanMaterialAdd(response, httpResponse, sdkErr); err != nil {
		return portqianchuan.WriteResult{}, classifyWriteFailure(err, httpResponse)
	}
	return portqianchuan.WriteResult{
		ObjectID: request.AdID, RequestID: stringValue(response.RequestId),
		RowErrors: []portqianchuan.RowError{},
	}, nil
}

func (adapter QianchuanWriteAdapter) DeleteMaterials(
	ctx context.Context,
	request portqianchuan.DeleteMaterialsRequest,
) (portqianchuan.WriteResult, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return portqianchuan.WriteResult{}, notSentWriteFailure(err)
	}
	adID, err := parsePositiveID(request.AdID, "ad_id")
	if err != nil {
		return portqianchuan.WriteResult{}, notSentWriteFailure(err)
	}
	materialIDs, err := parseUniqueQianchuanIDs(request.MaterialIDs, "material_id", 100)
	if err != nil {
		return portqianchuan.WriteResult{}, notSentWriteFailure(err)
	}
	payload := models.QianchuanUniPromotionAdMaterialDeleteV10Request{
		AdvertiserId: advertiserID, AdId: adID, MaterialIds: materialIDs,
	}
	response, httpResponse, sdkErr := client.sdk.QianchuanUniPromotionAdMaterialDeleteV10Api().Post(ctx).
		AccessToken(request.AccessToken).QianchuanUniPromotionAdMaterialDeleteV10Request(payload).Execute()
	if err := guardQianchuanMaterialDelete(response, httpResponse, sdkErr); err != nil {
		return portqianchuan.WriteResult{}, classifyWriteFailure(err, httpResponse)
	}
	return portqianchuan.WriteResult{
		ObjectID: request.AdID, RequestID: stringValue(response.RequestId),
		RowErrors: []portqianchuan.RowError{},
	}, nil
}

func (adapter QianchuanWriteAdapter) UpdatePlan(
	ctx context.Context,
	request portqianchuan.MutationRequest,
) (portqianchuan.WriteResult, error) {
	client, advertiserID, err := adapter.client(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return portqianchuan.WriteResult{}, notSentWriteFailure(err)
	}
	adIDs, err := parseUniqueQianchuanIDs(request.AdIDs, "ad_id", qianchuanMutationBatchLimit)
	if err != nil {
		return portqianchuan.WriteResult{}, notSentWriteFailure(err)
	}
	objectSet := make(map[int64]struct{}, len(adIDs))
	for _, adID := range adIDs {
		objectSet[adID] = struct{}{}
	}
	switch request.Kind {
	case portqianchuan.MutationStatus:
		return adapter.updateStatus(ctx, client, request, advertiserID, adIDs, objectSet)
	case portqianchuan.MutationBudget:
		return adapter.updateBudget(ctx, client, request, advertiserID, adIDs, objectSet)
	case portqianchuan.MutationROI:
		return adapter.updateROI(ctx, client, request, advertiserID, adIDs, objectSet)
	default:
		return portqianchuan.WriteResult{}, notSentWriteFailure(
			errors.New("Qianchuan mutation operation is unsupported"),
		)
	}
}

func (adapter QianchuanWriteAdapter) updateStatus(
	ctx context.Context,
	client *Client,
	request portqianchuan.MutationRequest,
	advertiserID int64,
	adIDs []int64,
	objectSet map[int64]struct{},
) (portqianchuan.WriteResult, error) {
	if request.Status != "ENABLE" && request.Status != "DISABLE" && request.Status != "DELETE" {
		return portqianchuan.WriteResult{}, notSentWriteFailure(
			errors.New("Qianchuan status must be ENABLE, DISABLE, or DELETE"),
		)
	}
	if strings.TrimSpace(request.Value) != "" || strings.TrimSpace(request.DeepExternalAction) != "" {
		return portqianchuan.WriteResult{}, notSentWriteFailure(
			errors.New("Qianchuan status mutation does not accept a value or ROI action"),
		)
	}
	payload := models.QianchuanUniPromotionAdStatusUpdateV10Request{
		AdvertiserId: advertiserID, AdIds: adIDs,
		OptStatus: models.QianchuanUniPromotionAdStatusUpdateV10OptStatus(request.Status),
	}
	response, httpResponse, sdkErr := client.sdk.QianchuanUniPromotionAdStatusUpdateV10Api().Post(ctx).
		AccessToken(request.AccessToken).QianchuanUniPromotionAdStatusUpdateV10Request(payload).Execute()
	if response == nil {
		return finishQianchuanMutation(httpResponse, sdkErr, nil, nil, nil, false, nil, nil)
	}
	rowErrors, mapErr := qianchuanStatusErrors(response.Data, objectSet)
	return finishQianchuanMutation(
		httpResponse, sdkErr, response.Code, response.Message, response.RequestId,
		response.Data != nil, rowErrors, mapErr,
	)
}

func (adapter QianchuanWriteAdapter) updateBudget(
	ctx context.Context,
	client *Client,
	request portqianchuan.MutationRequest,
	advertiserID int64,
	adIDs []int64,
	objectSet map[int64]struct{},
) (portqianchuan.WriteResult, error) {
	if strings.TrimSpace(request.Status) != "" || strings.TrimSpace(request.DeepExternalAction) != "" {
		return portqianchuan.WriteResult{}, notSentWriteFailure(
			errors.New("Qianchuan budget mutation does not accept status or ROI action"),
		)
	}
	value, err := parseQianchuanDecimal(request.Value, "budget")
	if err != nil {
		return portqianchuan.WriteResult{}, notSentWriteFailure(err)
	}
	rows := make([]*models.QianchuanUniPromotionAdBudgetUpdateV10RequestUpdateBudgetInfosInner, 0, len(adIDs))
	for _, adID := range adIDs {
		rows = append(rows, &models.QianchuanUniPromotionAdBudgetUpdateV10RequestUpdateBudgetInfosInner{
			AdId: adID, Budget: value,
		})
	}
	payload := models.QianchuanUniPromotionAdBudgetUpdateV10Request{
		AdvertiserId: advertiserID, UpdateBudgetInfos: rows,
	}
	response, httpResponse, sdkErr := client.sdk.QianchuanUniPromotionAdBudgetUpdateV10Api().Post(ctx).
		AccessToken(request.AccessToken).QianchuanUniPromotionAdBudgetUpdateV10Request(payload).Execute()
	if response == nil {
		return finishQianchuanMutation(httpResponse, sdkErr, nil, nil, nil, false, nil, nil)
	}
	rowErrors, mapErr := qianchuanBudgetErrors(response.Data, objectSet)
	return finishQianchuanMutation(
		httpResponse, sdkErr, response.Code, response.Message, response.RequestId,
		response.Data != nil, rowErrors, mapErr,
	)
}

func (adapter QianchuanWriteAdapter) updateROI(
	ctx context.Context,
	client *Client,
	request portqianchuan.MutationRequest,
	advertiserID int64,
	adIDs []int64,
	objectSet map[int64]struct{},
) (portqianchuan.WriteResult, error) {
	if strings.TrimSpace(request.Status) != "" {
		return portqianchuan.WriteResult{}, notSentWriteFailure(
			errors.New("Qianchuan ROI mutation does not accept status"),
		)
	}
	value, err := parseQianchuanDecimal(request.Value, "roi2_goal")
	if err != nil {
		return portqianchuan.WriteResult{}, notSentWriteFailure(err)
	}
	action := strings.TrimSpace(request.DeepExternalAction)
	if action != "" && action != "AD_CONVERT_TYPE_LIVE_PAY_ROI" &&
		action != "AD_CONVERT_TYPE_LIVE_PURE_PAY_ROI" {
		return portqianchuan.WriteResult{}, notSentWriteFailure(
			errors.New("Qianchuan ROI deep_external_action is unsupported"),
		)
	}
	rows := make([]*models.QianchuanUniPromotionAdRoi2GoalUpdateV10RequestUpdateRoi2InfosInner, 0, len(adIDs))
	for _, adID := range adIDs {
		row := &models.QianchuanUniPromotionAdRoi2GoalUpdateV10RequestUpdateRoi2InfosInner{
			AdId: adID, Roi2Goal: value,
		}
		if action != "" {
			value := models.QianchuanUniPromotionAdRoi2GoalUpdateV10UpdateRoi2InfosDeepExternalAction(action)
			row.DeepExternalAction = &value
		}
		rows = append(rows, row)
	}
	payload := models.QianchuanUniPromotionAdRoi2GoalUpdateV10Request{
		AdvertiserId: advertiserID, UpdateRoi2Infos: rows,
	}
	response, httpResponse, sdkErr := client.sdk.QianchuanUniPromotionAdRoi2GoalUpdateV10Api().Post(ctx).
		AccessToken(request.AccessToken).QianchuanUniPromotionAdRoi2GoalUpdateV10Request(payload).Execute()
	if response == nil {
		return finishQianchuanMutation(httpResponse, sdkErr, nil, nil, nil, false, nil, nil)
	}
	rowErrors, mapErr := qianchuanROIErrors(response.Data, objectSet)
	return finishQianchuanMutation(
		httpResponse, sdkErr, response.Code, response.Message, response.RequestId,
		response.Data != nil, rowErrors, mapErr,
	)
}

func (adapter QianchuanWriteAdapter) client(
	advertiserID string,
	accessToken string,
) (*Client, int64, error) {
	if adapter.Factory == nil {
		return nil, 0, errors.New("Ocean Engine client factory is required")
	}
	parsedAdvertiserID, err := parsePositiveID(advertiserID, "advertiser_id")
	if err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(accessToken) == "" {
		return nil, 0, errors.New("Qianchuan write access token is required")
	}
	client, err := adapter.Factory.Client("qianchuan", ProfileBusiness, TimeoutStandard)
	return client, parsedAdvertiserID, err
}

func decodeStrictQianchuanPayload(payload json.RawMessage, target any, operation string) error {
	if len(bytes.TrimSpace(payload)) == 0 || !json.Valid(payload) {
		return fmt.Errorf("Qianchuan %s payload must be valid JSON", operation)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode Qianchuan %s payload: %w", operation, err)
	}
	if err := ensureQianchuanJSONEOF(decoder); err != nil {
		return fmt.Errorf("decode Qianchuan %s payload: %w", operation, err)
	}
	return nil
}

func ensureQianchuanJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("payload contains multiple JSON values")
		}
		return err
	}
	return nil
}

func parseUniqueQianchuanIDs(values []string, field string, maximum int) ([]int64, error) {
	if len(values) == 0 || len(values) > maximum {
		return nil, fmt.Errorf("Qianchuan %s list requires 1 to %d values", field, maximum)
	}
	result := make([]int64, 0, len(values))
	seen := make(map[int64]struct{}, len(values))
	for index, value := range values {
		parsed, err := parsePositiveID(value, fmt.Sprintf("%s[%d]", field, index))
		if err != nil {
			return nil, err
		}
		if _, exists := seen[parsed]; exists {
			return nil, fmt.Errorf("Qianchuan %s values must be unique", field)
		}
		seen[parsed] = struct{}{}
		result = append(result, parsed)
	}
	return result, nil
}

func parseQianchuanDecimal(value, field string) (float64, error) {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, ".")
	if value == "" || strings.ContainsAny(value, "eE") || len(parts) > 2 ||
		parts[0] == "" || (len(parts) == 2 && (parts[1] == "" || len(parts[1]) > 2)) {
		return 0, fmt.Errorf("Qianchuan %s must be a plain positive decimal with at most two decimal places", field)
	}
	for _, part := range parts {
		for _, character := range part {
			if character < '0' || character > '9' {
				return 0, fmt.Errorf("Qianchuan %s must be a plain positive decimal", field)
			}
		}
	}
	rational, ok := new(big.Rat).SetString(value)
	if !ok || rational.Sign() <= 0 {
		return 0, fmt.Errorf("Qianchuan %s must be greater than zero", field)
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("Qianchuan %s exceeds the supported numeric range", field)
	}
	return parsed, nil
}

func guardQianchuanCreate(
	response *models.QianchuanUniAwemeAdCreateV10Response,
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

func guardQianchuanMaterialAdd(
	response *models.QianchuanUniPromotionAdMaterialAddV10Response,
	httpResponse *http.Response,
	sdkErr error,
) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, false, false)
	}
	return GuardEnvelope(
		httpResponse, sdkErr, response.Code, response.Message, response.RequestId,
		false, true,
	)
}

func guardQianchuanMaterialDelete(
	response *models.QianchuanUniPromotionAdMaterialDeleteV10Response,
	httpResponse *http.Response,
	sdkErr error,
) error {
	if response == nil {
		return GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, false, false)
	}
	return GuardEnvelope(
		httpResponse, sdkErr, response.Code, response.Message, response.RequestId,
		false, true,
	)
}

func finishQianchuanMutation(
	httpResponse *http.Response,
	sdkErr error,
	code *int64,
	message *string,
	requestID *string,
	hasData bool,
	rowErrors []portqianchuan.RowError,
	mapErr error,
) (portqianchuan.WriteResult, error) {
	result := portqianchuan.WriteResult{
		RequestID: stringPointerValue(requestID), RowErrors: rowErrors,
	}
	if mapErr != nil {
		return result, unknownWriteFailure(mapErr)
	}
	if err := GuardEnvelope(httpResponse, sdkErr, code, message, requestID, true, hasData); err != nil {
		return result, classifyWriteFailure(err, httpResponse)
	}
	if len(rowErrors) != 0 {
		return result, &domainplans.DispatchFailure{
			State: domainplans.DispatchAcknowledged,
			Cause: fmt.Errorf("Qianchuan mutation returned %d failed rows", len(rowErrors)),
		}
	}
	return result, nil
}

func qianchuanStatusErrors(
	data *models.QianchuanUniPromotionAdStatusUpdateV10ResponseData,
	expected map[int64]struct{},
) ([]portqianchuan.RowError, error) {
	if data == nil {
		return nil, nil
	}
	seen := make(map[int64]struct{}, len(data.Results))
	errorsByRow := []portqianchuan.RowError{}
	for _, row := range data.Results {
		if row == nil || row.AdId == nil || *row.AdId <= 0 {
			return nil, errors.New("Qianchuan status response row is missing ad_id")
		}
		adID := *row.AdId
		if _, ok := expected[adID]; !ok {
			return nil, errors.New("Qianchuan status response returned an unexpected ad_id")
		}
		if _, duplicate := seen[adID]; duplicate {
			return nil, errors.New("Qianchuan status response repeated ad_id")
		}
		seen[adID] = struct{}{}
		if row.Flag == nil || !*row.Flag || row.Error != nil {
			item := portqianchuan.RowError{ObjectID: strconv.FormatInt(adID, 10), Message: "Qianchuan status update failed"}
			if row.Error != nil {
				if row.Error.ErrorCode != nil {
					item.Code = strconv.FormatInt(*row.Error.ErrorCode, 10)
				}
				if row.Error.ErrorMessage != nil && strings.TrimSpace(*row.Error.ErrorMessage) != "" {
					item.Message = Redact(*row.Error.ErrorMessage)
				}
			}
			errorsByRow = append(errorsByRow, item)
		}
	}
	if len(seen) != len(expected) {
		return nil, errors.New("Qianchuan status response omitted requested ad_id rows")
	}
	return errorsByRow, nil
}

func qianchuanBudgetErrors(
	data *models.QianchuanUniPromotionAdBudgetUpdateV10ResponseData,
	expected map[int64]struct{},
) ([]portqianchuan.RowError, error) {
	if data == nil {
		return nil, nil
	}
	rows := make([]qianchuanMutationRow, 0, len(data.Results))
	for _, row := range data.Results {
		if row == nil {
			return nil, errors.New("Qianchuan budget response contains a nil row")
		}
		rows = append(rows, qianchuanMutationRow{
			AdID: row.AdId, Status: stringPointerEnum(row.Status), Message: stringPointerValue(row.ErrorMessage),
		})
	}
	return validateQianchuanMutationRows(rows, expected, "budget")
}

func qianchuanROIErrors(
	data *models.QianchuanUniPromotionAdRoi2GoalUpdateV10ResponseData,
	expected map[int64]struct{},
) ([]portqianchuan.RowError, error) {
	if data == nil {
		return nil, nil
	}
	rows := make([]qianchuanMutationRow, 0, len(data.Results))
	for _, row := range data.Results {
		if row == nil {
			return nil, errors.New("Qianchuan ROI response contains a nil row")
		}
		rows = append(rows, qianchuanMutationRow{
			AdID: row.AdId, Status: stringPointerEnum(row.Status), Message: stringPointerValue(row.ErrorMessage),
		})
	}
	return validateQianchuanMutationRows(rows, expected, "ROI")
}

type qianchuanMutationRow struct {
	AdID    *int64
	Status  string
	Message string
}

func validateQianchuanMutationRows(
	rows []qianchuanMutationRow,
	expected map[int64]struct{},
	operation string,
) ([]portqianchuan.RowError, error) {
	seen := make(map[int64]struct{}, len(rows))
	errorsByRow := []portqianchuan.RowError{}
	for _, row := range rows {
		if row.AdID == nil || *row.AdID <= 0 {
			return nil, fmt.Errorf("Qianchuan %s response row is missing ad_id", operation)
		}
		adID := *row.AdID
		if _, ok := expected[adID]; !ok {
			return nil, fmt.Errorf("Qianchuan %s response returned an unexpected ad_id", operation)
		}
		if _, duplicate := seen[adID]; duplicate {
			return nil, fmt.Errorf("Qianchuan %s response repeated ad_id", operation)
		}
		seen[adID] = struct{}{}
		if row.Status != "SUCCESS" {
			message := strings.TrimSpace(row.Message)
			if message == "" {
				message = fmt.Sprintf("Qianchuan %s update failed", operation)
			}
			errorsByRow = append(errorsByRow, portqianchuan.RowError{
				ObjectID: strconv.FormatInt(adID, 10), Message: Redact(message),
			})
		}
	}
	if len(seen) != len(expected) {
		return nil, fmt.Errorf("Qianchuan %s response omitted requested ad_id rows", operation)
	}
	return errorsByRow, nil
}

func stringPointerEnum[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}
