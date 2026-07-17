import json
import tempfile
import unittest
from pathlib import Path

from ocean_watch.core.errors import ConfigurationError
from ocean_watch.integrations import qianchuan_work_metadata


class QianchuanWorkMetadataConfigTests(unittest.TestCase):
    def test_empty_or_missing_endpoint_disables_integration(self):
        self.assertIsNone(qianchuan_work_metadata.endpoint_from_config({}))
        self.assertIsNone(qianchuan_work_metadata.endpoint_from_config({
            "integrations": {
                "qianchuan_work_metadata": {"endpoint": ""},
            },
        }))
        self.assertFalse(qianchuan_work_metadata.is_configured({}))
        self.assertTrue(qianchuan_work_metadata.is_configured({
            "integrations": {
                "qianchuan_work_metadata": {
                    "endpoint": "https://metadata.example.test/api",
                },
            },
        }))

    def test_endpoint_requires_credential_free_https(self):
        for endpoint in (
            "http://metadata.example.test/api",
            "https://user:pass@metadata.example.test/api",
            "https://metadata.example.test/api#fragment",
        ):
            with self.subTest(endpoint=endpoint), self.assertRaises(ConfigurationError):
                qianchuan_work_metadata.validate_endpoint(endpoint)

    def test_set_status_and_clear_keep_endpoint_local_and_redacted(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "config.json"
            path.write_text("{}\n", encoding="utf-8")

            configured = qianchuan_work_metadata.set_endpoint(
                path,
                "https://metadata.example.test/api?version=1",
            )
            stored = json.loads(path.read_text(encoding="utf-8"))

            self.assertTrue(configured["configured"])
            self.assertEqual(configured["endpoint"], "<configured locally>")
            self.assertEqual(
                stored["integrations"]["qianchuan_work_metadata"]["endpoint"],
                "https://metadata.example.test/api?version=1",
            )

            cleared = qianchuan_work_metadata.clear_endpoint(path)
            stored = json.loads(path.read_text(encoding="utf-8"))
            self.assertFalse(cleared["configured"])
            self.assertNotIn("integrations", stored)


if __name__ == "__main__":
    unittest.main()
