package localauth

import (
	"context"
	"errors"
	"html/template"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/f2config"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

const f2ConfigurationPath = "/f2/configure"

type F2Options struct {
	Settings f2config.Settings
	Address  string
	Lifetime time.Duration
}

type F2Manager struct {
	options  F2Options
	parent   context.Context
	mu       sync.Mutex
	closed   bool
	sessions map[string]*f2Session
	active   *f2Session
}

type f2Session struct {
	mu                    sync.Mutex
	view                  f2config.Session
	origin, csrf          string
	ctx                   context.Context
	cancel                context.CancelFunc
	server                *http.Server
	done, stopped, served chan struct{}
	once                  sync.Once
}

func NewF2(ctx context.Context, options F2Options) *F2Manager {
	if options.Address == "" {
		options.Address = "127.0.0.1:0"
	}
	if options.Lifetime <= 0 {
		options.Lifetime = 10 * time.Minute
	}
	return &F2Manager{parent: ctx, options: options, sessions: map[string]*f2Session{}}
}

func (manager *F2Manager) Start(ctx context.Context) (f2config.Session, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return f2config.Session{}, err
	}
	if manager.closed || manager.parent.Err() != nil {
		return f2config.Session{}, failure("f2_configuration_unavailable")
	}
	if !f2config.SystemBackend(manager.options.Settings.BackendName()) {
		return f2config.Session{}, failure("f2_credential_unavailable")
	}
	if current := manager.active; current != nil {
		current.mu.Lock()
		view := current.view
		current.mu.Unlock()
		if view.Status == "awaiting_user" {
			return view, nil
		}
	}
	host, _, err := net.SplitHostPort(manager.options.Address)
	if err != nil || host != "127.0.0.1" {
		return f2config.Session{}, failure("f2_configuration_unavailable")
	}
	listener, err := net.Listen("tcp4", manager.options.Address)
	if err != nil {
		return f2config.Session{}, failure("f2_configuration_busy")
	}
	id, err := nonce()
	if err != nil {
		_ = listener.Close()
		return f2config.Session{}, err
	}
	csrf, err := nonce()
	if err != nil {
		_ = listener.Close()
		return f2config.Session{}, err
	}
	origin := "http://" + listener.Addr().String()
	expires := time.Now().Add(manager.options.Lifetime)
	sessionContext, cancel := context.WithDeadline(manager.parent, expires)
	current := &f2Session{origin: origin, csrf: csrf, ctx: sessionContext, cancel: cancel,
		done: make(chan struct{}), stopped: make(chan struct{}), served: make(chan struct{}),
		view: f2config.Session{SessionID: id, Status: "awaiting_user", ConfigurationURL: origin + f2ConfigurationPath + "?session=" + id,
			ExpiresAt: expires.UTC().Format(time.RFC3339), Backend: manager.options.Settings.BackendName()},
	}
	current.server = &http.Server{Handler: manager.f2Handler(current), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 16 << 10}
	// Retain only bounded, non-secret session summaries in memory.
	if len(manager.sessions) >= 32 {
		for key, old := range manager.sessions {
			select {
			case <-old.stopped:
				delete(manager.sessions, key)
			default:
			}
		}
	}
	manager.sessions[id], manager.active = current, current
	view := current.view
	go func() {
		if err := current.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			current.mu.Lock()
			current.finishLocked("failed", "f2_configuration_unavailable")
			current.mu.Unlock()
		}
	}()
	go manager.finishF2Session(current)
	return view, nil
}

func (current *f2Session) finishLocked(status, code string) {
	if current.view.Status != "awaiting_user" {
		return
	}
	current.view.Status, current.view.ErrorCode = status, code
	current.view.ConfigurationURL = ""
	close(current.done)
}

func (manager *F2Manager) finishF2Session(current *f2Session) {
	select {
	case <-current.ctx.Done():
		current.mu.Lock()
		status := "cancelled"
		if errors.Is(current.ctx.Err(), context.DeadlineExceeded) {
			status = "expired"
		}
		current.finishLocked(status, "")
		current.mu.Unlock()
	case <-current.done:
	}
	timer := time.NewTimer(5 * time.Second)
	select {
	case <-current.served:
	case <-current.ctx.Done():
	case <-timer.C:
	}
	timer.Stop()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	if current.server.Shutdown(shutdownContext) != nil {
		_ = current.server.Close()
	}
	cancel()
	current.cancel()
	close(current.stopped)
}

