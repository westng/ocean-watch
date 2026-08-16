package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/oceanengine"
	authapplication "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/application/auth"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

const (
	defaultOAuthRedirectURI = "http://127.0.0.1:8787/oauth/callback"
	defaultOAuthTimeout     = 5 * time.Minute
	maxOAuthFormBytes       = 4096
)

type AuthRuntime struct {
	Authorizations authapplication.AuthorizationStore
	RefreshLocker  authapplication.RefreshLocker
	OAuth          authapplication.OAuthAdapter
	Discovery      authapplication.AdvertiserDiscovery
	ClientFactory  *oceanengine.ClientFactory
	Now            func() time.Time
	OpenURL        func(string) error
}

type authOptions struct {
	configPath      string
	channel         string
	authorizationID string
	authAccountID   string
	advertiserID    string
	redirectURI     string
	timeout         time.Duration
	noOpen          bool
	printURL        bool
	configureApp    bool
	rebindExisting  bool
	out             string
}

func parseAuthOptions(action string, args []string) (authOptions, error) {
	options := authOptions{channel: "marketing", redirectURI: defaultOAuthRedirectURI, timeout: defaultOAuthTimeout}
	flags := flag.NewFlagSet("auth "+action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.configPath, "config", "", "")
	flags.StringVar(&options.channel, "channel", "marketing", "")
	flags.StringVar(&options.authorizationID, "authorization-id", "", "")
	flags.StringVar(&options.authAccountID, "auth-account-id", "", "")
	flags.StringVar(&options.advertiserID, "advertiser-id", "", "")
	flags.StringVar(&options.redirectURI, "redirect-uri", defaultOAuthRedirectURI, "")
	flags.DurationVar(&options.timeout, "timeout", defaultOAuthTimeout, "")
	flags.BoolVar(&options.noOpen, "no-open", false, "")
	flags.BoolVar(&options.printURL, "print-url", false, "")
	flags.BoolVar(&options.configureApp, "configure-app", false, "")
	flags.BoolVar(&options.rebindExisting, "rebind-existing", false, "")
	flags.StringVar(&options.out, "out", "", "")
	if err := flags.Parse(args); err != nil {
		return authOptions{}, err
	}
	if len(flags.Args()) != 0 {
		return authOptions{}, errors.New("unexpected positional authorization arguments")
	}
	if options.channel != "marketing" && options.channel != "qianchuan" {
		return authOptions{}, errors.New("channel must be marketing or qianchuan")
	}
	if options.timeout <= 0 || options.timeout > 30*time.Minute {
		return authOptions{}, errors.New("timeout must be between 1ns and 30m")
	}
	if err := validateCLIPositiveID(options.advertiserID, "advertiser_id", false); err != nil {
		return authOptions{}, err
	}
	if err := validateCLIPositiveID(options.authAccountID, "auth_account_id", false); err != nil {
		return authOptions{}, err
	}
	return options, nil
}

