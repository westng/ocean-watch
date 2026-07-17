#!/usr/bin/env python3
import argparse
import html
import ipaddress
import json
import secrets
import threading
import time
import urllib.parse
import webbrowser
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import ocean_watch.auth.authorization_store as authorization_store
import ocean_watch.auth.channel_adapters as channel_adapters
import ocean_watch.auth.channels as channels
import ocean_watch.auth.credential_store as credential_store
import ocean_watch.auth.token_manager as token_manager
import ocean_watch.core.config_paths as config_paths
from ocean_watch.api.client import official_https_url

DEFAULT_REDIRECT_URI = "http://127.0.0.1:8787/oauth/callback"
APP_SETUP_PATH = "/oauth/setup"
MAX_FORM_BYTES = 4096
CALLBACK_QUERY_FIELDS = ("state", "auth_code", "code", "error", "message")


class OAuthStateError(ValueError):
    def __init__(self, code, message):
        super().__init__(message)
        self.code = code


class OAuthHTTPServer(ThreadingHTTPServer):
    daemon_threads = True
    block_on_close = False


def build_oauth_state(channel, nonce=None):
    random_value = str(nonce or secrets.token_urlsafe(24)).strip()
    if not random_value:
        raise ValueError("OAuth state random value is required")
    return f"{channels.oauth_state_code(channel)}.{random_value}"


def channel_from_oauth_state(state):
    code, separator, random_value = str(state or "").partition(".")
    if not separator or not random_value:
        raise ValueError("OAuth state random value is required")
    try:
        return channels.channel_from_oauth_state_code(code)
    except channels.ChannelError as error:
        raise ValueError(str(error)) from error


def validate_callback_state(state, expected_state, expected_channel):
    if not secrets.compare_digest(str(state or ""), str(expected_state or "")):
        raise OAuthStateError("state_mismatch", "OAuth state does not match the current session")
    try:
        callback_channel = channel_from_oauth_state(state)
    except ValueError as error:
        raise OAuthStateError("state_invalid", str(error)) from error
    if callback_channel != expected_channel:
        raise OAuthStateError(
            "state_channel_mismatch",
            f"OAuth state channel {callback_channel} does not match {expected_channel}",
        )
    return callback_channel


