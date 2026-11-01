package oceanengine

import (
	"context"
	"crypto/sha256"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oceanengine/ad_open_sdk_go/models"
	auth "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
	port "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/official"
)

// OfficialAppTokens is used only by an approved App-authenticated query. It
// keeps App and user tokens distinct and never writes credentials or logs them.
type OfficialAppTokens struct {
	Credentials auth.CredentialStore
	Factory     *ClientFactory
	mu          sync.Mutex
	identity    [32]byte
	lease       port.AppTokenLease
	expires     time.Time
}

func (provider *OfficialAppTokens) EnsureApp(ctx context.Context, expected string) (port.AppTokenLease, error) {
	if ctx == nil || ctx.Err() != nil {
		return port.AppTokenLease{}, errors.New("app token request cancelled")
	}
	if provider.Credentials == nil || provider.Factory == nil || expected == "" {
		return port.AppTokenLease{}, errors.New("app token configuration required")
	}
	account, _ := domain.AppCredentialAccount("qianchuan")
	credential, err := provider.Credentials.Read(ctx, account)
	if err != nil {
		return port.AppTokenLease{}, errors.New("app token credential unavailable")
	}
	appID, _ := credential["app_id"].(string)
	secret, _ := credential["secret"].(string)
	numeric, err := strconv.ParseInt(appID, 10, 64)
	if err != nil || numeric < 1 || appID != expected || secret == "" || strings.ContainsAny(secret, "\r\n\x00") {
		return port.AppTokenLease{}, errors.New("app token identity unverified")
	}
	identity := sha256.Sum256([]byte(appID + "\x00" + secret))
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if identity == provider.identity && time.Now().Add(time.Minute).Before(provider.expires) && provider.lease.Token != "" {
		return provider.lease, nil
	}
	client, err := provider.Factory.Client("qianchuan", ProfileAppOAuth, TimeoutStandard)
	if err != nil {
		return port.AppTokenLease{}, err
	}
	if err := requestcontrol.ReserveAttempt(ctx); err != nil {
		return port.AppTokenLease{}, errors.New("app token request budget required")
	}
	response, httpResponse, sdkErr := client.sdk.Oauth2AppAccessTokenApi().Post(ctx).Oauth2AppAccessTokenRequest(models.Oauth2AppAccessTokenRequest{AppId: numeric, Secret: secret}).Execute()
	if response == nil {
		return port.AppTokenLease{}, errors.New("app token response missing")
	}
	if GuardEnvelope(httpResponse, sdkErr, response.Code, response.Message, response.RequestId, true, response.Data != nil) != nil {
		return port.AppTokenLease{}, errors.New("app token request failed")
	}
	data := response.Data
	if data.AccessToken == nil || *data.AccessToken == "" || data.ExpiresIn == nil || *data.ExpiresIn < 1 || *data.ExpiresIn > 86400*30 {
		return port.AppTokenLease{}, errors.New("app token response invalid")
	}
	provider.identity = identity
	provider.lease = port.AppTokenLease{AppID: appID, Token: *data.AccessToken}
	provider.expires = time.Now().Add(time.Duration(*data.ExpiresIn) * time.Second)
	return provider.lease, nil
}
