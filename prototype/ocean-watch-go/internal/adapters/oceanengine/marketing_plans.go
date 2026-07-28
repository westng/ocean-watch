package oceanengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/oceanengine/ad_open_sdk_go/models"
	domainplans "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain/plans"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/pagination"
	platformretry "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/retry"
	portmarketing "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/marketing"
)

const marketingReconciliationPageSize = 100

type MarketingPlanAdapter struct {
	Factory *ClientFactory
	Retry   platformretry.Policy
}

type marketingPlanCandidate struct {
	ObjectID string
	Name     string
	ParentID string
}

func (adapter MarketingPlanAdapter) CreateProject(
	ctx context.Context,
	request portmarketing.ProjectCreateRequest,
) (portmarketing.CreateResult, error) {
	client, advertiserID, err := adapter.writeClient(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return portmarketing.CreateResult{}, notSentWriteFailure(err)
	}
	payload, err := decodeProjectCreatePayload(request.Payload, advertiserID)
	if err != nil {
		return portmarketing.CreateResult{}, notSentWriteFailure(err)
	}
	response, httpResponse, sdkErr := client.sdk.ProjectCreateV30Api().Post(ctx).
		AccessToken(request.AccessToken).ProjectCreateV30Request(payload).Execute()
	if err := guardProjectCreate(response, httpResponse, sdkErr); err != nil {
		return portmarketing.CreateResult{}, classifyWriteFailure(err, httpResponse)
	}
	if response.Data.ProjectId == nil || *response.Data.ProjectId <= 0 {
		return portmarketing.CreateResult{}, unknownWriteFailure(
			errors.New("Marketing project success response is missing project_id"),
		)
	}
	return portmarketing.CreateResult{
		ObjectID:  strconv.FormatInt(*response.Data.ProjectId, 10),
		RequestID: stringValue(response.RequestId),
	}, nil
}

func (adapter MarketingPlanAdapter) CreatePromotion(
	ctx context.Context,
	request portmarketing.PromotionCreateRequest,
) (portmarketing.CreateResult, error) {
	client, advertiserID, err := adapter.writeClient(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return portmarketing.CreateResult{}, notSentWriteFailure(err)
	}
	projectID, err := parsePositiveID(request.ProjectID, "project_id")
	if err != nil {
		return portmarketing.CreateResult{}, notSentWriteFailure(err)
	}
	payload, err := decodePromotionCreatePayload(request.Payload, advertiserID, projectID)
	if err != nil {
		return portmarketing.CreateResult{}, notSentWriteFailure(err)
	}
	response, httpResponse, sdkErr := client.sdk.PromotionCreateV30Api().Post(ctx).
		AccessToken(request.AccessToken).PromotionCreateV30Request(payload).Execute()
	if err := guardPromotionCreate(response, httpResponse, sdkErr); err != nil {
		return portmarketing.CreateResult{}, classifyWriteFailure(err, httpResponse)
	}
	if response.Data.PromotionId == nil || *response.Data.PromotionId <= 0 {
		return portmarketing.CreateResult{}, unknownWriteFailure(
			errors.New("Marketing promotion success response is missing promotion_id"),
		)
	}
	return portmarketing.CreateResult{
		ObjectID:  strconv.FormatInt(*response.Data.PromotionId, 10),
		RequestID: stringValue(response.RequestId),
	}, nil
}