class OAuthCallbackHandler(BaseHTTPRequestHandler):
    server_version = "AdsPlanMonitorOAuth/1.0"

    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        if parsed.path == "/":
            self.respond(
                "本地授权服务已启动",
                detail=(
                    "请继续当前应用配置流程。这个地址不是配置或授权入口。"
                    if self.server.requires_app_configuration
                    else "请在官方授权页面完成授权。这个地址不是配置或授权入口，只负责接收官方回调。"
                ),
            )
            return

        if parsed.path == APP_SETUP_PATH and self.server.requires_app_configuration:
            params = urllib.parse.parse_qs(parsed.query)
            setup_token = (params.get("setup_token") or [""])[0]
            if not secrets.compare_digest(setup_token, self.server.setup_token):
                self.respond("配置会话无效，请关闭页面后重新运行授权。", status=403)
                return
            self.respond_html(render_app_setup_page(
                self.server.channel_display_name,
                self.server.setup_token,
            ))
            return

        if self.server.requires_app_configuration:
            self.respond("请先完成本地应用配置。", status=409)
            return

        expected_path = urllib.parse.urlparse(self.server.redirect_uri).path
        if normalize_callback_path(parsed.path) != normalize_callback_path(expected_path):
            self.respond(
                "授权地址无效",
                status=404,
                detail="请返回 Codex 重新运行授权命令，不要手动修改本地回调地址。",
            )
            return

        params = urllib.parse.parse_qs(parsed.query)
        if not any(params.get(field) for field in CALLBACK_QUERY_FIELDS):
            self.respond(
                "正在等待官方授权回调",
                detail="回调地址不是授权入口，请返回官方授权页面继续，或在 Codex 中重新运行授权命令。",
            )
            return
        state = (params.get("state") or [""])[0]
        auth_code = (params.get("auth_code") or params.get("code") or [""])[0]
        error = (params.get("error") or params.get("message") or [""])[0]

        try:
            callback_channel = validate_callback_state(
                state,
                self.server.expected_state,
                self.server.expected_channel,
            )
        except OAuthStateError as error:
            self.server.result = {
                "ok": False,
                "error": error.code,
            }
            self.respond("授权失败：state 校验不一致，请关闭页面后重新运行授权。")
            return

        if error:
            self.server.result = {
                "ok": False,
                "error": error,
            }
            self.respond("授权失败：平台返回错误，请回到 Codex 查看详情。")
            return

        if token_manager.is_missing(auth_code):
            self.server.result = {
                "ok": False,
                "error": "missing_auth_code",
            }
            self.respond("授权失败：回调里没有 auth_code，请回到 Codex 查看详情。")
            return

        self.server.result = {
            "ok": True,
            "auth_code": auth_code,
            "state": state,
            "channel": callback_channel,
        }
        self.respond("授权成功，可以关闭这个页面，回到 Codex 继续。")

    def do_POST(self):
        parsed = urllib.parse.urlparse(self.path)
        if parsed.path != APP_SETUP_PATH or not (
            self.server.requires_app_configuration
            or self.server.app_configuration_completed
        ):
            self.respond(
                "授权地址无效",
                status=404,
                detail="请返回 Codex 重新运行授权命令，不要手动修改本地授权地址。",
            )
            return
        content_type = self.headers.get("Content-Type", "")
        try:
            content_length = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            content_length = 0
        if (
            content_type.split(";", 1)[0].strip().lower()
            != "application/x-www-form-urlencoded"
            or content_length <= 0
            or content_length > MAX_FORM_BYTES
        ):
            self.respond("配置请求无效，请返回后重试。", status=400)
            return
        form = urllib.parse.parse_qs(
            self.rfile.read(content_length).decode("utf-8"),
            keep_blank_values=True,
        )
        setup_token = (form.get("setup_token") or [""])[0]
        if not secrets.compare_digest(setup_token, self.server.setup_token):
            self.respond("配置会话无效，请关闭页面后重新运行授权。", status=403)
            return

        with self.server.app_configuration_lock:
            if not self.server.app_configuration_completed:
                app_id = (form.get("app_id") or [""])[0].strip()
                secret = (form.get("secret") or [""])[0].strip()
                try:
                    validate_app_credentials(app_id, secret)
                    credential_store.configure_app(
                        app_id,
                        secret,
                        channel=self.server.expected_channel,
                    )
                    self.server.config.setdefault("api", {}).update({
                        "app_id": app_id,
                        "secret": secret,
                    })
                    if not self.server.configure_app_only:
                        self.server.app_authorize_url = get_authorize_url(
                            self.server.config,
                            self.server.redirect_uri,
                            self.server.expected_state,
                        )
                except (RuntimeError, ValueError):
                    self.respond_html(render_app_setup_page(
                        self.server.channel_display_name,
                        self.server.setup_token,
                        error="保存失败，请检查两个字段并确认系统凭据库可用。",
                    ), status=400)
                    return

                self.server.app_configuration_completed = True
                self.server.requires_app_configuration = False

            if self.server.configure_app_only:
                self.server.result = {
                    "ok": True,
                    "mode": "app_configured",
                    "channel": self.server.expected_channel,
                }
                self.respond("应用配置已安全保存，可以关闭这个页面。")
                return

            authorize_url = self.server.app_authorize_url

        self.redirect(authorize_url)

    def log_message(self, format, *args):
        return

    def redirect(self, location):
        self.send_response(303)
        self.send_header("Location", location)
        self.send_header("Content-Length", "0")
        self.send_header("Connection", "close")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Referrer-Policy", "no-referrer")
        self.end_headers()
        self.close_connection = True

    def respond(
        self,
        message,
        status=200,
        detail="这个本地页面只用于接收官方回调，不会展示或保存 token。",
    ):
        body = f"""<!doctype html>
<html lang=\"zh-CN\">
<head>
  <meta charset=\"utf-8\">
  <title>巨量授权</title>
  <style>
    body {{ font-family: -apple-system, BlinkMacSystemFont, \"Segoe UI\", sans-serif; margin: 48px; line-height: 1.6; }}
    main {{ max-width: 680px; }}
  </style>
</head>
<body>
  <main>
    <h1>{html.escape(message)}</h1>
    <p>{html.escape(detail)}</p>
  </main>
</body>
</html>
"""
        self.respond_html(body, status=status)

    def respond_html(self, body, status=200):
        encoded = body.encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(encoded)))
        self.send_header("Cache-Control", "no-store")
        self.send_header(
            "Content-Security-Policy",
            "default-src 'none'; style-src 'unsafe-inline'; "
            f"form-action 'self' {' '.join(self.server.authorize_origins)}; "
            "frame-ancestors 'none'",
        )
        self.send_header("Referrer-Policy", "no-referrer")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.end_headers()
        self.wfile.write(encoded)


def validate_app_credentials(app_id, secret):
    if token_manager.is_missing(app_id) or token_manager.is_missing(secret):
        raise ValueError("app_id and secret are required")
    if len(str(app_id)) > 128 or len(str(secret)) > 512:
        raise ValueError("app credentials exceed the allowed length")