func (runner Runner) runAuth(
	ctx context.Context,
	action string,
	args []string,
	stateRoot string,
	credentials authapplication.CredentialStore,
	stdout io.Writer,
	stderr io.Writer,
) int {
	options, err := parseAuthOptions(action, args)
	if err != nil {
		WriteDomainError(stdout, domain.NewError("invalid_arguments", err.Error(), 2, nil))
		return 2
	}
	runtimeConfig := runner.Auth
	authorizations := runtimeConfig.Authorizations
	if authorizations == nil {
		authorizations = filesystem.AuthorizationStore{Root: stateRoot}
	}
	locker := runtimeConfig.RefreshLocker
	if locker == nil {
		locker = filesystem.RefreshLocker{StateRoot: stateRoot}
	}
	factory := runtimeConfig.ClientFactory
	if factory == nil {
		factory, err = oceanengine.NewClientFactory(oceanengine.FactoryOptions{
			SharedQianchuanControl: filesystem.QianchuanRequestController{Root: stateRoot},
		})
		if err != nil {
			WriteDomainError(stdout, domain.NewError("unexpected_error", err.Error(), 1, nil))
			return 1
		}
	}
	oauth := runtimeConfig.OAuth
	if oauth == nil {
		oauth = oceanengine.OAuthAdapter{Factory: factory}
	}
	discovery := runtimeConfig.Discovery
	if discovery == nil {
		discovery = oceanengine.AdvertiserDiscoveryAdapter{Factory: factory}
	}
	manager := &authapplication.TokenManager{
		Credentials: credentials, Authorizations: authorizations, Locks: locker,
		OAuth: oauth, Now: runtimeConfig.Now,
	}

	var result any
	switch action {
	case "set-app":
		result, err = runLocalOAuth(ctx, localOAuthRequest{
			Channel: options.channel, RedirectURI: options.redirectURI, Timeout: options.timeout,
			ConfigureOnly: true, Credentials: credentials, OpenURL: runtimeConfig.OpenURL,
			NoOpen: options.noOpen, PrintURL: options.printURL, Stderr: stderr,
		})
	case "authorize":
		var callback localOAuthResult
		callback, err = runLocalOAuth(ctx, localOAuthRequest{
			Channel: options.channel, RedirectURI: options.redirectURI, Timeout: options.timeout,
			ForceConfigure: options.configureApp, Credentials: credentials,
			OpenURL: runtimeConfig.OpenURL, NoOpen: options.noOpen,
			PrintURL: options.printURL, Stderr: stderr,
		})
		if err == nil {
			app, appErr := readOAuthApp(ctx, credentials, options.channel)
			if appErr != nil {
				err = appErr
				break
			}
			var token domain.OAuthToken
			token, err = oauth.ExchangeCode(ctx, options.channel, app, callback.AuthCode)
			if err == nil {
				result, err = (authapplication.Authorizer{
					Credentials: credentials, Authorizations: authorizations,
					Discovery: discovery, Now: runtimeConfig.Now,
				}).Authorize(ctx, authapplication.AuthorizationRequest{
					Channel: options.channel, Token: token, RebindExisting: options.rebindExisting,
				})
			}
		}
	case "status":
		result, err = (authapplication.QueryService{
			Credentials: credentials, Authorizations: authorizations,
		}).Status(ctx, authapplication.StatusQuery{Channel: options.channel, AdvertiserID: options.advertiserID})
	case "refresh":
		result, err = manager.Ensure(ctx, authapplication.TokenQuery{
			Channel: options.channel, AdvertiserID: options.advertiserID,
			AuthAccountID: options.authAccountID, AuthorizationID: options.authorizationID,
			AllowPending: false, ForceRefresh: true,
		})
	case "sync-accounts":
		result, err = (authapplication.AdvertiserSnapshotSync{
			Tokens: manager, Authorizations: authorizations, Discovery: discovery, Now: runtimeConfig.Now,
		}).Sync(ctx, authapplication.AdvertiserSyncQuery{
			Channel: options.channel, AuthorizationID: options.authorizationID,
			AuthAccountID: options.authAccountID, RebindExisting: options.rebindExisting,
		})
	case "mappings":
		result, err = (authapplication.QueryService{
			Credentials: credentials, Authorizations: authorizations,
		}).Mappings(ctx, authapplication.StatusQuery{Channel: options.channel, AdvertiserID: options.advertiserID})
	default:
		err = errors.New("unsupported authorization action")
	}
	if err != nil {
		var domainErr *domain.Error
		if errors.As(err, &domainErr) {
			WriteDomainError(stdout, domainErr)
			return domainErr.ExitCode
		}
		WriteDomainError(stdout, domain.NewError("authorization_failed", err.Error(), 1, nil))
		return 1
	}
	if err := WriteJSONDestination(stdout, map[string]any{"ok": true, "mode": "authorization_" + strings.ReplaceAll(action, "-", "_"), "result": result}, options.out); err != nil {
		WriteDomainError(stdout, domain.WrapError("configuration_error", "failed to write output", 2, err))
		return 2
	}
	return 0
}

type localOAuthRequest struct {
	Channel        string
	RedirectURI    string
	Timeout        time.Duration
	ConfigureOnly  bool
	ForceConfigure bool
	Credentials    authapplication.CredentialStore
	OpenURL        func(string) error
	NoOpen         bool
	PrintURL       bool
	Stderr         io.Writer
}

