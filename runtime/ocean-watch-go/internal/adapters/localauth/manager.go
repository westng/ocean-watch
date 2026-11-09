package localauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	auth "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
)

const callbackPath = "/oauth/callback"

type Options struct {
	Credentials auth.CredentialStore
	OAuth       auth.OAuthAdapter
	Users       auth.AuthorizedUserProvider
	Authorizer  auth.Authorizer
	Syncer      auth.AdvertiserSnapshotSync
	// Address and Lifetime are installation/test dependencies, never MCP inputs.
	Address  string
	Lifetime time.Duration
}

type Manager struct {
	options  Options
	parent   context.Context
	mu       sync.Mutex
	startMu  sync.Mutex
	closed   bool
	sessions map[string]*session
	active   *session
}

type session struct {
	input        auth.RecoveryRequest
	coordinator  *auth.RecoveryCoordinator
	server       *http.Server
	serveDone    chan struct{}
	shutdownDone chan struct{}
	resultServed chan struct{}
	stopWaiting  chan struct{}
	resultOnce   sync.Once
	stopOnce     sync.Once
	origin       string
}

func New(ctx context.Context, options Options) *Manager {
	if options.Address == "" {
		options.Address = "127.0.0.1:8787"
	}
	if options.Lifetime <= 0 {
		options.Lifetime = 10 * time.Minute
	}
	return &Manager{options: options, parent: ctx, sessions: map[string]*session{}}
}

func (manager *Manager) Close() {
	manager.mu.Lock()
	manager.closed = true
	sessions := make([]*session, 0, len(manager.sessions))
	for _, current := range manager.sessions {
		sessions = append(sessions, current)
	}
	manager.mu.Unlock()
	for _, current := range sessions {
		current.coordinator.RequestCancel()
		current.stopOnce.Do(func() { close(current.stopWaiting) })
	}
	for _, current := range sessions {
		<-current.shutdownDone
	}
}

