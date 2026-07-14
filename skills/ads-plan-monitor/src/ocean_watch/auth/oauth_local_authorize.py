#!/usr/bin/env python3
import argparse
import json
import secrets
import time
import urllib.parse
import webbrowser
from http.server import BaseHTTPRequestHandler, HTTPServer

import ocean_watch.auth.authorization_store as authorization_store
import ocean_watch.auth.channels as channels
import ocean_watch.auth.token_manager as token_manager
import ocean_watch.core.config_paths as config_paths

DEFAULT_REDIRECT_URI = "http://127.0.0.1:8787/oauth/callback"
DEFAULT_AUTHORIZE_URL = "https://ad.oceanengine.com/openapi/audit/oauth.html"


class OAuthStateError(ValueError):
    def __init__(self, code, message):
        super().__init__(message)
        self.code = code


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
        expected_path = urllib.parse.urlparse(self.server.redirect_uri).path
        if parsed.path != expected_path:
            self.send_response(404)
            self.end_headers()
            self.wfile.write(b"Not found")
            return

        params = urllib.parse.parse_qs(parsed.query)
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

    def log_message(self, format, *args):
        return

    def respond(self, message):
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
    <h1>{message}</h1>
    <p>这个本地页面只用于接收官方回调，不会展示或保存 token。</p>
  </main>
</body>
</html>
"""
        encoded = body.encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)


def get_authorize_url(config, redirect_uri, state):
    app_id = token_manager.get_path(config, "api.app_id")
    if token_manager.is_missing(app_id):
        raise RuntimeError("missing api.app_id")

    authorize_url = (
        token_manager.get_path(config, "oauth.authorize_url")
        or DEFAULT_AUTHORIZE_URL
    )
    params = {
        "app_id": app_id,
        "state": state,
        "redirect_uri": redirect_uri,
    }
    return authorize_url + "?" + urllib.parse.urlencode(params)


def wait_for_callback(redirect_uri, state, channel, timeout):
    parsed = urllib.parse.urlparse(redirect_uri)
    host = parsed.hostname or "127.0.0.1"
    port = parsed.port or 80
    server = HTTPServer((host, port), OAuthCallbackHandler)
    server.redirect_uri = redirect_uri
    server.expected_state = state
    server.expected_channel = channel
    server.result = None

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
    parser = argparse.ArgumentParser()
    parser.add_argument("--config")
    parser.add_argument("--redirect-uri")
    parser.add_argument("--timeout", type=int, default=300)
    parser.add_argument("--no-open", action="store_true", help="Print URL only; do not open browser.")
    parser.add_argument("--print-url", action="store_true")
    parser.add_argument("--channel", default="marketing", choices=("marketing", "qianchuan"))
    parser.add_argument("--rebind-existing", action="store_true")
    args = parser.parse_args(argv)

    config_path = config_paths.resolve_config_path(args.config)
    config = token_manager.load_config(config_path, channel=args.channel, capability="oauth")
    app = authorization_store.read_app(args.channel)
    if not app.get("app_id") or not app.get("secret"):
        raise RuntimeError(
            "missing local OAuth app credentials; run "
            f"ocean-watch auth set-app --config {config_path} --channel {args.channel} first"
        )
    config.setdefault("api", {}).update({key: app[key] for key in ("app_id", "secret") if key in app})
    redirect_uri = (
        args.redirect_uri
        or token_manager.get_path(config, "oauth.redirect_uri")
        or DEFAULT_REDIRECT_URI
    )
    state = build_oauth_state(args.channel)
    authorize_url = get_authorize_url(config, redirect_uri, state)

    if args.print_url or args.no_open:
        print(json.dumps({
            "mode": "oauth_local_authorize",
            "redirect_uri": redirect_uri,
            "authorize_url": authorize_url,
            "safe_note": "Do not paste tokens in chat. This URL contains only app_id, redirect_uri, and state.",
        }, ensure_ascii=False, indent=2), flush=True)

    if not args.no_open:
        webbrowser.open(authorize_url)

    result = wait_for_callback(redirect_uri, state, args.channel, args.timeout)
    if not result.get("ok"):
        print(json.dumps({
            "mode": "oauth_local_authorize",
            "ok": False,
            "error": result.get("error"),
        }, ensure_ascii=False, indent=2), flush=True)
        return 1

    _, exchange_summary = token_manager.exchange_auth_code(
        config_path,
        result["auth_code"],
        config=config,
        channel=result["channel"],
        rebind_existing=args.rebind_existing,
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
