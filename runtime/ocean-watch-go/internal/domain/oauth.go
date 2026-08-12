package domain

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"
)

type OAuthApp struct {
	AppID  string
	Secret string
}

type OAuthToken struct {
	AccessToken     string
	RefreshToken    string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	RequestID       string
}

func BuildOAuthState(channel, nonce string) (string, error) {
	code, err := oauthStateCode(channel)
	if err != nil {
		return "", err
	}
	nonce = strings.TrimSpace(nonce)
	if nonce == "" || strings.ContainsAny(nonce, ".\x00\r\n") {
		return "", errors.New("OAuth state random value is invalid")
	}
	return code + "." + nonce, nil
}

func ChannelFromOAuthState(state string) (string, error) {
	code, nonce, found := strings.Cut(strings.TrimSpace(state), ".")
	if !found || nonce == "" || strings.Contains(nonce, ".") {
		return "", errors.New("OAuth state random value is required")
	}
	switch code {
	case "AD":
		return "marketing", nil
	case "QC":
		return "qianchuan", nil
	default:
		return "", fmt.Errorf("unknown OAuth state channel code: %s", code)
	}
}

func ValidateOAuthCallbackState(state, expectedState, expectedChannel string) *Error {
	if subtle.ConstantTimeCompare([]byte(state), []byte(expectedState)) != 1 {
		return NewError("state_mismatch", "OAuth state does not match the current session", 1, nil)
	}
	channel, err := ChannelFromOAuthState(state)
	if err != nil {
		return NewError("state_invalid", err.Error(), 1, nil)
	}
	if channel != expectedChannel {
		return NewError(
			"state_channel_mismatch",
			fmt.Sprintf("OAuth state channel %s does not match %s", channel, expectedChannel),
			1,
			nil,
		)
	}
	return nil
}

func oauthStateCode(channel string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "marketing":
		return "AD", nil
	case "qianchuan":
		return "QC", nil
	default:
		return "", fmt.Errorf("unknown channel: %s", channel)
	}
}