type localOAuthResult struct {
	Channel           string `json:"channel"`
	CredentialBackend string `json:"credential_backend,omitempty"`
	AuthCode          string `json:"-"`
}

type localOAuthEvent struct {
	result localOAuthResult
	err    error
}

func runLocalOAuth(ctx context.Context, request localOAuthRequest) (localOAuthResult, error) {
	if request.Credentials == nil {
		return localOAuthResult{}, errors.New("credential store is required")
	}
	redirect, address, err := validateOAuthRedirect(request.RedirectURI)
	if err != nil {
		return localOAuthResult{}, err
	}
	stateNonce, err := randomHex(24)
	if err != nil {
		return localOAuthResult{}, err
	}
	state, err := domain.BuildOAuthState(request.Channel, stateNonce)
	if err != nil {
		return localOAuthResult{}, err
	}
	setupToken, err := randomHex(24)
	if err != nil {
		return localOAuthResult{}, err
	}
	app, err := readOAuthApp(ctx, request.Credentials, request.Channel)
	if err != nil {
		return localOAuthResult{}, err
	}
	requiresSetup := request.ConfigureOnly || request.ForceConfigure || app.AppID == "" || app.Secret == ""
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return localOAuthResult{}, fmt.Errorf("bind local OAuth server: %w", err)
	}
	defer listener.Close()
	events := make(chan localOAuthEvent, 1)
	send := func(event localOAuthEvent) {
		select {
		case events <- event:
		default:
		}
	}
	var handler http.Handler
	handler = localOAuthHandler(localOAuthHandlerConfig{
		Channel: request.Channel, RedirectURI: redirect.String(), State: state,
		SetupToken: setupToken, ConfigureOnly: request.ConfigureOnly,
		Credentials: request.Credentials, InitialApp: app, Send: send,
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	serverErrors := make(chan error, 1)
	go func() {
		serveErr := server.Serve(listener)
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			serverErrors <- serveErr
		}
	}()
	startURL := oauthAuthorizeURL(request.Channel, app.AppID, redirect.String(), state)
	if requiresSetup {
		startURL = "http://" + address + "/oauth/setup?setup_token=" + url.QueryEscape(setupToken)
	}
	if request.PrintURL || request.NoOpen {
		_, _ = fmt.Fprintf(request.Stderr, "Open this local OAuth URL: %s\n", startURL)
	}
	if !request.NoOpen {
		opener := request.OpenURL
		if opener == nil {
			opener = openSystemURL
		}
		if err := opener(startURL); err != nil {
			_, _ = fmt.Fprintf(request.Stderr, "Unable to open the browser automatically. Open: %s\n", startURL)
		}
	}
	waitCtx, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()
	var event localOAuthEvent
	select {
	case event = <-events:
	case err = <-serverErrors:
		event.err = err
	case <-waitCtx.Done():
		event.err = domain.NewError("oauth_timeout", "local OAuth session timed out", 1, nil)
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
	return event.result, event.err
}

type localOAuthHandlerConfig struct {
	Channel       string
	RedirectURI   string
	State         string
	SetupToken    string
	ConfigureOnly bool
	Credentials   authapplication.CredentialStore
	InitialApp    domain.OAuthApp
	Send          func(localOAuthEvent)
}

func localOAuthHandler(config localOAuthHandlerConfig) http.Handler {
	app := config.InitialApp
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/setup", func(writer http.ResponseWriter, request *http.Request) {
		secureLocalHeaders(writer)
		if request.Method == http.MethodGet {
			if request.URL.Query().Get("setup_token") != config.SetupToken {
				http.Error(writer, "配置会话无效", http.StatusForbidden)
				return
			}
			writeOAuthSetupPage(writer, config.Channel, config.SetupToken, "")
			return
		}
		if request.Method != http.MethodPost || request.ContentLength <= 0 || request.ContentLength > maxOAuthFormBytes {
			http.Error(writer, "配置请求无效", http.StatusBadRequest)
			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, maxOAuthFormBytes)
		if err := request.ParseForm(); err != nil || request.Form.Get("setup_token") != config.SetupToken {
			http.Error(writer, "配置请求无效", http.StatusBadRequest)
			return
		}
		app = domain.OAuthApp{AppID: strings.TrimSpace(request.Form.Get("app_id")), Secret: strings.TrimSpace(request.Form.Get("secret"))}
		if !positiveIdentifier(app.AppID) || app.Secret == "" || len(app.Secret) > 512 {
			writeOAuthSetupPage(writer, config.Channel, config.SetupToken, "请检查 APP ID 和 Secret")
			return
		}
		account, _ := domain.AppCredentialAccount(config.Channel)
		backend, err := config.Credentials.Write(request.Context(), account, map[string]any{"app_id": app.AppID, "secret": app.Secret})
		if err != nil {
			writeOAuthSetupPage(writer, config.Channel, config.SetupToken, "系统凭据库写入失败")
			return
		}
		if config.ConfigureOnly {
			config.Send(localOAuthEvent{result: localOAuthResult{Channel: config.Channel, CredentialBackend: backend}})
			writeOAuthMessage(writer, "应用配置已安全保存，可以关闭页面。")
			return
		}
		http.Redirect(writer, request, oauthAuthorizeURL(config.Channel, app.AppID, config.RedirectURI, config.State), http.StatusSeeOther)
	})
	callbackPath, _ := url.Parse(config.RedirectURI)
	mux.HandleFunc(callbackPath.Path, func(writer http.ResponseWriter, request *http.Request) {
		secureLocalHeaders(writer)
		if request.Method != http.MethodGet {
			http.Error(writer, "授权请求无效", http.StatusMethodNotAllowed)
			return
		}
		if stateErr := domain.ValidateOAuthCallbackState(request.URL.Query().Get("state"), config.State, config.Channel); stateErr != nil {
			config.Send(localOAuthEvent{err: stateErr})
			http.Error(writer, "授权 state 校验失败", http.StatusBadRequest)
			return
		}
		if platformError := strings.TrimSpace(request.URL.Query().Get("error")); platformError != "" {
			config.Send(localOAuthEvent{err: domain.NewError("oauth_rejected", "official OAuth authorization was not completed", 1, nil)})
			http.Error(writer, "授权未完成", http.StatusBadRequest)
			return
		}
		code := strings.TrimSpace(request.URL.Query().Get("auth_code"))
		if code == "" {
			code = strings.TrimSpace(request.URL.Query().Get("code"))
		}
		if code == "" {
			config.Send(localOAuthEvent{err: domain.NewError("oauth_code_missing", "OAuth callback code is required", 1, nil)})
			http.Error(writer, "授权回调缺少 auth_code", http.StatusBadRequest)
			return
		}
		config.Send(localOAuthEvent{result: localOAuthResult{Channel: config.Channel, AuthCode: code}})
		writeOAuthMessage(writer, "授权成功，可以关闭页面，回到 Codex 继续。")
	})
	return mux
}