def normalize_callback_path(path):
    normalized = str(path or "/")
    return normalized.rstrip("/") or "/"


def render_app_setup_page(channel_display_name, setup_token, error=None):
    error_html = (
        f'<p class="error" role="alert">{html.escape(error)}</p>'
        if error
        else ""
    )
    return f"""<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{html.escape(channel_display_name)}应用配置</title>
  <style>
    * {{ box-sizing: border-box; }}
    body {{ margin: 0; color: #171717; background: #f5f6f8; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }}
    main {{ width: min(520px, calc(100% - 32px)); margin: 64px auto; padding: 32px; background: white; border: 1px solid #dedede; border-radius: 8px; }}
    h1 {{ margin: 0 0 8px; font-size: 24px; letter-spacing: 0; }}
    p {{ margin: 0 0 24px; color: #555; line-height: 1.6; }}
    label {{ display: block; margin: 18px 0 6px; font-weight: 600; }}
    input {{ width: 100%; height: 42px; padding: 0 12px; border: 1px solid #b8b8b8; border-radius: 6px; font: inherit; }}
    input:focus {{ border-color: #1269d3; outline: 2px solid #cfe3ff; }}
    button {{ width: 100%; height: 42px; margin-top: 24px; border: 0; border-radius: 6px; color: white; background: #1269d3; font: inherit; font-weight: 600; cursor: pointer; }}
    .error {{ margin-bottom: 16px; color: #b42318; }}
    .note {{ margin-top: 16px; margin-bottom: 0; font-size: 13px; }}
  </style>
</head>
<body>
  <main>
    <h1>{html.escape(channel_display_name)}应用配置</h1>
    <p>一次填写应用信息，提交后继续官方授权。</p>
    {error_html}
    <form method="post" action="{APP_SETUP_PATH}" autocomplete="off">
      <input type="hidden" name="setup_token" value="{html.escape(setup_token, quote=True)}">
      <label for="app_id">APP ID</label>
      <input id="app_id" name="app_id" type="text" inputmode="numeric" maxlength="128" required autofocus>
      <label for="secret">Secret</label>
      <input id="secret" name="secret" type="password" maxlength="512" required>
      <button type="submit">保存并继续授权</button>
    </form>
    <p class="note">应用信息只写入当前电脑的系统凭据库，不会进入项目配置或浏览器存储。</p>
  </main>
</body>
</html>
"""


def get_authorize_url(config, redirect_uri, state):
    app_id = token_manager.get_path(config, "api.app_id")
    if token_manager.is_missing(app_id):
        raise RuntimeError("missing api.app_id")

    channel = channels.selected_channel(config)
    adapter = channel_adapters.get_adapter(channel, capability="oauth")
    authorize_url = token_manager.get_path(config, "oauth.authorize_url") or adapter.authorize_url
    if not official_https_url(authorize_url):
        raise RuntimeError("OAuth authorize URL must use an official HTTPS host")
    parsed = urllib.parse.urlparse(authorize_url)
    if parsed.query or parsed.fragment:
        raise RuntimeError("OAuth authorize URL must not contain a query or fragment")
    params = adapter.authorize_params(app_id, state, redirect_uri)
    return authorize_url + "?" + urllib.parse.urlencode(params)


def create_local_server(
    redirect_uri,
    state,
    channel,
    config,
    requires_app_configuration=False,
    configure_app_only=False,
):
    parsed = urllib.parse.urlparse(redirect_uri)
    host = parsed.hostname or "127.0.0.1"
    if host != "localhost":
        try:
            if not ipaddress.ip_address(host).is_loopback:
                raise ValueError("OAuth redirect URI must use a loopback host")
        except ValueError as error:
            if str(error) == "OAuth redirect URI must use a loopback host":
                raise
            raise ValueError("OAuth redirect URI must use a loopback host") from error
    port = parsed.port if parsed.port is not None else 80
    _, channel_definition = channels.get(channel, capability="oauth")
    adapter = channel_adapters.get_adapter(channel, capability="oauth")
    authorize_url = token_manager.get_path(config, "oauth.authorize_url") or adapter.authorize_url
    if not official_https_url(authorize_url):
        raise RuntimeError("OAuth authorize URL must use an official HTTPS host")
    parsed_authorize_url = urllib.parse.urlparse(authorize_url)
    authorize_origins = {
        f"{parsed_authorize_url.scheme}://{parsed_authorize_url.netloc}",
        *adapter.authorize_navigation_origins,
    }
    if not all(official_https_url(origin) for origin in authorize_origins):
        raise RuntimeError("OAuth navigation origins must use official HTTPS hosts")
    server = OAuthHTTPServer((host, port), OAuthCallbackHandler)
    server.redirect_uri = redirect_uri
    server.expected_state = state
    server.expected_channel = channel
    server.channel_display_name = channel_definition["display_name"]
    server.authorize_origins = tuple(sorted(authorize_origins))
    server.config = config
    server.requires_app_configuration = requires_app_configuration
    server.configure_app_only = configure_app_only
    server.app_configuration_completed = False
    server.app_configuration_lock = threading.Lock()
    server.app_authorize_url = None
    server.setup_token = secrets.token_urlsafe(24)
    server.result = None
    return server


