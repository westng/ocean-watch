package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/authorization"
)

var (
	ErrNotFound                = errors.New("credential reference was not found")
	ErrConflict                = errors.New("credential revision conflicts with persisted state")
	ErrUnavailable             = errors.New("credential storage is unavailable")
	ErrInvalidState            = errors.New("credential state is invalid")
	ErrReauthorizationRequired = errors.New("reauthorization_required")
)

type Secrets struct {
	AccessToken  string `json:"-"`
	RefreshToken string `json:"-"`
}

func (Secrets) String() string               { return "[redacted credential]" }
func (Secrets) GoString() string             { return "[redacted credential]" }
func (Secrets) MarshalJSON() ([]byte, error) { return json.Marshal("[redacted credential]") }

func (secrets Secrets) Validate() error {
	for _, token := range []string{secrets.AccessToken, secrets.RefreshToken} {
		if len(token) == 0 || len(token) > 16384 || strings.TrimSpace(token) != token {
			return ErrInvalidState
		}
		for _, char := range token {
			if unicode.IsControl(char) || unicode.IsSpace(char) {
				return ErrInvalidState
			}
		}
	}
	return nil
}

type Record struct {
	Version authorization.CredentialVersion
	Secrets Secrets `json:"-"`
}

func (record Record) Validate() error {
	if record.Version.Validate() != nil || record.Secrets.Validate() != nil {
		return ErrInvalidState
	}
	return nil
}

type SecureBackend interface {
	Read(context.Context, string) (Record, error)
	Put(context.Context, Record) error
}

type RefreshResult struct {
	Secrets          Secrets
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
}

type RefreshProvider interface {
	Refresh(context.Context, authorization.Key, Secrets) (RefreshResult, error)
}

type RefreshLocker interface {
	Acquire(context.Context, string, string) (func() error, error)
}

type Rotations interface {
	LoadCredentialState(context.Context, authorization.Key) (authorization.CredentialState, error)
	BeginCredentialRotation(context.Context, authorization.Rotation) error
	RecordCredentialCandidate(context.Context, authorization.Key, int64, authorization.CredentialVersion) error
	ActivateCredentialCandidate(context.Context, authorization.Key, int64, time.Time) error
	CompleteCredentialRotation(context.Context, authorization.Key, int64, time.Time) error
	RequireCredentialReauthorization(context.Context, authorization.Key, int64, string, time.Time) error
}
