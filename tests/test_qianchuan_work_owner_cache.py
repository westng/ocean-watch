import datetime as dt
import tempfile
import unittest
from pathlib import Path

from ocean_watch.materials import qianchuan_work_owner_cache as owner_cache


class QianchuanWorkOwnerCacheTests(unittest.TestCase):
    def test_cache_is_advertiser_scoped_and_expires(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "owners.json"
            now = dt.datetime(2026, 7, 17, tzinfo=dt.timezone.utc)
            updated = owner_cache.update_owner_hints(
                "1234567890123456",
                {
                    "7001": {"aweme_id": "9001", "aweme_show_id": "creator-one"},
                    "7002": {"aweme_id": "9002", "aweme_show_id": "creator-two"},
                },
                path=path,
                now=now,
            )

            self.assertEqual(updated, 2)
            self.assertEqual(
                owner_cache.load_owner_hints(
                    "1234567890123456",
                    ["7001", "7003"],
                    path=path,
                    now=now,
                ),
                {"7001": {"aweme_id": "9001", "aweme_show_id": "creator-one"}},
            )
            self.assertEqual(
                owner_cache.load_owner_hints(
                    "2345678901234567",
                    ["7001"],
                    path=path,
                    now=now,
                ),
                {},
            )
            self.assertEqual(
                owner_cache.load_owner_hints(
                    "1234567890123456",
                    ["7001"],
                    path=path,
                    now=now + dt.timedelta(days=31),
                ),
                {},
            )

    def test_malformed_cache_fails_open(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "owners.json"
            path.write_text("not-json", encoding="utf-8")
            self.assertEqual(
                owner_cache.load_owner_hints(
                    "1234567890123456",
                    ["7001"],
                    path=path,
                ),
                {},
            )