def app_setup_url(server):
    return (
        f"http://{server.server_address[0]}:{server.server_address[1]}"
        f"{APP_SETUP_PATH}?{urllib.parse.urlencode({'setup_token': server.setup_token})}"
    )


def wait_for_callback(server, timeout):

    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline and server.result is None:
        server.timeout = 0.5
        server.handle_request()
    result = server.result
    server.server_close()
    if not result:
        raise TimeoutError(f"authorization timed out after {timeout} seconds")
    return result


def main(argv=None):
    configure_app_only = "--configure-app-only" in (argv or [])
    parser = argparse.ArgumentParser()
    parser.add_argument("--config")
    parser.add_argument("--redirect-uri")
    parser.add_argument("--timeout", type=int, default=300)
    parser.add_argument("--no-open", action="store_true", help="Print URL only; do not open browser.")
    parser.add_argument("--print-url", action="store_true")
    parser.add_argument("--channel", default="marketing", choices=("marketing", "qianchuan"))
    if not configure_app_only:
        parser.add_argument("--rebind-existing", action="store_true")
        parser.add_argument("--configure-app", action="store_true")
    parser.add_argument("--configure-app-only", action="store_true", help=argparse.SUPPRESS)
    args = parser.parse_args(argv)

    config_path = config_paths.resolve_config_path(args.config)
    config = token_manager.load_config(config_path, channel=args.channel, capability="oauth")
    app = authorization_store.read_app(args.channel)
    has_app = bool(app.get("app_id") and app.get("secret"))
    if has_app:
        config.setdefault("api", {}).update({key: app[key] for key in ("app_id", "secret")})
    redirect_uri = (
        args.redirect_uri
        or token_manager.get_path(config, "oauth.redirect_uri")
        or DEFAULT_REDIRECT_URI
    )
    state = build_oauth_state(args.channel)
    requires_app_configuration = (
        getattr(args, "configure_app", False)
        or args.configure_app_only
        or not has_app
    )
    server = create_local_server(
        redirect_uri,
        state,
        args.channel,
        config,
        requires_app_configuration=requires_app_configuration,
        configure_app_only=args.configure_app_only,
    )
    start_url = (
        app_setup_url(server)
        if requires_app_configuration
        else get_authorize_url(config, redirect_uri, state)
    )

    if args.print_url or args.no_open:
        print(json.dumps({
            "mode": "app_configuration" if requires_app_configuration else "oauth_local_authorize",
            "redirect_uri": redirect_uri,
            "redirect_uri_usage": "official_registration_and_callback_only_do_not_open_directly",
            "start_url": start_url,
            "start_url_usage": "open_this_url_to_begin",
            "requires_app_configuration": requires_app_configuration,
            "safe_note": "Do not paste app secrets or tokens in chat.",
        }, ensure_ascii=False, indent=2), flush=True)

    if not args.no_open:
        webbrowser.open(start_url)

    result = wait_for_callback(server, args.timeout)
    if not result.get("ok"):
        print(json.dumps({
            "mode": "oauth_local_authorize",
            "ok": False,
            "error": result.get("error"),
        }, ensure_ascii=False, indent=2), flush=True)
        return 1

    if result.get("mode") == "app_configured":
        print(json.dumps({
            "mode": "app_configuration",
            "ok": True,
            "channel": result["channel"],
            "credential_backend": credential_store.backend_name(),
        }, ensure_ascii=False, indent=2), flush=True)
        return 0

    _, exchange_summary = token_manager.exchange_auth_code(
        config_path,
        result["auth_code"],
        config=config,
        channel=result["channel"],
        rebind_existing=getattr(args, "rebind_existing", False),
    )
    print(json.dumps({
        "mode": "oauth_local_authorize",
        "ok": True,
        "channel": result["channel"],
        "config": str(config_path),
        "redirect_uri": redirect_uri,
        "token_update": exchange_summary,
    }, ensure_ascii=False, indent=2), flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
