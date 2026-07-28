import tempfile
import unittest
from pathlib import Path

from scripts.acceptance.run_skill_eval import _redact
from scripts.acceptance.scan_evidence import scan


class EvidenceRedactionTests(unittest.TestCase):
    def test_redaction_preserves_metadata_and_removes_sensitive_values(self):
        value = {
            "plugin_version": "0.9.1+codex.20260720094548",
            "model": "Bearer model-secret-value-123456",
            "trace": "Bearer abcdefghijklmnop access_token=secret-value-123456",
            "advertiser_id": "1000000000000001",
            "command": "ocean-watch accounts list --advertiser-id 1000000000000001",
        }
        redacted = _redact(value)
        self.assertEqual(redacted["plugin_version"], value["plugin_version"])
        self.assertNotIn("model-secret-value-123456", redacted["model"])
        self.assertNotIn("abcdefghijklmnop", redacted["trace"])
        self.assertNotEqual(redacted["advertiser_id"], value["advertiser_id"])
        self.assertNotIn("1000000000000001", redacted["command"])

    def test_evidence_scanner_allows_explicit_fixture_tokens_only(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            clean = root / "clean.json"
            clean.write_text('{"token":"TEST_ACCESS_TOKEN_DO_NOT_USE"}\n', encoding="utf-8")
            self.assertEqual(scan(root), [])
            dirty = root / "dirty.json"
            dirty.write_text('{"access_token":"real-secret-value-123456"}\n', encoding="utf-8")
            self.assertTrue(scan(root))

    def test_source_references_and_fixed_official_mcp_url_are_not_secrets(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / "source.py"
            source.write_text(
                'self.access_token = access_token\n'
                'headers = {"Access-Token": self.access_token}\n'
                'endpoint = "https://open.oceanengine.com/qianchuan/mcp"\n',
                encoding="utf-8",
            )
            self.assertEqual(scan(root), [])

    def test_literal_token_and_nonofficial_mcp_url_are_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / "source.py"
            source.write_text(
                'access_token = "real-secret-value-123456"\n'
                'refresh_token: abcdefghijklmnop\n'
                'endpoint = "https://attacker.example/streamable-mcp"\n',
                encoding="utf-8",
            )
            findings = scan(root)
            self.assertEqual(len(findings), 3)


if __name__ == "__main__":
    unittest.main()