func (manager *Manager) Start(ctx context.Context, input auth.RecoveryRequest) (auth.RecoverySession, error) {
	manager.startMu.Lock()
	defer manager.startMu.Unlock()
	if err := ctx.Err(); err != nil {
		return auth.RecoverySession{}, err
	}
	manager.mu.Lock()
	closed := manager.closed
	manager.mu.Unlock()
	if closed {
		return auth.RecoverySession{}, failure("authorization_recovery_unavailable")
	}
	if err := manager.parent.Err(); err != nil {
		return auth.RecoverySession{}, err
	}
	flow := auth.Service{Credentials: manager.options.Credentials, OAuth: manager.options.OAuth, Users: manager.options.Users, Authorizer: manager.options.Authorizer, Syncer: manager.options.Syncer}
	input, app, err := flow.Prepare(ctx, input)
	if err != nil {
		return auth.RecoverySession{}, err
	}
	manager.mu.Lock()
	current := manager.active
	manager.mu.Unlock()
	if current != nil {
		view := current.coordinator.View()
		if !terminal(view.Status) {
			if current.input == input {
				return view, nil
			}
			return auth.RecoverySession{}, failure("authorization_busy")
		}
		current.stopOnce.Do(func() { close(current.stopWaiting) })
		select {
		case <-current.shutdownDone:
		case <-ctx.Done():
			return auth.RecoverySession{}, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return auth.RecoverySession{}, err
	}
	if err := manager.parent.Err(); err != nil {
		return auth.RecoverySession{}, err
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed {
		return auth.RecoverySession{}, failure("authorization_recovery_unavailable")
	}
	host, _, err := net.SplitHostPort(manager.options.Address)
	if err != nil || host != "127.0.0.1" {
		return auth.RecoverySession{}, failure("authorization_recovery_unavailable")
	}
	listener, err := net.Listen("tcp4", manager.options.Address)
	if err != nil {
		return auth.RecoverySession{}, failure("authorization_busy")
	}
	id, err := nonce()
	if err != nil {
		_ = listener.Close()
		return auth.RecoverySession{}, err
	}
	stateNonce, err := nonce()
	if err != nil {
		_ = listener.Close()
		return auth.RecoverySession{}, err
	}
	state, err := domain.BuildOAuthState(input.Channel, stateNonce)
	if err != nil {
		_ = listener.Close()
		return auth.RecoverySession{}, err
	}
	sessionContext, _, _, err := requestcontrol.PrepareCommandContext(manager.parent, requestcontrol.DefaultCommandRequestLimit)
	if err != nil {
		_ = listener.Close()
		return auth.RecoverySession{}, err
	}
	origin := "http://" + listener.Addr().String()
	view := auth.RecoverySession{SessionID: id, Channel: input.Channel, Operation: input.Operation, AdvertiserID: input.AdvertiserID, AuthorizationID: input.AuthorizationID,
		Status: "awaiting_user", AuthorizationURL: origin + "/oauth/confirm?session=" + id, ExpiresAt: time.Now().UTC().Add(manager.options.Lifetime).Format(time.RFC3339)}
	if input.Operation != "sync_accounts" {
		view.Status = "awaiting_callback"
		view.AuthorizationURL = AuthorizationURL(input.Channel, app.AppID, origin+callbackPath, state)
	}
	current = &session{input: input, origin: origin, serveDone: make(chan struct{}), shutdownDone: make(chan struct{}), resultServed: make(chan struct{}), stopWaiting: make(chan struct{})}
	current.coordinator = auth.NewRecoveryCoordinator(sessionContext, input, view, state, manager.options.Lifetime, flow)
	current.server = &http.Server{Handler: manager.handler(current), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	// Bound retained, safe terminal metadata; never persist a bearer callback.
	if len(manager.sessions) >= 32 {
		for key, old := range manager.sessions {
			select {
			case <-old.shutdownDone:
				delete(manager.sessions, key)
			default:
				continue
			}
			break
		}
	}
	manager.sessions[id], manager.active = current, current
	go func() {
		err := current.server.Serve(listener)
		close(current.serveDone)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			current.coordinator.FailService()
		}
	}()
	go manager.finishSession(current)
	return view, nil
}

func (manager *Manager) session(channel, id string) (*session, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	current := manager.sessions[id]
	if current == nil || current.input.Channel != channel {
		return nil, failure("authorization_session_not_found")
	}
	return current, nil
}

func (manager *Manager) Get(ctx context.Context, channel, id string) (auth.RecoverySession, error) {
	if err := ctx.Err(); err != nil {
		return auth.RecoverySession{}, err
	}
	current, err := manager.session(channel, id)
	if err != nil {
		return auth.RecoverySession{}, err
	}
	return current.coordinator.View(), nil
}

func (manager *Manager) Cancel(ctx context.Context, channel, id string) (auth.RecoverySession, error) {
	if err := ctx.Err(); err != nil {
		return auth.RecoverySession{}, err
	}
	current, err := manager.session(channel, id)
	if err != nil {
		return auth.RecoverySession{}, err
	}
	current.coordinator.RequestCancel()
	current.stopOnce.Do(func() { close(current.stopWaiting) })
	select {
	case <-current.shutdownDone:
		return current.coordinator.View(), nil
	case <-ctx.Done():
		return auth.RecoverySession{}, ctx.Err()
	}
}

func (manager *Manager) finishSession(current *session) {
	<-current.coordinator.Done()
	view := current.coordinator.View()
	if view.Status != "cancelled" && view.Status != "expired" {
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-current.resultServed:
		case <-current.stopWaiting:
		case <-timer.C:
		}
		timer.Stop()
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := current.server.Shutdown(shutdownContext); err != nil {
		_ = current.server.Close()
	}
	cancel()
	<-current.serveDone
	close(current.shutdownDone)
}

func (manager *Manager) handler(current *session) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
		if !localHostMatches(request.Host, current.origin) {
			http.Error(writer, "请求来源无效", http.StatusForbidden)
			return
		}
		switch request.URL.Path {
		case "/oauth/confirm":
			manager.confirm(writer, request, current)
		case callbackPath:
			manager.callback(writer, request, current)
		default:
			http.NotFound(writer, request)
		}
	})
}