func validateOAuthRedirect(value string) (*url.URL, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, "", errors.New("OAuth redirect URI must be a plain loopback HTTP URL")
	}
	hostname := parsed.Hostname()
	ip := net.ParseIP(hostname)
	if hostname != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, "", errors.New("OAuth redirect URI must use a loopback host")
	}
	if parsed.Port() == "" || parsed.Path == "" || parsed.Path == "/" {
		return nil, "", errors.New("OAuth redirect URI must include a port and callback path")
	}
	return parsed, net.JoinHostPort(hostname, parsed.Port()), nil
}

func oauthAuthorizeURL(channel, appID, redirectURI, state string) string {
	base := "https://ad.oceanengine.com/openapi/audit/oauth.html"
	if channel == "qianchuan" {
		base = "https://qianchuan.jinritemai.com/openapi/qc/audit/oauth.html"
	}
	values := url.Values{"app_id": {appID}, "redirect_uri": {redirectURI}, "state": {state}}
	if channel == "qianchuan" {
		values.Set("material_auth", "1")
	}
	return base + "?" + values.Encode()
}

func readOAuthApp(ctx context.Context, credentials authapplication.CredentialStore, channel string) (domain.OAuthApp, error) {
	account, err := domain.AppCredentialAccount(channel)
	if err != nil {
		return domain.OAuthApp{}, err
	}
	value, err := credentials.Read(ctx, account)
	if err != nil {
		return domain.OAuthApp{}, err
	}
	return domain.OAuthApp{AppID: credentialSafeString(value, "app_id"), Secret: credentialSafeString(value, "secret")}, nil
}

