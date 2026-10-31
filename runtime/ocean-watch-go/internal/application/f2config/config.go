package f2config

import (
	"context"
	"errors"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

const CredentialAccount = "douyin-f2-cookie"
const MaxCookieBytes = 32 << 10

type CredentialStore interface {
	Read(context.Context, string) (map[string]any, error)
	Write(context.Context, string, map[string]any) (string, error)
	BackendName() string
}

type Settings struct{ Store CredentialStore }

func (settings Settings) BackendName() string {
	if settings.Store == nil {
		return "unavailable"
	}
	return settings.Store.BackendName()
}

func SystemBackend(backend string) bool {
	return backend == "macos-keychain" || backend == "linux-secret-service" || backend == "windows-credential-manager"
}

func ValidateCookie(value string) bool {
	if value == "" || len(value) > MaxCookieBytes {
		return false
	}
	for _, ch := range value {
		if ch < 0x20 || ch > 0x7e {
			return false
		}
	}
	for _, part := range strings.Split(value, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, _, ok := strings.Cut(part, "=")
		if !ok || name == "" || strings.ContainsAny(name, " ()<>@,:\\\"/[]?={}") {
			return false
		}
	}
	return strings.Contains(value, "=")
}

func (settings Settings) Save(ctx context.Context, value string) (string, error) {
	value = strings.TrimSpace(value)
	if !ValidateCookie(value) {
		return "", failure("f2_cookie_invalid", "请填写完整的抖音 Cookie，不能包含换行或请求头名称")
	}
	if !SystemBackend(settings.BackendName()) {
		return "", failure("f2_credential_unavailable", "系统凭据库不可用，Cookie 未保存")
	}
	backend, err := settings.Store.Write(ctx, CredentialAccount, map[string]any{"cookie": value})
	if err != nil || !SystemBackend(backend) {
		return "", failure("f2_credential_write_failed", "Cookie 保存或回读失败，请检查系统凭据库权限")
	}
	return backend, nil
}

// Cookie is an internal process boundary, never an MCP output or config field.
func (settings Settings) Cookie(ctx context.Context) (string, error) {
	if !SystemBackend(settings.BackendName()) {
		return "", failure("f2_credential_unavailable", "系统凭据库不可用，请先配置 F2")
	}
	entry, err := settings.Store.Read(ctx, CredentialAccount)
	if err != nil {
		return "", failure("f2_credential_unavailable", "无法读取 F2 系统凭据，请检查凭据库权限")
	}
	value, _ := entry["cookie"].(string)
	if value == "" {
		return "", failure("f2_cookie_missing", "F2 尚未配置，请先使用“配置 F2”在本机页面填写 Cookie")
	}
	if !ValidateCookie(value) {
		return "", failure("f2_cookie_invalid", "已保存的 F2 Cookie 格式无效，请重新配置 F2")
	}
	return value, nil
}

// Status checks current vault readability independently of any process-local
// configuration session. It never claims that Douyin accepted the Cookie.
func (settings Settings) Status(ctx context.Context) Status {
	status := Status{Backend: settings.BackendName()}
	if ctx.Err() != nil {
		status.ErrorCode = "f2_credential_unavailable"
		return status
	}
	if _, err := settings.Cookie(ctx); err != nil {
		status.ErrorCode = "f2_credential_unavailable"
		var domainErr *domain.WorkLinkError
		if errors.As(err, &domainErr) {
			status.ErrorCode = domainErr.Code
		}
		return status
	}
	status.Configured = true
	return status
}

type Status struct {
	Configured     bool   `json:"configured"`
	Backend        string `json:"credential_backend"`
	OnlineVerified bool   `json:"online_verified"`
	ErrorCode      string `json:"error_code,omitempty"`
}

func failure(code, message string) error { return domain.NewWorkLinkError(code, message) }

type Session struct {
	SessionID        string `json:"session_id"`
	Status           string `json:"status"`
	ConfigurationURL string `json:"configuration_url,omitempty"`
	ExpiresAt        string `json:"expires_at"`
	Backend          string `json:"credential_backend"`
	CredentialSaved  bool   `json:"credential_saved"`
	CanResume        bool   `json:"can_resume"`
	ErrorCode        string `json:"error_code,omitempty"`
}

type Configuration interface {
	Start(context.Context) (Session, error)
	Get(context.Context, string) (Session, error)
	Cancel(context.Context, string) (Session, error)
	Status(context.Context) Status
}
