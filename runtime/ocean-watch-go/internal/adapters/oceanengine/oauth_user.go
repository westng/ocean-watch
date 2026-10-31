package oceanengine

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
	platformretry "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/retry"
)

func (adapter OAuthAdapter) GetAuthorizedUser(ctx context.Context, channel, accessToken string) (domain.AuthorizedUser, error) {
	if adapter.Factory == nil || strings.TrimSpace(accessToken) == "" {
		return domain.AuthorizedUser{}, errors.New("authorized user dependencies are incomplete")
	}
	if _, ok := requestcontrol.Authorization(ctx); !ok {
		return domain.AuthorizedUser{}, requestcontrol.ErrAuthorizationScopeMissing
	}
	client, err := adapter.Factory.Client(channel, ProfileAccountDiscovery, TimeoutStandard)
	if err != nil {
		return domain.AuthorizedUser{}, err
	}
	return platformretry.Do(ctx, readPolicy(platformretry.Policy{}), ClassifyReadError, func(ctx context.Context, _ int) (domain.AuthorizedUser, error) {
		response, httpResponse, sdkErr := client.sdk.UserInfoV2Api().Get(ctx).AccessToken(accessToken).Execute()
		if response == nil {
			return domain.AuthorizedUser{}, GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false)
		}
		if err := GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil); err != nil {
			return domain.AuthorizedUser{}, err
		}
		if response.Data.Id == nil || *response.Data.Id <= 0 {
			return domain.AuthorizedUser{}, &authorizedUserResponseError{requestID: stringPointerValue(response.RequestId), httpStatus: httpResponse.StatusCode}
		}
		user := domain.AuthorizedUser{ID: strconv.FormatInt(*response.Data.Id, 10)}
		if response.Data.AppId != nil {
			user.AppID = strconv.FormatInt(*response.Data.AppId, 10)
		}
		return user, nil
	})
}

type authorizedUserResponseError struct {
	requestID  string
	httpStatus int
}

func (*authorizedUserResponseError) Error() string {
	return "official authorized user response is invalid"
}
func (*authorizedUserResponseError) AuthorizationFailureReason() string {
	return "INVALID_USER_INFO_RESPONSE"
}

func (err *authorizedUserResponseError) AuthorizationFailureMetadata() (int64, string, int) {
	return 0, err.requestID, err.httpStatus
}