func openSystemURL(value string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", value)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", value)
	default:
		command = exec.Command("xdg-open", value)
	}
	return command.Start()
}

func secureLocalHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
}

var oauthSetupTemplate = template.Must(template.New("oauth-setup").Parse(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>巨量应用配置</title><style>body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#f5f6f8;margin:0}main{max-width:520px;margin:64px auto;padding:32px;background:#fff;border-radius:8px}label{display:block;margin:18px 0 6px;font-weight:600}input,button{box-sizing:border-box;width:100%;height:42px;padding:0 12px}button{margin-top:24px;background:#1269d3;color:#fff;border:0;border-radius:6px}.error{color:#b42318}</style></head><body><main><h1>{{.Title}}应用配置</h1><p>应用信息只写入当前电脑的系统凭据库。</p>{{if .Error}}<p class="error">{{.Error}}</p>{{end}}<form method="post" action="/oauth/setup" autocomplete="off"><input type="hidden" name="setup_token" value="{{.Token}}"><label>APP ID</label><input name="app_id" inputmode="numeric" required autofocus><label>Secret</label><input name="secret" type="password" required><button type="submit">保存并继续</button></form></main></body></html>`))

func writeOAuthSetupPage(writer http.ResponseWriter, channel, token, message string) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	title := "巨量营销"
	if channel == "qianchuan" {
		title = "巨量千川"
	}
	_ = oauthSetupTemplate.Execute(writer, map[string]string{"Title": title, "Token": token, "Error": message})
}

func writeOAuthMessage(writer http.ResponseWriter, message string) {
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(writer, message+"\n")
}

func randomHex(size int) (string, error) {
	payload := make([]byte, size)
	if _, err := rand.Read(payload); err != nil {
		return "", err
	}
	return hex.EncodeToString(payload), nil
}

func credentialSafeString(value map[string]any, key string) string {
	text := strings.TrimSpace(fmt.Sprint(value[key]))
	if text == "<nil>" || strings.HasPrefix(text, "REPLACE_WITH") {
		return ""
	}
	return text
}

func credentialPresent(value map[string]any, key string) bool {
	return credentialSafeString(value, key) != ""
}

func positiveIdentifier(value string) bool {
	return value != "" && value != "0" && value[0] != '0' && domain.ValidateDecimalID(value, "id") == nil
}

func positiveCLIInteger(value any, fallback int) (int, error) {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return fallback, nil
	}
	var parsed int
	if _, err := fmt.Sscan(text, &parsed); err != nil || parsed < 1 {
		return 0, errors.New("token_revision must be positive")
	}
	return parsed, nil
}

func sortedMapKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func stringValues(value any) []string {
	result := []string{}
	switch values := value.(type) {
	case []any:
		for _, item := range values {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" && text != "<nil>" {
				result = append(result, text)
			}
		}
	case []string:
		result = append(result, values...)
	}
	return result
}

func objectMaps(value any) []map[string]any {
	result := []map[string]any{}
	values, _ := value.([]any)
	for _, item := range values {
		if row, ok := item.(map[string]any); ok {
			result = append(result, row)
		}
	}
	return result
}

func sanitizedAuthorizedAccounts(value any) []map[string]any {
	result := []map[string]any{}
	for _, row := range objectMaps(value) {
		result = append(result, map[string]any{
			"account_id": row["account_id"], "account_name": row["account_name"],
			"account_role": row["account_role"], "account_type": row["account_type"],
			"advertiser_ids": stringValues(row["advertiser_ids"]),
		})
	}
	return result
}
