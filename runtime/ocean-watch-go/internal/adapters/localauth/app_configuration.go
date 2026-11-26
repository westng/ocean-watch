package localauth

import (
	"errors"
	"html/template"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

func (manager *Manager) configureApp(writer http.ResponseWriter, request *http.Request, current *session) {
	view := current.coordinator.View()
	// A successful form POST uses a 303 to the fixed official OAuth site.
	// Explicitly permit that destination for browsers enforcing CSP on redirects.
	official, _ := url.Parse(AuthorizationURL(view.Channel, "", "", ""))
	writer.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self' "+official.Scheme+"://"+official.Host+"; frame-ancestors 'none'")
	if view.Status != "awaiting_configuration" {
		http.Error(writer, "配置会话已结束，请返回对话继续授权", http.StatusConflict)
		return
	}
	if request.Method == http.MethodGet {
		if request.URL.Query().Get("session") != view.SessionID {
			http.Error(writer, "配置会话无效", http.StatusForbidden)
			return
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = appConfigurationTemplate.Execute(writer, map[string]string{"Title": channelTitle(view.Channel), "Session": view.SessionID, "CSRF": current.setupCSRF})
		return
	}
	if request.Method != http.MethodPost {
		http.Error(writer, "请使用配置表单", http.StatusMethodNotAllowed)
		return
	}
	if !appFormSource(request, current.origin) {
		http.Error(writer, "请求来源无效", http.StatusForbidden)
		return
	}
	contentType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || contentType != "application/x-www-form-urlencoded" {
		http.Error(writer, "请使用配置表单", http.StatusUnsupportedMediaType)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 8192)
	if request.ParseForm() != nil || request.PostForm.Get("session") != view.SessionID || request.PostForm.Get("csrf") != current.setupCSRF {
		http.Error(writer, "配置表单无效", http.StatusForbidden)
		return
	}
	app := domain.OAuthApp{AppID: strings.TrimSpace(request.PostForm.Get("app_id")), Secret: strings.TrimSpace(request.PostForm.Get("secret"))}
	request.PostForm.Del("secret")
	request.Form.Del("secret")
	authorizationURL := AuthorizationURL(view.Channel, app.AppID, current.origin+callbackPath, current.oauthState)
	err = current.coordinator.ConfigureApp(app, authorizationURL)
	app.Secret = ""
	if err != nil {
		message, status := "应用配置未完成，请检查系统凭据库权限后返回重试", http.StatusInternalServerError
		var domainErr *domain.Error
		if errors.As(err, &domainErr) {
			switch domainErr.Code {
			case "invalid_app_configuration":
				message, status = "请填写有效的 App ID 和 Secret", http.StatusBadRequest
			case "app_configuration_changed", "authorization_callback_used":
				message, status = "配置已变化或会话已结束，请返回对话重新发起授权", http.StatusConflict
			case "authorization_identity_mismatch":
				message, status = "应用与原授权不一致，请使用原应用", http.StatusConflict
			}
		}
		http.Error(writer, message, status)
		return
	}
	http.Redirect(writer, request, authorizationURL, http.StatusSeeOther)
}

func appFormSource(request *http.Request, expectedOrigin string) bool {
	if site := strings.TrimSpace(request.Header.Get("Sec-Fetch-Site")); site != "" && site != "same-origin" {
		return false
	}
	if origin := strings.TrimSpace(request.Header.Get("Origin")); origin != "" && origin != "null" {
		return localOriginMatches(origin, expectedOrigin, false)
	}
	if referer := strings.TrimSpace(request.Header.Get("Referer")); referer != "" {
		parsed, err := url.Parse(referer)
		if err != nil || parsed.Path != "/oauth/setup" {
			return false
		}
		parsed.Path, parsed.RawQuery, parsed.Fragment = "", "", ""
		return localOriginMatches(parsed.String(), expectedOrigin, false)
	}
	// Embedded browsers may omit source headers. Host and an independent
	// page-only CSRF nonce are still mandatory in configureApp.
	return true
}

var appConfigurationTemplate = template.Must(template.New("app-configuration").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}}应用配置</title>
<style>body{font:16px/1.6 system-ui,sans-serif;background:#f4f6fa;color:#152238}main{max-width:520px;margin:6vh auto;padding:32px;background:white;border-radius:12px}label{display:block;margin-top:20px}input{box-sizing:border-box;width:100%;padding:12px;font:inherit}button{margin-top:24px;background:#1768de;color:white;border:0;padding:12px 20px;font:inherit}small{display:block;margin-top:20px;color:#526071}</style></head>
<body><main><h1>{{.Title}}应用配置</h1><p>填写此渠道的应用信息，保存后继续官方授权。</p>
<form method="post" action="/oauth/setup" autocomplete="off"><input type="hidden" name="session" value="{{.Session}}"><input type="hidden" name="csrf" value="{{.CSRF}}">
<label for="app_id">App ID</label><input id="app_id" name="app_id" inputmode="numeric" maxlength="19" required>
<label for="secret">Secret</label><input id="secret" name="secret" type="password" maxlength="512" autocomplete="new-password" required>
<button type="submit">保存并继续授权</button><small>应用信息只提交到本机并保存至系统凭据库。请在此填写，无需发送到聊天。此页面不能替换已配置的应用。</small></form></main></body></html>`))