func (manager *Manager) confirm(writer http.ResponseWriter, request *http.Request, current *session) {
	view := current.coordinator.View()
	if request.Method == http.MethodGet {
		if request.URL.Query().Get("session") != view.SessionID {
			http.Error(writer, "会话无效", http.StatusForbidden)
			return
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		err := confirmTemplate.Execute(writer, map[string]any{"Session": view, "Title": channelTitle(view.Channel), "Awaiting": view.Status == "awaiting_user", "Sync": view.Operation == "sync_accounts", "Rebind": current.input.RebindExisting,
			"StatusText": recoveryStatusText(view), "ErrorMessage": recoveryErrorMessage(view), "Polling": view.Status == "authorizing" || view.Status == "syncing"})
		if err == nil && terminal(view.Status) && http.NewResponseController(writer).Flush() == nil {
			current.resultOnce.Do(func() { close(current.resultServed) })
		}
		return
	}
	if request.Method != http.MethodPost {
		http.Error(writer, "仅允许浏览器确认", http.StatusMethodNotAllowed)
		return
	}
	if !localConfirmSource(request, current.origin) {
		http.Error(writer, "请求来源无效", http.StatusForbidden)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 4096)
	if err := request.ParseForm(); err != nil || request.PostForm.Get("session") != view.SessionID {
		http.Error(writer, "确认无效", http.StatusForbidden)
		return
	}
	if err := current.coordinator.StartSync(); err != nil {
		http.Error(writer, "会话已结束或已确认", http.StatusConflict)
		return
	}
	http.Redirect(writer, request, current.origin+"/oauth/confirm?session="+view.SessionID, http.StatusSeeOther)
}

func (manager *Manager) callback(writer http.ResponseWriter, request *http.Request, current *session) {
	if request.Method != http.MethodGet {
		http.Error(writer, "回调方法无效", http.StatusMethodNotAllowed)
		return
	}
	code := strings.TrimSpace(request.URL.Query().Get("auth_code"))
	if code == "" {
		code = strings.TrimSpace(request.URL.Query().Get("code"))
	}
	err := current.coordinator.AcceptCallback(request.URL.Query().Get("state"), code, request.URL.Query().Get("error"))
	if err != nil {
		status := http.StatusForbidden
		var domainErr *domain.Error
		if errors.As(err, &domainErr) && domainErr.Code == "authorization_callback_used" {
			status = http.StatusConflict
		}
		http.Error(writer, "授权回调无效、已使用或已失效", status)
		return
	}
	// Remove the auth code from the browser URL immediately. This page reports
	// completion only after the Core saved both credential and account snapshot.
	http.Redirect(writer, request, current.origin+"/oauth/confirm?session="+current.coordinator.View().SessionID, http.StatusSeeOther)
}

func terminal(status string) bool { return auth.RecoveryTerminal(status) }
func failure(code string) error {
	return domain.NewError(code, "local authorization interaction could not complete", 1, nil)
}
func nonce() (string, error) {
	data := make([]byte, 24)
	_, err := rand.Read(data)
	return hex.EncodeToString(data), err
}
func channelTitle(channel string) string {
	if channel == "star_map" {
		return "巨量星图"
	}
	if channel == "qianchuan" {
		return "巨量千川"
	}
	return "巨量营销"
}
func statusText(status string) string {
	return map[string]string{"awaiting_user": "等待你确认", "awaiting_callback": "等待完成官方授权", "authorizing": "正在确认授权", "syncing": "正在同步可访问账户", "completed": "授权和账户同步已完成", "failed": "授权恢复未完成", "cancelled": "已取消", "expired": "本次操作已过期，请回到会话重新发起"}[status]
}
func recoveryStatusText(view auth.RecoverySession) string {
	if view.Status == "failed" && view.SyncRequired {
		return "授权已保存，账户同步未完成"
	}
	if view.Status == "completed" && view.DiscoveryIssueCount > 0 {
		return "授权已保存，已同步可校验的账户范围；部分账户角色未展开"
	}
	return statusText(view.Status)
}
func errorMessage(code string) string {
	return map[string]string{
		"OAUTH_REJECTED":                  "未完成官方授权，请回到会话重新发起。",
		"AUTHORIZATION_FAILED":            "授权未完成，请回到会话查看失败原因。",
		"OAUTH_APP_MISSING":               "本机尚未配置此渠道的 OAuth App，请先通过受控 CLI 配置 App。",
		"ACCOUNT_SYNC_FAILED":             "账户同步未完成，请回到会话查看具体失败原因。",
		"AUTHORIZATION_SERVICE_FAILED":    "本机授权服务异常结束，请重新发起授权。",
		"REAUTHORIZATION_REQUIRED":        "原授权已失效，请回到会话重新发起授权。",
		"AUTHORIZATION_SCOPE_MISSING":     "这次授权未覆盖待恢复的账户，请使用有权访问该账户的身份重新授权。",
		"AUTHORIZATION_IDENTITY_MISMATCH": "当前应用或登录账号与所选授权不一致，请回到会话选择正确的授权或使用原账号登录。",
		"AUTHORIZATION_NOT_FOUND":         "所选授权已不存在，请回到会话检查当前渠道的授权记录。",
		"AUTHORIZATION_AMBIGUOUS":         "当前登录账号对应多份授权，请回到会话选择要更新的那一份。",
	}[code]
}

func recoveryErrorMessage(view auth.RecoverySession) string {
	message := errorMessage(view.ErrorCode)
	if view.FailureReason == "ACCOUNT_MAPPING_CONFLICT" {
		message = "账户已绑定其他授权，请回到会话明确确认是否改绑；重复同步不会解决此冲突。"
	}
	stage := map[string]string{
		"app_credentials": "读取应用配置", "token_exchange": "兑换官方授权", "authorized_user": "核验官方登录身份",
		"credential_storage": "保存安全凭据", "authorization_storage": "保存授权记录", "account_sync": "同步账户",
	}[view.FailureStage]
	reason := map[string]string{
		"APP_CREDENTIAL_READ_FAILED": "本机应用配置读取失败", "TOKEN_EXCHANGE_FAILED": "官方授权兑换未完成",
		"CREDENTIAL_WRITE_FAILED": "安全凭据库写入失败", "CREDENTIAL_VERIFY_FAILED": "安全凭据保存后的核验未通过",
		"AUTHORIZATION_STORE_FAILED": "授权记录保存失败", "ACCOUNT_SYNC_FAILED": "授权账户同步失败",
		"OFFICIAL_API_ERROR": "官方接口拒绝请求", "OFFICIAL_HTTP_ERROR": "官方接口返回 HTTP 错误",
		"REQUEST_FAILED": "接口请求未成功", "INVALID_TOKEN_RESPONSE": "官方授权响应缺少必要字段",
		"USER_INFO_FAILED": "官方登录用户读取失败", "INVALID_USER_INFO_RESPONSE": "官方用户信息缺少有效用户 ID",
		"AUTHORIZATION_IDENTITY_MISMATCH": "应用或登录用户与所选授权不一致", "AUTHORIZATION_NOT_FOUND": "所选授权不存在", "AUTHORIZATION_AMBIGUOUS": "同一登录用户对应多份授权",
		"ACCOUNT_DISCOVERY_FAILED": "授权账户发现未完成", "ACCOUNT_SNAPSHOT_INVALID": "授权账户数据校验未通过",
		"ACCOUNT_ACTIVATION_FAILED": "授权账户快照保存失败", "ACCOUNT_MAPPING_CONFLICT": "账户与既有授权归属冲突",
	}[view.FailureReason]
	if stage != "" {
		message += " 失败环节：" + stage + "。"
	}
	if reason != "" {
		message += " 原因：" + reason + "。"
	}
	diagnostics := view.FailureDiagnostics()
	endpoint, _ := diagnostics["failed_endpoint"].(string)
	if label := map[string]string{
		"/oauth2/advertiser/get/": "授权账户列表", "/2/customer_center/advertiser/list/": "客户中心广告主列表",
		"/2/ebp/advertiser/list/": "企业工作台广告主列表", "/2/advertiser/info/": "广告主详情校验",
		"/2/agent/advertiser/select/": "千川代理商广告主列表", "/v1.0/qianchuan/shop/advertiser/list/": "千川店铺广告主列表", "/2/star/info/": "星图账户详情校验",
	}[endpoint]; label != "" {
		message += " 失败接口：" + label + "。"
	}
	validation, _ := diagnostics["validation_reason"].(string)
	if label := map[string]string{
		"INVALID_REQUEST": "账户请求参数无效", "INVALID_RESPONSE": "账户响应格式无效", "INVALID_ACCOUNT_LIST": "账户列表字段缺失或格式无效",
		"INVALID_ACCOUNT_ID": "广告账户缺少有效 ID", "INVALID_ACCOUNT_VALIDITY": "账户有效性字段无效", "DUPLICATE_ACCOUNT_ID": "授权账户 ID 重复",
		"INVALID_ADVERTISER_ID": "广告主 ID 缺失或无效", "INVALID_PAGINATION": "分页信息缺失、矛盾或遍历不完整",
		"INVALID_ADVERTISER_INFO": "广告主详情与请求范围不匹配", "DUPLICATE_ADVERTISER_ID": "广告主详情 ID 重复", "INCOMPLETE_ADVERTISER_INFO": "广告主详情返回不完整",
	}[validation]; label != "" {
		message += " 校验原因：" + label + "。"
	}
	if view.OfficialCode > 0 {
		message += " 官方错误码：" + strconv.FormatInt(view.OfficialCode, 10) + "。"
	}
	if view.OfficialRequestID != "" {
		message += " 官方请求编号：" + view.OfficialRequestID + "。"
	}
	return message
}
func localHostMatches(rawHost, expectedOrigin string) bool {
	expected, err := url.Parse(expectedOrigin)
	if err != nil || expected.Scheme != "http" {
		return false
	}
	_, expectedPort, err := net.SplitHostPort(expected.Host)
	if err != nil || expectedPort == "" {
		return false
	}
	host, port, err := net.SplitHostPort(strings.TrimSpace(rawHost))
	return err == nil && port == expectedPort && isLocalBrowserHost(host)
}
func localConfirmSource(request *http.Request, expectedOrigin string) bool {
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin != "" && origin != "null" {
		return localOriginMatches(origin, expectedOrigin, false)
	}
	referer := strings.TrimSpace(request.Header.Get("Referer"))
	if referer != "" {
		return localOriginMatches(referer, expectedOrigin, true)
	}
	// The listener is bound to loopback and the form carries a one-time,
	// high-entropy session nonce. Embedded browsers may omit both source
	// headers, so the Host check plus that nonce is the remaining CSRF boundary.
	return true
}
func localOriginMatches(rawOrigin, expectedOrigin string, confirmPath bool) bool {
	actual, err := url.Parse(rawOrigin)
	if err != nil || actual.Scheme != "http" || actual.User != nil || actual.Host == "" {
		return false
	}
	expected, err := url.Parse(expectedOrigin)
	if err != nil || expected.Scheme != "http" {
		return false
	}
	_, expectedPort, err := net.SplitHostPort(expected.Host)
	if err != nil || expectedPort == "" || actual.Port() != expectedPort || !isLocalBrowserHost(actual.Hostname()) {
		return false
	}
	if confirmPath {
		return actual.Path == "/oauth/confirm"
	}
	return actual.Path == ""
}
func isLocalBrowserHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	return host == "127.0.0.1" || host == "localhost"
}

