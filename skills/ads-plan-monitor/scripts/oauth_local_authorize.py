#!/usr/bin/env python3
import argparse
import json
import secrets
import time
import urllib.parse
import webbrowser
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path

import credential_store
import token_manager


DEFAULT_REDIRECT_URI = "http://127.0.0.1:8787/oauth/callback"
DEFAULT_AUTHORIZE_URL = "https://ad.oceanengine.com/openapi/audit/oauth.html"


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

        if state != self.server.expected_state:
            self.server.result = {
                "ok": False,
                "error": "state_mismatch",
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


def wait_for_callback(redirect_uri, state, timeout):
    parsed = urllib.parse.urlparse(redirect_uri)
    host = parsed.hostname or "127.0.0.1"
    port = parsed.port or 80
    server = HTTPServer((host, port), OAuthCallbackHandler)
    server.redirect_uri = redirect_uri
    server.expected_state = state
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


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", default="config/ads-plan-monitor/config.json")
    parser.add_argument("--redirect-uri")
    parser.add_argument("--timeout", type=int, default=300)
    parser.add_argument("--no-open", action="store_true", help="Print URL only; do not open browser.")
    parser.add_argument("--print-url", action="store_true")
    args = parser.parse_args()

    config_path = Path(args.config).expanduser()
    config = token_manager.load_config(config_path)
    credential_status = credential_store.status(config_path)
    if not credential_status.get("has_app_id") or not credential_status.get("has_secret"):
        raise RuntimeError(
            "missing local OAuth app credentials; run "
            f"scripts/credential_store.py --config {config_path} --set-app first"
        )
    redirect_uri = (
        args.redirect_uri
        or token_manager.get_path(config, "oauth.redirect_uri")
        or DEFAULT_REDIRECT_URI
    )
    state = secrets.token_urlsafe(24)
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

    result = wait_for_callback(redirect_uri, state, args.timeout)
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
    )
    print(json.dumps({
        "mode": "oauth_local_authorize",
        "ok": True,
        "config": str(config_path),
        "redirect_uri": redirect_uri,
        "token_update": exchange_summary,
    }, ensure_ascii=False, indent=2), flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