func (manager *F2Manager) find(id string) (*f2Session, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	current := manager.sessions[id]
	if current == nil {
		return nil, failure("f2_configuration_session_not_found")
	}
	return current, nil
}

func (manager *F2Manager) Get(ctx context.Context, id string) (f2config.Session, error) {
	if err := ctx.Err(); err != nil {
		return f2config.Session{}, err
	}
	current, err := manager.find(id)
	if err != nil {
		return f2config.Session{}, err
	}
	current.mu.Lock()
	defer current.mu.Unlock()
	return current.view, nil
}

func (manager *F2Manager) Status(ctx context.Context) f2config.Status {
	return manager.options.Settings.Status(ctx)
}

func (manager *F2Manager) Cancel(ctx context.Context, id string) (f2config.Session, error) {
	current, err := manager.find(id)
	if err != nil {
		return f2config.Session{}, err
	}
	current.mu.Lock()
	current.finishLocked("cancelled", "")
	view := current.view
	current.mu.Unlock()
	current.cancel()
	select {
	case <-current.stopped:
		return view, nil
	case <-ctx.Done():
		return f2config.Session{}, ctx.Err()
	}
}

func (manager *F2Manager) Close() {
	manager.mu.Lock()
	manager.closed = true
	sessions := make([]*f2Session, 0, len(manager.sessions))
	for _, current := range manager.sessions {
		sessions = append(sessions, current)
		current.cancel()
	}
	manager.mu.Unlock()
	for _, current := range sessions {
		<-current.stopped
	}
}

func (manager *F2Manager) f2Handler(current *f2Session) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
		if !localHostMatches(request.Host, current.origin) {
			http.Error(writer, "请求来源无效", http.StatusForbidden)
			return
		}
		if request.URL.Path != f2ConfigurationPath {
			http.NotFound(writer, request)
			return
		}
		if request.Method != http.MethodGet && request.Method != http.MethodPost {
			http.Error(writer, "请求方法无效", http.StatusMethodNotAllowed)
			return
		}
		current.mu.Lock()
		defer current.mu.Unlock()
		if current.ctx.Err() != nil {
			http.Error(writer, "会话已结束，请重新配置 F2", http.StatusGone)
			return
		}
		if request.Method == http.MethodGet {
			if request.URL.Query().Get("session") != current.view.SessionID {
				http.Error(writer, "会话无效", http.StatusForbidden)
				return
			}
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			if f2Template.Execute(writer, map[string]any{"Session": current.view, "CSRF": current.csrf, "Awaiting": current.view.Status == "awaiting_user"}) == nil && current.view.Status != "awaiting_user" {
				_ = http.NewResponseController(writer).Flush()
				current.once.Do(func() { close(current.served) })
			}
			return
		}
		if !f2FormSource(request, current.origin) {
			http.Error(writer, "请求来源无效", http.StatusForbidden)
			return
		}
		contentType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || contentType != "application/x-www-form-urlencoded" {
			http.Error(writer, "请使用配置表单", http.StatusUnsupportedMediaType)
			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, 3*f2config.MaxCookieBytes+4096)
		if request.ParseForm() != nil || request.PostForm.Get("session") != current.view.SessionID || request.PostForm.Get("csrf") != current.csrf {
			http.Error(writer, "表单无效", http.StatusForbidden)
			return
		}
		if current.view.Status != "awaiting_user" {
			http.Error(writer, "会话已结束，请重新配置 F2", http.StatusConflict)
			return
		}
		value := request.PostForm.Get("cookie")
		request.PostForm.Del("cookie")
		request.Form.Del("cookie")
		if !f2config.ValidateCookie(strings.TrimSpace(value)) {
			http.Error(writer, "Cookie 格式无效，请返回并填写 Cookie 值，不能包含换行或请求头名称", http.StatusBadRequest)
			return
		}
		backend, err := manager.options.Settings.Save(current.ctx, value)
		value = ""
		if err != nil {
			code := "f2_credential_write_failed"
			var domainErr *domain.WorkLinkError
			if errors.As(err, &domainErr) {
				code = domainErr.Code
			}
			current.finishLocked("failed", code)
		} else {
			current.view.Backend, current.view.CredentialSaved, current.view.CanResume = backend, true, true
			current.finishLocked("completed", "")
		}
		http.Redirect(writer, request, current.origin+f2ConfigurationPath+"?session="+current.view.SessionID, http.StatusSeeOther)
	})
}

