package oceanengine

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/oceanengine/ad_open_sdk_go/models"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/platform/requestcontrol"
)

type OAuthAdapter struct {
	Factory *ClientFactory
}

func (adapter OAuthAdapter) ExchangeCode(
	ctx context.Context,
	channel string,
	app domain.OAuthApp,
	authCode string,
) (domain.OAuthToken, error) {
	client, request, err := adapter.exchangeRequest(channel, app, authCode)
	if err != nil {
		return domain.OAuthToken{}, err
	}
	requestContext, err := requestcontrol.WithAuthorization(ctx, channel, "oauth-bootstrap")
	if err != nil {
		return domain.OAuthToken{}, err
	}
	response, httpResponse, sdkErr := client.sdk.Oauth2AccessTokenApi().Post(requestContext).
		Oauth2AccessTokenRequest(request).Execute()
	if response == nil {
		if guardErr := GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false); guardErr != nil {
			return domain.OAuthToken{}, guardErr
		}
		return domain.OAuthToken{}, errors.New("OAuth token response is missing")
	}
	if err := GuardEnvelope(
		httpResponse, sdkErr, response.Code, response.Message, response.RequestId,
		true, response.Data != nil,
	); err != nil {
		return domain.OAuthToken{}, err
	}
	return mapAccessToken(response.Data, response.RequestId, true)
}

func (adapter OAuthAdapter) RefreshToken(
	ctx context.Context,
	channel string,
	app domain.OAuthApp,
	refreshToken string,
) (domain.OAuthToken, error) {
	client, request, err := adapter.refreshRequest(channel, app, refreshToken)
	if err != nil {
		return domain.OAuthToken{}, err
	}
	if _, ok := requestcontrol.Authorization(ctx); !ok {
		return domain.OAuthToken{}, requestcontrol.ErrAuthorizationScopeMissing
	}
	response, httpResponse, sdkErr := client.sdk.Oauth2RefreshTokenApi().Post(ctx).
		Oauth2RefreshTokenRequest(request).Execute()
	if response == nil {
		if guardErr := GuardEnvelope(httpResponse, sdkErr, nil, nil, nil, true, false); guardErr != nil {
			return domain.OAuthToken{}, guardErr
		}
		return domain.OAuthToken{}, errors.New("OAuth refresh response is missing")
	}
	if err := GuardEnvelope(
		httpResponse, sdkErr, response.Code, response.Message, response.RequestId,
		true, response.Data != nil,
	); err != nil {
		return domain.OAuthToken{}, err
	}
	return mapRefreshToken(response.Data, response.RequestId)
}

func (adapter OAuthAdapter) exchangeRequest(
	channel string,
	app domain.OAuthApp,
	authCode string,
) (*Client, models.Oauth2AccessTokenRequest, error) {
	client, appID, err := adapter.clientAndAppID(channel, app)
	if err != nil {
		return nil, models.Oauth2AccessTokenRequest{}, err
	}
	if strings.TrimSpace(authCode) == "" {
		return nil, models.Oauth2AccessTokenRequest{}, errors.New("OAuth auth_code is required")
	}
	return client, models.Oauth2AccessTokenRequest{
		AppId: &appID, AuthCode: authCode, Secret: app.Secret,
	}, nil
}

func (adapter OAuthAdapter) refreshRequest(
	channel string,
	app domain.OAuthApp,
	refreshToken string,
) (*Client, models.Oauth2RefreshTokenRequest, error) {
	client, appID, err := adapter.clientAndAppID(channel, app)
	if err != nil {
		return nil, models.Oauth2RefreshTokenRequest{}, err
	}
	if strings.TrimSpace(refreshToken) == "" {
		return nil, models.Oauth2RefreshTokenRequest{}, errors.New("OAuth refresh_token is required")
	}
	return client, models.Oauth2RefreshTokenRequest{
		AppId: &appID, RefreshToken: refreshToken, Secret: app.Secret,
	}, nil
}

func (adapter OAuthAdapter) clientAndAppID(channel string, app domain.OAuthApp) (*Client, int64, error) {
	if adapter.Factory == nil {
		return nil, 0, errors.New("OAuth SDK client factory is required")
	}
	appID, err := strconv.ParseInt(strings.TrimSpace(app.AppID), 10, 64)
	if err != nil || appID <= 0 || strings.TrimSpace(app.Secret) == "" {
		return nil, 0, errors.New("OAuth app_id and secret are invalid")
	}
	client, err := adapter.Factory.Client(channel, ProfileOAuth, TimeoutStandard)
	return client, appID, err
}

func mapAccessToken(
	data *models.Oauth2AccessTokenResponseData,
	requestID *string,
	requireRefresh bool,
) (domain.OAuthToken, error) {
	if data == nil || data.AccessToken == nil || strings.TrimSpace(*data.AccessToken) == "" {
		return domain.OAuthToken{}, errors.New("OAuth response did not include access_token")
	}
	if requireRefresh && (data.RefreshToken == nil || strings.TrimSpace(*data.RefreshToken) == "") {
		return domain.OAuthToken{}, errors.New("OAuth response did not include refresh_token")
	}
	result := domain.OAuthToken{AccessToken: *data.AccessToken}
	if data.RefreshToken != nil {
		result.RefreshToken = *data.RefreshToken
	}
	if data.ExpiresIn != nil && *data.ExpiresIn > 0 {
		result.AccessTokenTTL = time.Duration(*data.ExpiresIn) * time.Second
	}
	if data.RefreshTokenExpiresIn != nil && *data.RefreshTokenExpiresIn > 0 {
		result.RefreshTokenTTL = time.Duration(*data.RefreshTokenExpiresIn) * time.Second
	}
	if requestID != nil {
		result.RequestID = *requestID
	}
	return result, nil
}

func mapRefreshToken(data *models.Oauth2RefreshTokenResponseData, requestID *string) (domain.OAuthToken, error) {
	if data == nil || data.AccessToken == nil || strings.TrimSpace(*data.AccessToken) == "" {
		return domain.OAuthToken{}, errors.New("OAuth refresh response did not include access_token")
	}
	result := domain.OAuthToken{AccessToken: *data.AccessToken}
	if data.RefreshToken != nil {
		result.RefreshToken = *data.RefreshToken
	}
	if data.ExpiresIn != nil && *data.ExpiresIn > 0 {
		result.AccessTokenTTL = time.Duration(*data.ExpiresIn) * time.Second
	}
	if data.RefreshTokenExpiresIn != nil && *data.RefreshTokenExpiresIn > 0 {
		result.RefreshTokenTTL = time.Duration(*data.RefreshTokenExpiresIn) * time.Second
	}
	if requestID != nil {
		result.RequestID = *requestID
	}
	return result, nil
}
