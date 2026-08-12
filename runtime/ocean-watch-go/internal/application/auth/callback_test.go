package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type callbackOAuth struct {
	mu        sync.Mutex
	exchanges []string
}

func (oauth *callbackOAuth) ExchangeCode(_ context.Context, channel string, _ domain.OAuthApp, code string) (domain.OAuthToken, error) {
	oauth.mu.Lock()
	defer oauth.mu.Unlock()
	oauth.exchanges = append(oauth.exchanges, channel+":"+code)
	return domain.OAuthToken{AccessToken: "fixture-access", RefreshToken: "fixture-refresh"}, nil
}

func (*callbackOAuth) RefreshToken(context.Context, string, domain.OAuthApp, string) (domain.OAuthToken, error) {
	return domain.OAuthToken{}, errors.New("not used")
}

func TestOAuthCallbackStateValidatedBeforeExchange(t *testing.T) {
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	oauth := new(callbackOAuth)
	session := &OAuthCallbackSession{
		Channel: "marketing", ExpectedState: "AD.expected", ExpiresAt: now.Add(time.Minute),
		App: domain.OAuthApp{AppID: "fixture-app", Secret: "fixture-secret"}, OAuth: oauth,
		Now: func() time.Time { return now },
	}
	for _, state := range []string{"QC.expected", "AD.wrong", "random"} {
		if _, err := session.Exchange(context.Background(), state, "code"); err == nil {
			t.Fatalf("invalid state accepted: %s", state)
		}
	}
	if len(oauth.exchanges) != 0 {
		t.Fatalf("invalid state reached token exchange: %v", oauth.exchanges)
	}
	if _, err := session.Exchange(context.Background(), "AD.expected", "valid-code"); err != nil {
		t.Fatal(err)
	}
	if len(oauth.exchanges) != 1 || oauth.exchanges[0] != "marketing:valid-code" {
		t.Fatalf("valid callback exchange calls = %v", oauth.exchanges)
	}
	if _, err := session.Exchange(context.Background(), "AD.expected", "replay-code"); err == nil {
		t.Fatal("duplicate callback was accepted")
	}
	if len(oauth.exchanges) != 1 {
		t.Fatalf("duplicate callback reached token exchange: %v", oauth.exchanges)
	}
}

func TestOAuthCallbackStateRejectsExpiredSession(t *testing.T) {
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	oauth := new(callbackOAuth)
	session := &OAuthCallbackSession{
		Channel: "qianchuan", ExpectedState: "QC.expected", ExpiresAt: now,
		OAuth: oauth, Now: func() time.Time { return now },
	}
	if _, err := session.Exchange(context.Background(), "QC.expected", "code"); err == nil {
		t.Fatal("expired callback was accepted")
	}
	if len(oauth.exchanges) != 0 {
		t.Fatal("expired callback reached token exchange")
	}
}
