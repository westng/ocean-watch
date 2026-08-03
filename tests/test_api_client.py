import datetime as dt
import io
import json
import tempfile
import threading
import time
import unittest
import urllib.error
import urllib.request
from pathlib import Path
from unittest import mock

from ocean_watch.api import client
from ocean_watch.api.qianchuan import QianchuanClientFactory
from ocean_watch.core.errors import ApiError


class FakeResponse:
    def __init__(self, payload, headers=None):
        self.body = json.dumps(payload).encode("utf-8")
        self.headers = headers or {}

    def __enter__(self):
        return self

    def __exit__(self, *_args):
        return False

    def read(self, size=-1):
        return self.body[:size]


class FakeClock:
    def __init__(self):
        self.now = 0.0
        self.sleeps = []

    def monotonic(self):
        return self.now

    def wall_time(self):
        return self.now

    def sleep(self, seconds):
        self.sleeps.append(seconds)
        self.now += seconds


class OceanEngineClientTests(unittest.TestCase):
    def test_default_client_requires_official_https_host(self):
        for base_url in (
            "http://api.oceanengine.com/open_api",
            "https://example.com/open_api",
            "file:///tmp/open_api",
        ):
            with self.subTest(base_url=base_url), self.assertRaises(ApiError):
                client.OceanEngineClient(base_url, "token")

    def test_default_transport_refuses_redirects(self):
        request = urllib.request.Request("https://api.oceanengine.com/open_api/test")
        with mock.patch.object(urllib.request, "build_opener") as build:
            build.return_value.open.side_effect = urllib.error.HTTPError(
                request.full_url,
                302,
                "redirect",
                {},
                None,
            )
            with self.assertRaises(urllib.error.HTTPError):
                client.default_opener(request, 30)
        self.assertIsInstance(build.call_args.args[0], client.RejectRedirects)

    def test_injected_transport_is_bounded_and_keeps_query_encoding(self):
        calls = []

        def opener(request, timeout):
            calls.append((request, timeout))
            return FakeResponse({"code": 0})

        api = client.OceanEngineClient(
            "https://api.test/open_api",
            "token",
            opener=opener,
            max_response_bytes=128,
        )
        self.assertEqual(api.get("/path", {"ids": [1, 2]}), {"code": 0})
        self.assertIn("ids=%5B1%2C+2%5D", calls[0][0].full_url)
        self.assertEqual(calls[0][0].get_header("Access-token"), "token")

    def test_http_error_payload_is_bounded_and_token_is_redacted(self):
        token = "never-print-token"
        error = urllib.error.HTTPError(
            "https://api.oceanengine.com/open_api/path",
            503,
            "unavailable",
            {},
            io.BytesIO(json.dumps({"message": f"token {token}"}).encode("utf-8")),
        )

        def opener(_request, timeout):
            self.assertEqual(timeout, 30)
            raise error

        result = client.OceanEngineClient(
            "https://api.test/open_api",
            token,
            opener=opener,
        ).get("/path")
        self.assertEqual(result["code"], 503)
        self.assertNotIn(token, json.dumps(result))

    def test_retry_after_seconds_accepts_seconds_and_rejects_invalid_values(self):
        self.assertEqual(client.retry_after_seconds({"Retry-After": "2.5"}), 2.5)
        self.assertIsNone(client.retry_after_seconds({"Retry-After": "later"}))
        self.assertIsNone(client.retry_after_seconds({"Retry-After": "-1"}))
        self.assertIsNone(client.retry_after_seconds({"Retry-After": "inf"}))
        self.assertIsNone(client.retry_after_seconds({}))

    def test_retry_after_seconds_accepts_http_date(self):
        now = dt.datetime(2026, 8, 3, 12, 0, tzinfo=dt.timezone.utc)
        self.assertEqual(
            client.retry_after_seconds(
                {"Retry-After": "Mon, 03 Aug 2026 12:00:07 GMT"},
                now_fn=now.timestamp,
            ),
            7.0,
        )

    def test_shared_throttle_spaces_requests_across_clients(self):
        clock = FakeClock()
        throttle = client.RequestThrottle(
            minimum_interval=0.25,
            monotonic_fn=clock.monotonic,
            sleep_fn=clock.sleep,
        )
        openers = [
            lambda _request, timeout: FakeResponse({"code": 0}),
            lambda _request, timeout: FakeResponse({"code": 0}),
        ]
        clients = [
            client.OceanEngineClient(
                "https://api.test/open_api",
                "token",
                opener=opener,
                request_throttle=throttle,
            )
            for opener in openers
        ]

        clients[0].get("/first")
        clients[1].get("/second")

        self.assertEqual(clock.sleeps, [0.25])

    def test_shared_throttle_applies_rate_limit_cooldown_to_next_client(self):
        clock = FakeClock()
        throttle = client.RequestThrottle(
            minimum_interval=0,
            rate_limit_cooldown=5,
            monotonic_fn=clock.monotonic,
            sleep_fn=clock.sleep,
        )
        limited = client.OceanEngineClient(
            "https://api.test/open_api",
            "token",
            opener=lambda _request, timeout: FakeResponse({"code": "40100"}),
            request_throttle=throttle,
        )
        following = client.OceanEngineClient(
            "https://api.test/open_api",
            "token",
            opener=lambda _request, timeout: FakeResponse({"code": 0}),
            request_throttle=throttle,
        )

        limited.get("/limited")
        following.get("/following")

        self.assertEqual(clock.sleeps, [5.0])

    def test_retry_after_overrides_default_rate_limit_cooldown(self):
        clock = FakeClock()
        throttle = client.RequestThrottle(
            minimum_interval=0,
            rate_limit_cooldown=5,
            monotonic_fn=clock.monotonic,
            sleep_fn=clock.sleep,
        )
        limited = client.OceanEngineClient(
            "https://api.test/open_api",
            "token",
            opener=lambda _request, timeout: FakeResponse(
                {"code": 40100},
                headers={"Retry-After": "12"},
            ),
            request_throttle=throttle,
        )
        following = client.OceanEngineClient(
            "https://api.test/open_api",
            "token",
            opener=lambda _request, timeout: FakeResponse({"code": 0}),
            request_throttle=throttle,
        )

        limited.get("/limited")
        following.get("/following")

        self.assertEqual(clock.sleeps, [12.0])

    def test_retry_after_is_bounded_before_shared_cooldown(self):
        clock = FakeClock()
        throttle = client.RequestThrottle(
            minimum_interval=0,
            rate_limit_cooldown=5,
            max_retry_after=30,
            monotonic_fn=clock.monotonic,
            sleep_fn=clock.sleep,
        )
        limited = client.OceanEngineClient(
            "https://api.test/open_api",
            "token",
            opener=lambda _request, timeout: FakeResponse(
                {"code": 40100},
                headers={"Retry-After": "600"},
            ),
            request_throttle=throttle,
        )
        following = client.OceanEngineClient(
            "https://api.test/open_api",
            "token",
            opener=lambda _request, timeout: FakeResponse({"code": 0}),
            request_throttle=throttle,
        )

        limited.get("/limited")
        following.get("/following")

        self.assertEqual(clock.sleeps, [30.0])

    def test_short_retry_after_does_not_reduce_default_cooldown(self):
        clock = FakeClock()
        throttle = client.RequestThrottle(
            minimum_interval=0,
            rate_limit_cooldown=5,
            max_retry_after=30,
            monotonic_fn=clock.monotonic,
            sleep_fn=clock.sleep,
        )
        limited = client.OceanEngineClient(
            "https://api.test/open_api",
            "token",
            opener=lambda _request, timeout: FakeResponse(
                {"code": 40100},
                headers={"Retry-After": "1"},
            ),
            request_throttle=throttle,
        )
        following = client.OceanEngineClient(
            "https://api.test/open_api",
            "token",
            opener=lambda _request, timeout: FakeResponse({"code": 0}),
            request_throttle=throttle,
        )

        limited.get("/limited")
        following.get("/following")

        self.assertEqual(clock.sleeps, [5.0])

    def test_slow_concurrent_requests_have_one_peak_in_flight(self):
        throttle = client.RequestThrottle(minimum_interval=0, max_in_flight=1)
        active = 0
        peak = 0
        active_lock = threading.Lock()

        def opener(_request, timeout):
            nonlocal active, peak
            with active_lock:
                active += 1
                peak = max(peak, active)
            time.sleep(0.01)
            with active_lock:
                active -= 1
            return FakeResponse({"code": 0})

        api = client.OceanEngineClient(
            "https://api.test/open_api",
            "token",
            opener=opener,
            request_throttle=throttle,
        )
        threads = [threading.Thread(target=api.get, args=(f"/{index}",)) for index in range(8)]
        for thread in threads:
            thread.start()
        for thread in threads:
            thread.join()

        self.assertEqual(peak, 1)

    def test_request_budget_is_atomic_under_concurrency(self):
        budget = client.RequestBudget(7)
        outcomes = []
        outcome_lock = threading.Lock()

        def reserve():
            try:
                budget.reserve()
                outcome = "reserved"
            except ApiError:
                outcome = "rejected"
            with outcome_lock:
                outcomes.append(outcome)

        threads = [threading.Thread(target=reserve) for _ in range(64)]
        for thread in threads:
            thread.start()
        for thread in threads:
            thread.join()

        self.assertEqual(outcomes.count("reserved"), 7)
        self.assertEqual(outcomes.count("rejected"), 57)
        self.assertEqual(budget.snapshot(), {"limit": 7, "used": 7, "remaining": 0})

    def test_qianchuan_factory_clients_share_throttle_and_budget(self):
        with tempfile.TemporaryDirectory() as directory:
            factory = QianchuanClientFactory(directory, "1234567890123456", request_limit=2)
            first = factory.client("https://api.oceanengine.com/open_api", "token")
            second = factory.client("https://ad.oceanengine.com/open_api", "token")

        self.assertIs(first.request_throttle, second.request_throttle)
        self.assertIs(first.request_budget, second.request_budget)
        first.request_budget.reserve()
        second.request_budget.reserve()
        with self.assertRaises(ApiError):
            first.request_budget.reserve()

    def test_shared_state_accepts_go_json_and_waits_for_cooldown(self):
        clock = FakeClock()
        clock.now = 100.0
        with tempfile.TemporaryDirectory() as directory:
            state_path = Path(directory) / "request-control" / "qianchuan-123.json"
            state_path.parent.mkdir(parents=True)
            state_path.write_text(
                '{"next_request_at":100.25,"cooldown_until":103.0}\n',
                encoding="utf-8",
            )
            throttle = client.RequestThrottle(
                minimum_interval=0.25,
                state_path=state_path,
                wall_time_fn=clock.wall_time,
                sleep_fn=clock.sleep,
            )

            throttle.wait()
            written = json.loads(state_path.read_text(encoding="utf-8"))

        self.assertEqual(clock.sleeps, [3.0])
        self.assertEqual(written["next_request_at"], 103.25)
        self.assertEqual(written["cooldown_until"], 103.0)

    def test_corrupt_shared_state_fails_closed(self):
        with tempfile.TemporaryDirectory() as directory:
            state_path = Path(directory) / "request-control" / "qianchuan-123.json"
            state_path.parent.mkdir(parents=True)
            state_path.write_text("not-json", encoding="utf-8")
            throttle = client.RequestThrottle(state_path=state_path)

            with self.assertRaisesRegex(ApiError, "request-control state is invalid"):
                throttle.wait()

    def test_shared_state_rejects_symlink_file_directory_and_lock(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target = root / "target"
            target.mkdir()
            state_directory = root / "request-control"
            state_directory.symlink_to(target, target_is_directory=True)
            throttle = client.RequestThrottle(
                state_path=state_directory / "qianchuan-123.json",
            )
            with self.assertRaisesRegex(ApiError, "request-control state is invalid"):
                throttle.wait()

        for kind in ("state", "lock"):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as directory:
                state_path = Path(directory) / "request-control" / "qianchuan-123.json"
                state_path.parent.mkdir(parents=True)
                target = Path(directory) / "target"
                target.write_text("{}", encoding="utf-8")
                linked_path = state_path if kind == "state" else state_path.with_suffix(".lock")
                linked_path.symlink_to(target)
                throttle = client.RequestThrottle(state_path=state_path)
                with self.assertRaisesRegex(ApiError, "request-control state is invalid"):
                    throttle.wait()

    def test_shared_state_rejects_unknown_and_negative_fields(self):
        invalid_states = (
            {"next_request_at": -1, "cooldown_until": 0},
            {"next_request_at": 0, "cooldown_until": -1},
            {"next_request_at": 0, "cooldown_until": 0, "unexpected": 1},
        )
        for state in invalid_states:
            with self.subTest(state=state), tempfile.TemporaryDirectory() as directory:
                state_path = Path(directory) / "request-control" / "qianchuan-123.json"
                state_path.parent.mkdir(parents=True)
                state_path.write_text(json.dumps(state), encoding="utf-8")
                throttle = client.RequestThrottle(state_path=state_path)

                with self.assertRaisesRegex(ApiError, "request-control state is invalid"):
                    throttle.wait()

    def test_qianchuan_factory_rejects_leading_zero_advertiser_id(self):
        with tempfile.TemporaryDirectory() as directory, self.assertRaisesRegex(
            ValueError,
            "positive decimal ID",
        ):
            QianchuanClientFactory(directory, "0123")

    def test_failed_throttle_acquisition_does_not_consume_request_budget(self):
        with tempfile.TemporaryDirectory() as directory:
            state_path = Path(directory) / "request-control" / "qianchuan-123.json"
            state_path.parent.mkdir(parents=True)
            state_path.write_text("not-json", encoding="utf-8")
            budget = client.RequestBudget(1)
            api = client.OceanEngineClient(
                "https://api.test/open_api",
                "token",
                opener=lambda _request, timeout: self.fail("request was dispatched"),
                request_throttle=client.RequestThrottle(state_path=state_path),
                request_budget=budget,
            )

            with self.assertRaisesRegex(ApiError, "request-control state is invalid"):
                api.get("/blocked")

        self.assertEqual(budget.snapshot(), {"limit": 1, "used": 0, "remaining": 1})


if __name__ == "__main__":
    unittest.main()