func (adapter MarketingPlanAdapter) FindProjects(
	ctx context.Context,
	request portmarketing.ProjectReconciliationRequest,
) ([]string, error) {
	client, advertiserID, err := adapter.readClient(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return nil, errors.New("Marketing project reconciliation name is required")
	}
	filtering := models.ProjectListV30Filtering{Name: stringPointer(name)}
	rows, err := pagination.CollectPages(ctx, pagination.PageOptions[marketingPlanCandidate]{
		Retry: readPolicy(adapter.Retry), Classify: ClassifyReadError,
		Key: func(row marketingPlanCandidate) string { return row.ObjectID },
		Fetch: func(ctx context.Context, page int) (pagination.Page[marketingPlanCandidate], error) {
			response, httpResponse, sdkErr := client.sdk.ProjectListV30Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).
				Fields([]string{"project_id", "name"}).Filtering(filtering).
				Page(int64(page)).PageSize(marketingReconciliationPageSize).Execute()
			if guardErr := guardProjectDiscovery(response, httpResponse, sdkErr); guardErr != nil {
				return pagination.Page[marketingPlanCandidate]{}, guardErr
			}
			pageInfo, mapErr := mapMarketingPageInfo(
				page, marketingReconciliationPageSize,
				response.Data.PageInfo.Page, response.Data.PageInfo.PageSize,
				response.Data.PageInfo.TotalPage, response.Data.PageInfo.TotalNumber,
			)
			if mapErr != nil {
				return pagination.Page[marketingPlanCandidate]{}, mapErr
			}
			candidates := make([]marketingPlanCandidate, 0, len(response.Data.List))
			for _, item := range response.Data.List {
				if item == nil || item.ProjectId == nil || *item.ProjectId <= 0 {
					return pagination.Page[marketingPlanCandidate]{}, errors.New("Marketing project reconciliation row is missing project_id")
				}
				candidates = append(candidates, marketingPlanCandidate{
					ObjectID: strconv.FormatInt(*item.ProjectId, 10), Name: stringValue(item.Name),
				})
			}
			return pagination.Page[marketingPlanCandidate]{
				Number: pageInfo.Page, TotalPages: pageInfo.TotalPages,
				TotalNumber: pageInfo.TotalNumber, Rows: candidates,
			}, nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("list Marketing projects for reconciliation: %w", err)
	}
	return exactCandidateIDs(rows, name, ""), nil
}

func (adapter MarketingPlanAdapter) FindPromotions(
	ctx context.Context,
	request portmarketing.PromotionReconciliationRequest,
) ([]string, error) {
	client, advertiserID, err := adapter.readClient(request.AdvertiserID, request.AccessToken)
	if err != nil {
		return nil, err
	}
	projectID, err := parsePositiveID(request.ProjectID, "project_id")
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return nil, errors.New("Marketing promotion reconciliation name is required")
	}
	filtering := models.PromotionListV30Filtering{
		Name: stringPointer(name), ProjectId: &projectID,
	}
	rows, err := pagination.CollectPages(ctx, pagination.PageOptions[marketingPlanCandidate]{
		Retry: readPolicy(adapter.Retry), Classify: ClassifyReadError,
		Key: func(row marketingPlanCandidate) string { return row.ObjectID },
		Fetch: func(ctx context.Context, page int) (pagination.Page[marketingPlanCandidate], error) {
			response, httpResponse, sdkErr := client.sdk.PromotionListV30Api().Get(ctx).
				AccessToken(request.AccessToken).AdvertiserId(advertiserID).Filtering(filtering).
				Fields([]string{"promotion_id", "project_id", "promotion_name"}).
				Page(int64(page)).PageSize(marketingReconciliationPageSize).Execute()
			if guardErr := guardPromotionDiscovery(response, httpResponse, sdkErr); guardErr != nil {
				return pagination.Page[marketingPlanCandidate]{}, guardErr
			}
			pageInfo, mapErr := mapMarketingPageInfo(
				page, marketingReconciliationPageSize,
				response.Data.PageInfo.Page, response.Data.PageInfo.PageSize,
				response.Data.PageInfo.TotalPage, response.Data.PageInfo.TotalNumber,
			)
			if mapErr != nil {
				return pagination.Page[marketingPlanCandidate]{}, mapErr
			}
			candidates := make([]marketingPlanCandidate, 0, len(response.Data.List))
			for _, item := range response.Data.List {
				if item == nil || item.PromotionId == nil || *item.PromotionId <= 0 ||
					item.ProjectId == nil || *item.ProjectId <= 0 {
					return pagination.Page[marketingPlanCandidate]{}, errors.New("Marketing promotion reconciliation row is missing identity")
				}
				candidates = append(candidates, marketingPlanCandidate{
					ObjectID: strconv.FormatInt(*item.PromotionId, 10),
					Name:     stringValue(item.PromotionName), ParentID: strconv.FormatInt(*item.ProjectId, 10),
				})
			}
			return pagination.Page[marketingPlanCandidate]{
				Number: pageInfo.Page, TotalPages: pageInfo.TotalPages,
				TotalNumber: pageInfo.TotalNumber, Rows: candidates,
			}, nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("list Marketing promotions for reconciliation: %w", err)
	}
	return exactCandidateIDs(rows, name, request.ProjectID), nil
}

func (adapter MarketingPlanAdapter) writeClient(advertiserID, accessToken string) (*Client, int64, error) {
	return adapter.client(advertiserID, accessToken, "Marketing plan write")
}

func (adapter MarketingPlanAdapter) readClient(advertiserID, accessToken string) (*Client, int64, error) {
	return adapter.client(advertiserID, accessToken, "Marketing plan reconciliation")
}

func (adapter MarketingPlanAdapter) client(
	advertiserID string,
	accessToken string,
	operation string,
) (*Client, int64, error) {
	if adapter.Factory == nil {
		return nil, 0, errors.New("Ocean Engine client factory is required")
	}
	parsedAdvertiserID, err := parsePositiveID(advertiserID, "advertiser_id")
	if err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(accessToken) == "" {
		return nil, 0, fmt.Errorf("%s access token is required", operation)
	}
	client, err := adapter.Factory.Client("marketing", ProfileBusiness, TimeoutStandard)
	return client, parsedAdvertiserID, err
}

func decodeProjectCreatePayload(payload json.RawMessage, advertiserID int64) (models.ProjectCreateV30Request, error) {
	var decoded models.ProjectCreateV30Request
	if len(payload) == 0 || !json.Valid(payload) {
		return decoded, errors.New("Marketing project payload must be valid JSON")
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return decoded, fmt.Errorf("decode Marketing project payload: %w", err)
	}
	if decoded.AdvertiserId != advertiserID {
		return decoded, errors.New("Marketing project advertiser_id does not match write scope")
	}
	if strings.TrimSpace(decoded.Name) == "" {
		return decoded, errors.New("Marketing project name is required")
	}
	return decoded, nil
}

func decodePromotionCreatePayload(
	payload json.RawMessage,
	advertiserID int64,
	projectID int64,
) (models.PromotionCreateV30Request, error) {
	var decoded models.PromotionCreateV30Request
	if len(payload) == 0 || !json.Valid(payload) {
		return decoded, errors.New("Marketing promotion payload must be valid JSON")
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return decoded, fmt.Errorf("decode Marketing promotion payload: %w", err)
	}
	if decoded.AdvertiserId != advertiserID {
		return decoded, errors.New("Marketing promotion advertiser_id does not match write scope")
	}
	if decoded.ProjectId != projectID {
		return decoded, errors.New("Marketing promotion project_id does not match transaction project")
	}
	if strings.TrimSpace(decoded.Name) == "" {
		return decoded, errors.New("Marketing promotion name is required")
	}
	return decoded, nil
}

func guardProjectCreate(
	response *models.ProjectCreateV30Response,
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

func guardPromotionCreate(
	response *models.PromotionCreateV30Response,
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

func classifyWriteFailure(err error, response *http.Response) error {
	if err == nil {
		return nil
	}
	var requestError *SDKRequestError
	if errors.As(err, &requestError) {
		if requestError.Dispatched() {
			return unknownWriteFailure(err)
		}
		return notSentWriteFailure(err)
	}
	var envelopeError *EnvelopeError
	if errors.As(err, &envelopeError) {
		return &domainplans.DispatchFailure{State: domainplans.DispatchAcknowledged, Cause: err}
	}
	if response != nil {
		return unknownWriteFailure(err)
	}
	return notSentWriteFailure(err)
}

func notSentWriteFailure(err error) error {
	return &domainplans.DispatchFailure{State: domainplans.DispatchNotSent, Cause: err}
}

func unknownWriteFailure(err error) error {
	return &domainplans.DispatchFailure{State: domainplans.DispatchUnknown, Cause: err}
}

func exactCandidateIDs(rows []marketingPlanCandidate, name, parentID string) []string {
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Name == name && (parentID == "" || row.ParentID == parentID) {
			result = append(result, row.ObjectID)
		}
	}
	return result
}