// AuthorizationURL is shared by the CLI and host interaction. Neither caller
// may supply a provider URL through model parameters.
func AuthorizationURL(channel, appID, redirect, state string) string {
	base := "https://ad.oceanengine.com/openapi/audit/oauth.html"
	if channel == "qianchuan" {
		base = "https://qianchuan.jinritemai.com/openapi/qc/audit/oauth.html"
	}
	query := url.Values{"app_id": {appID}, "redirect_uri": {redirect}, "state": {state}}
	if channel == "qianchuan" {
		query.Set("material_auth", "1")
	}
	return base + "?" + query.Encode()
}

var confirmTemplate = template.Must(template.New("confirm").Parse(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width">{{if .Polling}}<meta http-equiv="refresh" content="2">{{end}}<title>Ocean Watch 授权</title><style>body{font-family:system-ui;background:#f5f6f8;color:#17212b}main{max-width:560px;margin:60px auto;padding:32px;background:white;border-radius:12px}button{width:100%;padding:12px;background:#1468d4;color:white;border:0;border-radius:6px}</style><main><h1>{{.Title}}授权恢复</h1>{{if .Awaiting}}<p>确认后将同步这次授权的官方账户。{{if .Rebind}}与其他授权重叠的账户将改绑到本次授权。{{else}}账户范围不会自动抢占其他授权。{{end}}此操作不会创建或修改投放计划。</p>{{if .Session.AdvertiserID}}<p>待恢复广告主：{{.Session.AdvertiserID}}</p>{{end}}<form method="post" action="/oauth/confirm"><input type="hidden" name="session" value="{{.Session.SessionID}}"><button type="submit">{{if .Rebind}}确认同步并改绑账户{{else}}确认同步账户{{end}}</button></form>{{else}}{{if eq .Session.Status "completed"}}<p>授权及账户同步已完成。可以关闭此页面，回到会话继续原查询或预检。</p>{{else}}<p>当前状态：{{.StatusText}}</p>{{if .Session.ErrorCode}}<p>{{.ErrorMessage}}</p>{{end}}{{if .Session.SyncRequired}}<p>授权已保存，账户同步未完成。请在会话中重试同步。</p>{{end}}<p>返回会话查询结果。取消后已保存的授权不会被删除。</p>{{end}}{{end}}</main></html>`))