func f2FormSource(request *http.Request, expectedOrigin string) bool {
	// Sandboxed embedded browsers can send an opaque Origin ("null") even
	// for this page's own form. Host and the separate CSRF nonce are still
	// checked by the handler; reject browser-reported cross-site requests.
	if site := strings.TrimSpace(request.Header.Get("Sec-Fetch-Site")); site != "" && site != "same-origin" {
		return false
	}
	if origin := strings.TrimSpace(request.Header.Get("Origin")); origin != "" && origin != "null" {
		return localOriginMatches(origin, expectedOrigin, false)
	}
	if referer := strings.TrimSpace(request.Header.Get("Referer")); referer != "" {
		return localOriginMatches(referer, expectedOrigin, false) || localF2Referer(referer, expectedOrigin)
	}
	// Some embedded browsers omit source headers. The form still requires its
	// separate random CSRF nonce, which is never returned by an MCP tool.
	return true
}

func localF2Referer(referer, origin string) bool {
	base, _, _ := strings.Cut(referer, "?")
	return base == origin+f2ConfigurationPath
}

var f2Template = template.Must(template.New("f2").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>配置 F2 · Ocean Watch</title>
<style>body{margin:0;background:#f4f6fa;color:#152238;font:16px/1.6 system-ui,sans-serif}main{max-width:680px;margin:6vh auto;padding:32px;background:white;border-radius:16px;box-shadow:0 8px 40px #15223812}h1{font-size:26px;margin:0 0 12px}p{color:#4b596c}label{display:block;font-weight:600;margin-top:24px}textarea{box-sizing:border-box;width:100%;min-height:180px;margin:8px 0;padding:12px;border:1px solid #adb9ca;border-radius:8px;font:14px/1.5 monospace;resize:vertical}button{background:#1768de;color:white;border:0;padding:12px 22px;border-radius:8px;font:inherit;cursor:pointer}small{display:block;color:#647389;margin-top:16px}.status{padding:16px;background:#eef5ff;border-radius:8px}@media(max-width:720px){main{margin:16px;padding:24px}}</style></head><body><main>
<h1>配置 F2 视频信息解析</h1>{{if .Awaiting}}
<p>填写你在抖音网页版登录后取得的 Cookie，供 F2 读取作品、作者和商品信息。</p>
<form method="post" action="/f2/configure" autocomplete="off"><input type="hidden" name="session" value="{{.Session.SessionID}}"><input type="hidden" name="csrf" value="{{.CSRF}}">
<label for="cookie">抖音 Cookie</label><textarea id="cookie" name="cookie" required maxlength="32768" placeholder="名称=值; 名称=值" autocomplete="off" autocapitalize="off" spellcheck="false" aria-describedby="storage"></textarea>
<button type="submit">保存到系统凭据库</button><small id="storage">Cookie 仅提交到本机并保存至系统凭据库，不写入项目配置或日志。请直接在此填写，无需发到聊天中。此页面 10 分钟内有效。</small></form>
{{else if .Session.CredentialSaved}}<div class="status">Cookie 已保存至系统凭据库。</div><p>可以关闭页面，返回对话继续解析。保存成功表示凭据已写入并回读，作品是否可解析仍需重试确认。</p>
{{else}}<div class="status">Cookie 未完成保存。</div><p>请检查系统凭据库权限，然后返回对话重新配置 F2。</p>{{end}}
</main></body></html>`))
